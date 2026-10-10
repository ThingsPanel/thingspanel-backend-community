package service

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	nativeE2EDSN         = "postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test"
	nativeE2EJWTKey      = "fixture-only-jwt-signing-key-not-for-production"
	nativeE2ESourceToken = "FIXTURE_SOURCE_AUTH_SENTINEL_20261009"
	nativeE2EProjToken   = "FIXTURE_PROJECTION_AUTH_SENTINEL_20261009"
	nativeE2ECoreURL     = "http://127.0.0.1:19404"
	nativeE2ESidecarURL  = "http://127.0.0.1:19583"
	nativeE2ETenant      = "tenant-native"
	nativeE2EDeployment  = "deployment-native"
)

// Opt-in integration: creates native records and sends one message only to
// the local fixture SMTP adapter, never to a real provider.
func TestNativeEncoreSourceBridgeEndToEnd(t *testing.T) {
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
	initialFixtureCalls := nativeE2EFixtureSendCalls(t)
	db := openNativeSourceE2EPG(t)
	seedNativeLegacyEmail(t, db)
	target, _ := url.Parse(nativeE2ECoreURL)
	var captureMu sync.Mutex
	var acceptedResponses []sourceAccepted
	var acceptedBodies [][]byte
	var acceptedKeys []string
	ackDropped := false
	var sourceStatuses []int
	sourceAuthForwarded := false
	proxyTransport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false}
	t.Cleanup(proxyTransport.CloseIdleConnections)
	reverseProxy := httputil.NewSingleHostReverseProxy(target)
	proxyServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sourceEventPath {
			reverseProxy.ServeHTTP(w, r)
			return
		}
		captureMu.Lock()
		sourceAuthForwarded = r.Header.Get("Authorization") == "Bearer "+nativeE2ESourceToken
		captureMu.Unlock()
		requestBody, err := io.ReadAll(io.LimitReader(r.Body, maxSourceBody+1))
		_ = r.Body.Close()
		if err != nil || len(requestBody) == 0 || len(requestBody) > maxSourceBody {
			http.Error(w, "source request invalid", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(requestBody))
		upstreamURL := *target
		upstreamURL.Path = r.URL.Path
		upstreamURL.RawPath = r.URL.RawPath
		upstreamURL.RawQuery = r.URL.RawQuery
		upstreamRequest := r.Clone(r.Context())
		upstreamRequest.URL = &upstreamURL
		upstreamRequest.Host = target.Host
		upstreamRequest.RequestURI = ""
		response, err := proxyTransport.RoundTrip(upstreamRequest)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		captureMu.Lock()
		sourceStatuses = append(sourceStatuses, response.StatusCode)
		captureMu.Unlock()
		body, err := io.ReadAll(io.LimitReader(response.Body, maxSourceBody+1))
		if err != nil || len(body) > maxSourceBody {
			http.Error(w, "upstream response invalid", http.StatusBadGateway)
			return
		}
		var envelope sourceEnvelope[sourceAccepted]
		isDurableAcceptance := response.StatusCode == http.StatusAccepted && json.Unmarshal(body, &envelope) == nil && envelope.Code == 200 && envelope.Data.Accepted
		if isDurableAcceptance {
			captureMu.Lock()
			acceptedResponses = append(acceptedResponses, envelope.Data)
			acceptedBodies = append(acceptedBodies, append([]byte(nil), requestBody...))
			acceptedKeys = append(acceptedKeys, r.Header.Get("Idempotency-Key"))
			dropThisAck := !ackDropped
			ackDropped = true
			captureMu.Unlock()
			if dropThisAck {
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					t.Error("TLS proxy cannot inject a dropped acknowledgement")
					return
				}
				conn, _, err := hijacker.Hijack()
				if err != nil {
					t.Error("could not drop first native acknowledgement")
					return
				}
				partial := body[:len(body)/2]
				_, _ = fmt.Fprintf(conn, "HTTP/1.1 202 Accepted\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", len(body)+32)
				_, _ = conn.Write(partial)
				_ = conn.Close()
				return
			}
		}
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(body)
	}))
	t.Cleanup(proxyServer.Close)
	roots := x509.NewCertPool()
	roots.AddCert(proxyServer.Certificate())
	checker, err := newEmailSourceCompatibilityChecker(proxyServer.URL, nativeE2EProjToken, roots)
	if err != nil {
		t.Fatal("construct TLS-pinned source compatibility checker")
	}
	bridge, err := newSourceBridge(SourceBridgeConfig{Enabled: true, BaseURL: proxyServer.URL, DeploymentID: nativeE2EDeployment, SourceBearerToken: nativeE2ESourceToken, ProjectionBearerToken: nativeE2EProjToken, RequestTimeout: 5 * time.Second}, checker, roots)
	if err != nil {
		checker.Close()
		t.Fatal("construct TLS-pinned source relay")
	}
	t.Cleanup(bridge.Close)

	client := proxyServer.Client()
	client.Timeout = 5 * time.Second
	adminJWT := nativeE2EAdminJWT(t)
	pluginID := nativeE2EFindSMTPPlugin(t, client, proxyServer.URL, adminJWT)
	instanceID, _ := nativeE2ECreateSMTPInstance(t, client, proxyServer.URL, adminJWT, pluginID)
	groupID, revision := nativeE2ECreateEmailGroup(t, client, proxyServer.URL, adminJWT, instanceID)
	projection := SourceGroupProjectionRequest{SourceDeploymentID: nativeE2EDeployment, TenantID: nativeE2ETenant, LegacyGroupID: "legacy-native-email", NotificationGroupID: groupID, GroupRevision: revision}
	legacyGroup, err := dal.GetNotificationGroupByTenantID(projection.LegacyGroupID, projection.TenantID)
	if err != nil {
		t.Fatal("read isolated legacy group before native switch")
	}
	snapshot, err := checker.fetchSnapshot(context.Background(), projection)
	if err != nil {
		t.Fatal("native group snapshot endpoint rejected the projection identity")
	}
	if snapshot.SourceDeploymentID != projection.SourceDeploymentID || snapshot.TenantID != projection.TenantID || snapshot.Group.ID != groupID || !snapshot.Group.Enabled || snapshot.Group.Revision != revision {
		t.Fatal("native group snapshot did not match its requested tenant and revision")
	}
	recipients, err := legacyEmailRecipients(legacyGroup)
	if err != nil {
		t.Fatal("fixture legacy recipient set is invalid")
	}
	legacyConfig, enabled, err := checker.legacyConfig(context.Background())
	if err != nil || !enabled {
		t.Fatal("fixture legacy SMTP identity unavailable")
	}
	if err := compareSourceEmailProjection(snapshot, recipients, legacyConfig); err != nil {
		t.Fatal("native SMTP group snapshot is not semantically compatible")
	}
	if err := checker.CheckEncoreCompatibility(context.Background(), legacyGroup, projection); err != nil {
		t.Fatal("native email compatibility check failed after exact snapshot validation")
	}
	if _, err := bridge.RegisterProjection(context.Background(), projection, "native-e2e-projection-0001"); err != nil {
		t.Fatal("native projection endpoint rejected the exact group revision")
	}
	if err := bridge.SwitchToEncore(context.Background(), projection, "native-e2e-projection-0001", 0); err != nil {
		t.Fatal("native projection and source route switch failed")
	}

	routeKey := dal.SourceRouteKey{DeploymentID: nativeE2EDeployment, TenantID: nativeE2ETenant, LegacyGroup: "legacy-native-email"}
	eventID, actionID := uuid.NewString(), uuid.NewString()
	occurred := time.Now().UTC().Truncate(time.Microsecond)
	alarm := &model.AlarmInfo{ID: eventID, AlarmConfigID: "fixture-alarm-config", Name: "native e2e alarm", AlarmTime: occurred, ProcessingResult: "UND", TenantID: nativeE2ETenant}
	legacyText := "Alert: native e2e alarm\nLevel: H\nTime: " + occurred.Format("2006-01-02 15:04:05") + "\nDescription: \nDetails: fixture source event"
	route, err := dal.SaveAlarmInfoWithSource(context.Background(), alarm, routeKey, func(snapshot dal.SourceRouteSnapshot) (*dal.SourceOutboxRecord, error) {
		return bridge.buildOutbox(snapshot, nativeE2ETenant, "legacy-native-email", eventID, actionID, occurred, "[ALERT] native e2e alarm [H]", legacyText, `{"source":"fixture"}`)
	})
	if err != nil || route.Engine != "encore" || route.NotificationGroupID != groupID || route.GroupRevision != revision {
		t.Fatalf("atomic alarm/outbox ownership mismatch: engine=%s revision=%d", route.Engine, route.GroupRevision)
	}
	var outbox dal.SourceOutboxRecord
	if err := global.DB.Where("source_event_id = ? AND source_action_id = ?", eventID, actionID).Take(&outbox).Error; err != nil {
		t.Fatal("read frozen source outbox row")
	}
	if err := bridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("relay native source outbox")
	}
	var state string
	var attempts int
	var failureCode string
	if err := global.DB.Raw(`SELECT state, attempts, coalesce(failure_code, '') FROM notification_source_outbox WHERE id = ?`, outbox.ID).Row().Scan(&state, &attempts, &failureCode); err != nil || state != "retry_wait" || attempts != 1 {
		captureMu.Lock()
		defer captureMu.Unlock()
		t.Fatalf("dropped native durable ACK was not retained for retry: state=%q attempts=%d failure=%q upstream_statuses=%v accepted_responses=%d ack_dropped=%v source_auth_forwarded=%v err=%v", state, attempts, failureCode, sourceStatuses, len(acceptedResponses), ackDropped, sourceAuthForwarded, err)
	}
	if err := global.DB.Exec(`UPDATE notification_source_outbox SET next_attempt_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Second), outbox.ID).Error; err != nil {
		t.Fatal("make native source retry immediately eligible")
	}
	bridge.Close()
	restartedChecker, err := newEmailSourceCompatibilityChecker(proxyServer.URL, nativeE2EProjToken, roots)
	if err != nil {
		t.Fatal("recreate compatibility checker after relay restart")
	}
	restartedBridge, err := newSourceBridge(SourceBridgeConfig{Enabled: true, BaseURL: proxyServer.URL, DeploymentID: nativeE2EDeployment, SourceBearerToken: nativeE2ESourceToken, ProjectionBearerToken: nativeE2EProjToken, RequestTimeout: 5 * time.Second}, restartedChecker, roots)
	if err != nil {
		restartedChecker.Close()
		t.Fatal("recreate relay after simulated process restart")
	}
	t.Cleanup(restartedBridge.Close)
	if err := restartedBridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("restarted relay retry failed")
	}
	if err := global.DB.Raw(`SELECT state, attempts FROM notification_source_outbox WHERE id = ?`, outbox.ID).Row().Scan(&state, &attempts); err != nil || state != "handed_off" || attempts != 2 {
		t.Fatalf("restarted relay did not persist source handoff: state=%q attempts=%d", state, attempts)
	}
	captureMu.Lock()
	if len(acceptedResponses) != 2 || acceptedResponses[0].NotificationID == "" || acceptedResponses[0].NotificationID != acceptedResponses[1].NotificationID || len(acceptedResponses[0].DeliveryIDs) != 1 || len(acceptedResponses[1].DeliveryIDs) != 1 || acceptedResponses[0].DeliveryIDs[0] != acceptedResponses[1].DeliveryIDs[0] || len(acceptedBodies) != 2 || string(acceptedBodies[0]) != string(outbox.RequestBody) || string(acceptedBodies[1]) != string(outbox.RequestBody) || len(acceptedKeys) != 2 || acceptedKeys[0] != outbox.IdempotencyKey || acceptedKeys[1] != outbox.IdempotencyKey {
		captureMu.Unlock()
		t.Fatal("native idempotent retry changed source key, body, notification, or delivery IDs")
	}
	accepted := acceptedResponses[0]
	captureMu.Unlock()

	dispatch, delivery := "", ""
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		dispatch, delivery = nativeE2EReadDelivery(client, proxyServer.URL, adminJWT, accepted.NotificationID, accepted.DeliveryIDs[0])
		if dispatch == "accepted" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if dispatch != "accepted" {
		t.Fatalf("native SMTP fixture did not accept delivery: dispatch=%q delivery=%q", dispatch, delivery)
	}
	if err := dal.SwitchSourceRouteToLegacy(context.Background(), routeKey, route.RouteVersion); err != nil {
		t.Fatal("explicit source route rollback failed")
	}
	rollbackID := uuid.NewString()
	rollbackAlarm := &model.AlarmInfo{ID: rollbackID, AlarmConfigID: "fixture-alarm-config", Name: "post rollback alarm", AlarmTime: time.Now().UTC(), ProcessingResult: "UND", TenantID: nativeE2ETenant}
	rollbackRoute, err := dal.SaveAlarmInfoWithSource(context.Background(), rollbackAlarm, routeKey, func(snapshot dal.SourceRouteSnapshot) (*dal.SourceOutboxRecord, error) {
		return bridge.buildOutbox(snapshot, nativeE2ETenant, "legacy-native-email", rollbackID, uuid.NewString(), rollbackAlarm.AlarmTime, "subject", "body", `{"source":"fixture"}`)
	})
	if err != nil || rollbackRoute.Engine != "legacy" {
		t.Fatal("post-rollback event did not select only legacy engine")
	}
	var alarmCount, outboxCount int64
	if err := global.DB.Raw(`SELECT count(*) FROM alarm_info WHERE id IN (?, ?)`, eventID, rollbackID).Scan(&alarmCount).Error; err != nil {
		t.Fatal("count fixture alarm rows")
	}
	if err := global.DB.Raw(`SELECT count(*) FROM notification_source_outbox WHERE source_event_id IN (?, ?)`, eventID, rollbackID).Scan(&outboxCount).Error; err != nil {
		t.Fatal("count fixture outbox rows")
	}
	if alarmCount != 2 || outboxCount != 1 {
		t.Fatalf("rollback changed event ownership: alarms=%d outbox=%d", alarmCount, outboxCount)
	}
	fixtureCalls := nativeE2EFixtureSendCalls(t)
	if fixtureCalls != initialFixtureCalls+1 {
		t.Fatalf("local SMTP fixture send count: calls=%d", fixtureCalls)
	}
	t.Logf("native e2e: projection=200 source_first=202 source_retry=202 dispatch=accepted delivery_status=%s notification_id=%s delivery_id=%s alarms=%d outbox=%d smtp_fixture_calls=%d", delivery, accepted.NotificationID, accepted.DeliveryIDs[0], alarmCount, outboxCount, fixtureCalls)
}

func openNativeSourceE2EPG(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open approved PostgreSQL fixture")
	}
	var database, user string
	if err := admin.Raw(`SELECT current_database(), current_user`).Row().Scan(&database, &user); err != nil || database != "notification_test" || user != "notification_test" {
		t.Fatal("approved PostgreSQL fixture identity mismatch")
	}
	schema := "t07native_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("create unique fixture schema")
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal("access fixture admin pool")
	}
	parsed, _ := url.Parse(dsn)
	values := parsed.Query()
	values.Set("options", "-c search_path="+schema)
	parsed.RawQuery = values.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open unique fixture schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("access unique fixture pool")
	}
	pool.SetMaxOpenConns(8)
	previousDB := global.DB
	previousLog := logrus.GetLevel()
	global.DB = db
	query.SetDefault(db)
	logrus.SetLevel(logrus.ErrorLevel)
	t.Cleanup(func() {
		global.DB = previousDB
		if previousDB != nil {
			query.SetDefault(previousDB)
		}
		logrus.SetLevel(previousLog)
		_ = pool.Close()
		_ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		_ = adminPool.Close()
	})
	for _, ddl := range []string{
		`CREATE TABLE casbin_rule (ptype text NOT NULL, v0 text, v1 text, v2 text, v3 text, v4 text, v5 text)`,
		`CREATE TABLE notification_groups (id text PRIMARY KEY, name text NOT NULL, notification_type text NOT NULL, status text NOT NULL, notification_config text, description text, tenant_id text NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, remark text)`,
		`CREATE TABLE notification_services_config (id text PRIMARY KEY, config text, notice_type text NOT NULL, status text NOT NULL, remark text)`,
		`CREATE TABLE alarm_info (id text PRIMARY KEY, alarm_config_id text NOT NULL, name text NOT NULL, alarm_time timestamptz NOT NULL, description text, content text, processor text, processing_result text NOT NULL, tenant_id text NOT NULL, remark text, alarm_level text)`,
		`CREATE TABLE alarm_history (id text PRIMARY KEY, alarm_config_id text NOT NULL, group_id text NOT NULL, scene_automation_id text NOT NULL, name text NOT NULL, description text, content text, alarm_status text NOT NULL, tenant_id text NOT NULL, remark text, create_at timestamptz NOT NULL, alarm_device_list text NOT NULL)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal("create isolated source fixture tables")
		}
	}
	applyNativeE2EMigration(t, db, schema, "../../sql/23.sql")
	applyNativeE2EMigration(t, db, schema, "../../sql/24.sql")
	applyNativeE2EMigration(t, db, schema, "../../sql/25.sql")
	return db
}

