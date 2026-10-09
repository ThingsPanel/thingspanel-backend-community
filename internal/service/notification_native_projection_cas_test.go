package service

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"project/internal/dal"
	"project/internal/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// A real Core projection may succeed while the old source-group snapshot
// changes before local CAS. The operator receives the ordinary switch error,
// and local route ownership remains legacy; no pending-state wire is invented.
func TestNativeProjectionSuccessThenLegacyCASConflictKeepsRouteLegacy(t *testing.T) {
	if os.Getenv("NOTIFICATION_NATIVE_E2E") != "1" {
		t.Skip("set NOTIFICATION_NATIVE_E2E=1 to run the local native integration")
	}
	if os.Getenv("NOTIFICATION_TEST_DSN") != nativeE2EDSN {
		t.Fatal("native integration requires the approved isolated PostgreSQL fixture")
	}
	for _, endpoint := range []struct{ raw, port string }{{nativeE2ECoreURL, "19404"}, {nativeE2ESidecarURL, "19583"}} {
		parsed, err := url.Parse(endpoint.raw)
		if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() != endpoint.port {
			t.Fatal("native integration endpoint is outside the approved local fixture")
		}
	}
	db := openNativeSourceE2EPG(t)
	seedNativeLegacyEmail(t, db)
	target, _ := url.Parse(nativeE2ECoreURL)
	coreProxy := httputil.NewSingleHostReverseProxy(target)
	legacyGroupID := uuid.NewString()
	var mu sync.Mutex
	var projectionStatuses []int
	var projectionResponses []SourceGroupProjection
	var mutationErr error
	proxyServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != sourceProjectPath {
			coreProxy.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxSourceBody+1))
		_ = r.Body.Close()
		if err != nil || len(body) == 0 || len(body) > maxSourceBody {
			http.Error(w, "invalid projection fixture", http.StatusBadRequest)
			return
		}
		var projection SourceGroupProjectionRequest
		if json.Unmarshal(body, &projection) != nil {
			http.Error(w, "invalid projection fixture", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		response := httptest.NewRecorder()
		coreProxy.ServeHTTP(response, r)
		mu.Lock()
		projectionStatuses = append(projectionStatuses, response.Code)
		if response.Code == http.StatusOK && projection.LegacyGroupID == legacyGroupID {
			var envelope sourceEnvelope[SourceGroupProjection]
			if json.Unmarshal(response.Body.Bytes(), &envelope) == nil {
				projectionResponses = append(projectionResponses, envelope.Data)
			}
			// Simulate a legacy editor changing the source group after native
			// publication succeeds but before local route CAS executes.
			mutationErr = db.Exec(`UPDATE notification_groups SET notification_config = ?, updated_at = now() + interval '1 second' WHERE id = ? AND tenant_id = ?`, `{"EMAIL":"changed@example.test"}`, legacyGroupID, nativeE2ETenant).Error
		}
		mu.Unlock()
		for key, values := range response.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	}))
	t.Cleanup(proxyServer.Close)
	roots := x509.NewCertPool()
	roots.AddCert(proxyServer.Certificate())

	adminJWT := nativeE2EAdminJWT(t)
	client := proxyServer.Client()
	pluginID := nativeE2EFindSMTPPlugin(t, client, proxyServer.URL, adminJWT)
	instanceID, _ := nativeE2ECreateSMTPInstance(t, client, proxyServer.URL, adminJWT, pluginID)
	nativeGroupID, nativeRevision := nativeE2ECreateEmailGroup(t, client, proxyServer.URL, adminJWT, instanceID)
	var currentGroup struct {
		ID       string `json:"id"`
		Enabled  bool   `json:"enabled"`
		Revision int64  `json:"revision"`
	}
	groupStatus := nativeE2EGetJSON(t, client, proxyServer.URL+"/api/v2/notification-groups/"+url.PathEscape(nativeGroupID), adminJWT, &currentGroup)
	if groupStatus != http.StatusOK || currentGroup.ID != nativeGroupID || !currentGroup.Enabled || currentGroup.Revision != nativeRevision {
		t.Fatalf("native group readback does not match fixture setup: status=%d enabled=%v revision_match=%v", groupStatus, currentGroup.Enabled, currentGroup.Revision == nativeRevision)
	}
	if err := db.Exec(`INSERT INTO notification_groups (id, name, notification_type, status, notification_config, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, 'OPEN', ?, ?, now(), now())`, legacyGroupID, "native CAS race source", model.NoticeType_Email, `{"EMAIL":"ops@example.test"}`, nativeE2ETenant).Error; err != nil {
		t.Fatal("create new isolated source group")
	}
	checker, err := newEmailSourceCompatibilityChecker(proxyServer.URL, nativeE2EProjToken, roots)
	if err != nil {
		t.Fatal("construct TLS-pinned source compatibility checker")
	}
	bridge, err := newSourceBridge(SourceBridgeConfig{Enabled: true, BaseURL: proxyServer.URL, DeploymentID: nativeE2EDeployment, SourceBearerToken: nativeE2ESourceToken, ProjectionBearerToken: nativeE2EProjToken, RequestTimeout: 5 * time.Second}, checker, roots)
	if err != nil {
		checker.Close()
		t.Fatal("construct TLS-pinned source bridge")
	}
	t.Cleanup(bridge.Close)
	request := SourceGroupProjectionRequest{SourceDeploymentID: nativeE2EDeployment, TenantID: nativeE2ETenant, LegacyGroupID: legacyGroupID, NotificationGroupID: nativeGroupID, GroupRevision: nativeRevision}
	legacyGroup, err := dal.GetNotificationGroupByTenantID(legacyGroupID, nativeE2ETenant)
	if err != nil || legacyGroup == nil {
		t.Fatal("read the isolated legacy group for semantic validation")
	}
	if _, err := legacyEmailRecipients(legacyGroup); err != nil {
		t.Fatal("isolated legacy recipient is invalid")
	}
	legacyConfig, enabled, err := checker.legacyConfig(context.Background())
	if err != nil || !enabled || legacyConfig.Host == "" {
		t.Fatal("isolated legacy SMTP identity is unavailable")
	}
	snapshot, err := checker.fetchSnapshot(context.Background(), request)
	if err != nil || snapshot.Group.ID != nativeGroupID || snapshot.Group.Revision != nativeRevision {
		diagnosticURL := proxyServer.URL + sourceGroupSnapshotPath + url.PathEscape(nativeGroupID) + "?tenantId=" + url.QueryEscape(nativeE2ETenant) + "&groupRevision=" + strconv.FormatInt(nativeRevision, 10)
		diagnosticRequest, _ := http.NewRequest(http.MethodGet, diagnosticURL, nil)
		diagnosticRequest.Header.Set("Authorization", "Bearer "+nativeE2EProjToken)
		diagnosticResponse, requestErr := client.Do(diagnosticRequest)
		diagnosticStatus, diagnosticCode := 0, 0
		if requestErr == nil && diagnosticResponse != nil {
			diagnosticStatus = diagnosticResponse.StatusCode
			var envelope struct {
				Code int `json:"code"`
			}
			_ = json.NewDecoder(io.LimitReader(diagnosticResponse.Body, 4096)).Decode(&envelope)
			diagnosticCode = envelope.Code
			_ = diagnosticResponse.Body.Close()
		}
		t.Fatalf("native exact-revision snapshot is unavailable: http=%d envelope_code=%d request_failed=%v", diagnosticStatus, diagnosticCode, requestErr != nil)
	}
	if err := compareSourceEmailProjection(snapshot, []string{"ops@example.test"}, legacyConfig); err != nil {
		t.Fatal("native email projection semantics do not match the isolated legacy group")
	}
	const projectionKey = "native-cas-conflict-projection-01"
	err = bridge.SwitchToEncore(context.Background(), request, projectionKey, 0)
	if err == nil || err.Error() != "source route update failed" {
		t.Fatalf("local CAS failure was not returned to caller: %v", err)
	}
	mu.Lock()
	statuses := append([]int(nil), projectionStatuses...)
	responses := append([]SourceGroupProjection(nil), projectionResponses...)
	updateErr := mutationErr
	mu.Unlock()
	if updateErr != nil {
		t.Fatal("could not inject the isolated legacy source-group edit")
	}
	if len(statuses) != 1 || statuses[0] != http.StatusOK || len(responses) != 1 || responses[0].TenantID != request.TenantID || responses[0].LegacyGroupID != request.LegacyGroupID || responses[0].NotificationGroupID != request.NotificationGroupID || responses[0].GroupRevision != request.GroupRevision {
		t.Fatalf("native projection was not durably published for the exact tuple: statuses=%v responses=%d", statuses, len(responses))
	}
	route, err := dal.WithLockedSourceRoute(context.Background(), dal.SourceRouteKey{DeploymentID: request.SourceDeploymentID, TenantID: request.TenantID, LegacyGroup: request.LegacyGroupID}, func(_ *gorm.DB, _ dal.SourceRouteSnapshot) error { return nil })
	if err != nil || route.Engine != "legacy" || route.RouteVersion != 0 || route.NotificationGroupID != "" {
		t.Fatalf("failed local CAS changed route ownership: route=%+v err=%v", route, err)
	}
	t.Logf("native projection CAS fixture: core_projection_status=200 local_route_error=visible route_engine=%s route_version=%d", route.Engine, route.RouteVersion)
}