func applyNativeE2EMigration(t *testing.T, db *gorm.DB, schema, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read isolated migration")
	}
	sql := strings.ReplaceAll(string(raw), "public.", `"`+schema+`".`)
	if strings.HasSuffix(path, "24.sql") {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal("apply isolated source rollback migration")
		}
		return
	}
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			sql = strings.ReplaceAll(sql, line, "")
		}
	}
	for _, statement := range strings.Split(sql, ";") {
		statement = strings.TrimSpace(statement)
		if statement != "" && db.Exec(statement).Error != nil {
			t.Fatal("apply source route fixture migration")
		}
	}
}

func seedNativeLegacyEmail(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO notification_groups (id, name, notification_type, status, notification_config, tenant_id, created_at, updated_at) VALUES (?, ?, 'EMAIL', 'OPEN', ?, ?, ?, ?)`, "legacy-native-email", "legacy fixture group", `{"EMAIL":"ops@example.test"}`, nativeE2ETenant, now, now).Error; err != nil {
		t.Fatal("seed fixture legacy email group")
	}
	if err := db.Exec(`INSERT INTO notification_services_config (id, config, notice_type, status) VALUES (?, ?, 'EMAIL', 'OPEN')`, "legacy-email-config", `{"host":"smtp.example.test","port":465,"from_password":"fixture-only-old-password","from_email":"sender@example.test","ssl":true}`).Error; err != nil {
		t.Fatal("seed fixture legacy email identity")
	}
}

func nativeE2EAdminJWT(t *testing.T) string {
	t.Helper()
	claims := utils.UserClaims{ID: "fixture-user", TenantID: nativeE2ETenant, Authority: "TENANT_ADMIN", StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(time.Hour).Unix(), IssuedAt: time.Now().Add(-time.Minute).Unix()}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(nativeE2EJWTKey))
	if err != nil {
		t.Fatal("sign fixture administrator JWT")
	}
	return token
}

func nativeE2EFindSMTPPlugin(t *testing.T, client *http.Client, baseURL, token string) string {
	t.Helper()
	var result struct {
		Items []struct {
			ID            string `json:"id"`
			PluginID      string `json:"pluginId"`
			PluginVersion string `json:"pluginVersion"`
			Enabled       bool   `json:"enabled"`
		} `json:"items"`
	}
	status := nativeE2EGetJSON(t, client, baseURL+"/api/v2/notification-plugins?page=1&pageSize=100", token, &result)
	if status != http.StatusOK {
		t.Fatalf("native plugin listing returned HTTP %d", status)
	}
	for _, plugin := range result.Items {
		if plugin.PluginID == "thingspanel.smtp" && plugin.PluginVersion == "1.0.1" && plugin.Enabled {
			return plugin.ID
		}
	}
	t.Fatal("enabled native SMTP fixture plugin 1.0.1 is unavailable")
	return ""
}

func nativeE2ECreateSMTPInstance(t *testing.T, client *http.Client, baseURL, token, pluginID string) (string, int64) {
	t.Helper()
	body := []byte(`{"pluginRegistrationId":"` + pluginID + `","name":"native source E2E SMTP","channel":"email","config":{"host":"smtp.example.test","port":465,"from_email":"sender@example.test","from_password":"fixture-only-new-password","ssl":true},"providerIdentity":{}}`)
	var created struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	status := nativeE2ERequestJSON(t, client, http.MethodPost, baseURL+"/api/v2/notification-instances", token, "native-e2e-instance-create-01", body, &created)
	if status != http.StatusCreated || created.ID == "" {
		t.Fatalf("native SMTP instance create returned HTTP %d", status)
	}
	enableBody := []byte(`{"expectedVersion":` + strconv.FormatInt(created.Version, 10) + `,"enabled":true}`)
	var enabled struct {
		Enabled bool `json:"enabled"`
	}
	status = nativeE2ERequestJSON(t, client, http.MethodPut, baseURL+"/api/v2/notification-instances/"+url.PathEscape(created.ID), token, "native-e2e-instance-enable-01", enableBody, &enabled)
	if status != http.StatusOK || !enabled.Enabled {
		t.Fatalf("native SMTP instance enable returned HTTP %d", status)
	}
	return created.ID, created.Version + 1
}

func nativeE2ECreateEmailGroup(t *testing.T, client *http.Client, baseURL, token, instanceID string) (string, int64) {
	t.Helper()
	body := []byte(`{"name":"legacy EMAIL source E2E","enabled":true,"bindings":[{"bindingId":"source-email-binding","instanceId":"` + instanceID + `","recipientSource":{"kind":"literal","recipient":{"kind":"email","address":"ops@example.test"}},"contentBinding":{"kind":"text","title":"{{subject}}","text":"{{text}}\n\n---\nThis email was sent by ThingsPanel"}}]}`)
	var group struct {
		ID       string `json:"id"`
		Enabled  bool   `json:"enabled"`
		Revision int64  `json:"revision"`
	}
	status := nativeE2ERequestJSON(t, client, http.MethodPost, baseURL+"/api/v2/notification-groups", token, "native-e2e-group-create-01", body, &group)
	if status != http.StatusCreated || group.ID == "" || !group.Enabled || group.Revision < 1 {
		t.Fatalf("native email group create returned HTTP %d", status)
	}
	return group.ID, group.Revision
}

func nativeE2EGetJSON(t *testing.T, client *http.Client, endpoint, token string, dst any) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal("build fixture GET")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("call fixture GET")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		t.Fatal("read bounded fixture GET response")
	}
	if dst != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(body, &envelope) != nil || json.Unmarshal(envelope.Data, dst) != nil {
			t.Fatalf("decode fixture GET response after HTTP %d", response.StatusCode)
		}
	}
	return response.StatusCode
}

func nativeE2ERequestJSON(t *testing.T, client *http.Client, method, endpoint, token, key string, requestBody []byte, dst any) int {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, strings.NewReader(string(requestBody)))
	if err != nil {
		t.Fatal("build fixture management request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("call fixture management endpoint")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		t.Fatal("read bounded fixture management response")
	}
	if dst != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(body, &envelope) != nil || json.Unmarshal(envelope.Data, dst) != nil {
			t.Fatalf("decode fixture management response after HTTP %d", response.StatusCode)
		}
	}
	return response.StatusCode
}

func nativeE2EReadDelivery(client *http.Client, baseURL, token, notificationID, deliveryID string) (string, string) {
	request, err := http.NewRequest(http.MethodGet, baseURL+"/api/v2/notifications/"+url.PathEscape(notificationID), nil)
	if err != nil {
		return "", ""
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return "", ""
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil || response.StatusCode != http.StatusOK {
		return "", ""
	}
	var envelope struct {
		Data struct {
			Deliveries []struct {
				DeliveryID     string `json:"deliveryId"`
				DispatchStatus string `json:"dispatchStatus"`
				DeliveryStatus string `json:"deliveryStatus"`
			} `json:"deliveries"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return "", ""
	}
	for _, delivery := range envelope.Data.Deliveries {
		if delivery.DeliveryID == deliveryID {
			return delivery.DispatchStatus, delivery.DeliveryStatus
		}
	}
	return "", ""
}

func nativeE2EFixtureSendCalls(t *testing.T) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, nativeE2ESidecarURL+"/__fixture/calls", nil)
	if err != nil {
		t.Fatal("build local SMTP fixture counter request")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal("read local SMTP fixture counter")
	}
	defer response.Body.Close()
	var result struct {
		SendCalls int `json:"sendCalls"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) != nil {
		t.Fatal("local SMTP fixture counter response unavailable")
	}
	return result.SendCalls
}
