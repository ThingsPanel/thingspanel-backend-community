package service

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"testing"
	"time"

	"project/internal/dal"

	"github.com/google/uuid"
)

// Opt-in local integration for the safe blocked path. The source route is
// seeded as if an earlier projection had been validated, then points at a
// native group revision that is unavailable to Core. No provider is invoked.
func TestNativeEncoreMissingRevisionBlocksAlarmWithoutSending(t *testing.T) {
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
	initialNativeCalls := nativeE2EFixtureSendCalls(t)
	db := openNativeSourceE2EPG(t)
	seedNativeLegacyEmail(t, db)
	fixtureSMTP := startCountingFixtureSMTP(t)
	legacySMTP, err := json.Marshal(struct {
		Host         string `json:"host"`
		Port         int    `json:"port"`
		FromPassword string `json:"from_password"`
		FromEmail    string `json:"from_email"`
		SSL          bool   `json:"ssl"`
	}{Host: fixtureSMTP.host, Port: fixtureSMTP.port, FromPassword: "fixture-only-password", FromEmail: "sender@example.test", SSL: false})
	if err != nil {
		t.Fatal("encode local-only legacy SMTP fixture")
	}
	if err := db.Exec(`UPDATE notification_services_config SET config = ? WHERE id = ?`, string(legacySMTP), "legacy-email-config").Error; err != nil {
		t.Fatal("configure the legacy sender to use only the local counting fixture")
	}
	if err := db.Exec(`CREATE TABLE alarm_config (
		id text PRIMARY KEY, name text NOT NULL, description text, alarm_level text NOT NULL,
		notification_group_id text NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
		tenant_id text NOT NULL, remark text, enabled text NOT NULL)`).Error; err != nil {
		t.Fatal("create isolated alarm config table")
	}
	configID := uuid.NewString()
	legacyGroupID := uuid.NewString()
	missingGroupID := uuid.NewString()
	if err := db.Exec(`INSERT INTO notification_groups (id, name, notification_type, status, notification_config, tenant_id, created_at, updated_at)
		VALUES (?, ?, 'EMAIL', 'OPEN', ?, ?, now(), now())`, legacyGroupID, "blocked source fixture group", `{"EMAIL":"ops@example.test"}`, nativeE2ETenant).Error; err != nil {
		t.Fatal("create unique legacy group for blocked source fixture")
	}
	if err := db.Exec(`INSERT INTO alarm_config (id, name, description, alarm_level, notification_group_id, created_at, updated_at, tenant_id, enabled)
		VALUES (?, ?, ?, 'H', ?, now(), now(), ?, 'Y')`, configID, "blocked source fixture alarm", "temporary unavailable native target", legacyGroupID, nativeE2ETenant).Error; err != nil {
		t.Fatal("create isolated alarm config")
	}

	// This route seed models a previously validated projection. The target is
	// intentionally absent from Core, so this new fixture tuple tests loss of
	// the mapped revision without changing any existing route or group history.
	routeKey := dal.SourceRouteKey{DeploymentID: nativeE2EDeployment, TenantID: nativeE2ETenant, LegacyGroup: legacyGroupID}
	const missingRevision = int64(71)
	if err := dal.SwitchSourceRoute(context.Background(), routeKey, 0, missingRevision, missingGroupID, "fixture-previously-validated"); err != nil {
		t.Fatalf("seed a new Encore-owned route for the missing native revision fixture: %v", err)
	}

	target, _ := url.Parse(nativeE2ECoreURL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxyServer := httptest.NewTLSServer(proxy)
	t.Cleanup(proxyServer.Close)
	roots := x509.NewCertPool()
	roots.AddCert(proxyServer.Certificate())
	checker, err := newEmailSourceCompatibilityChecker(proxyServer.URL, nativeE2EProjToken, roots)
	if err != nil {
		t.Fatal("construct TLS-pinned compatibility checker")
	}
	bridge, err := newSourceBridge(SourceBridgeConfig{Enabled: true, BaseURL: proxyServer.URL, DeploymentID: nativeE2EDeployment, SourceBearerToken: nativeE2ESourceToken, ProjectionBearerToken: nativeE2EProjToken, RequestTimeout: 5 * time.Second}, checker, roots)
	if err != nil {
		checker.Close()
		t.Fatal("construct TLS-pinned source relay")
	}
	t.Cleanup(bridge.Close)
	previousBridge := currentSourceBridge()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(previousBridge) })

	ok, alarmID := GroupApp.Alarm.AddAlarmInfo(configID, "blocked native source fixture")
	if !ok || alarmID == "" {
		t.Fatal("AddAlarmInfo did not persist alarm and source outbox")
	}
	var alarmCount int64
	if err := db.Raw(`SELECT count(*) FROM alarm_info WHERE id = ? AND alarm_config_id = ? AND tenant_id = ?`, alarmID, configID, nativeE2ETenant).Scan(&alarmCount).Error; err != nil || alarmCount != 1 {
		t.Fatal("source alarm row was not retained")
	}
	var outbox dal.SourceOutboxRecord
	if err := db.Where("source_event_id = ? AND tenant_id = ? AND source_deployment_id = ?", alarmID, nativeE2ETenant, nativeE2EDeployment).Take(&outbox).Error; err != nil {
		t.Fatal("source outbox row was not retained")
	}
	if outbox.NotificationGroupID != missingGroupID || outbox.GroupRevision != missingRevision || outbox.State != "pending" {
		t.Fatal("source outbox did not preserve the missing mapped target")
	}
	if err := bridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("relay source event to actual native Core")
	}
	var state string
	if err := db.Raw(`SELECT state FROM notification_source_outbox WHERE id = ?`, outbox.ID).Row().Scan(&state); err != nil || state != "handed_off" {
		t.Fatalf("native durable blocked ACK did not hand off source row: state=%q err=%v", state, err)
	}

	adminJWT := nativeE2EAdminJWT(t)
	client := proxyServer.Client()
	var blockedPage struct {
		Total int64 `json:"total"`
		Items []struct {
			ID            string `json:"id"`
			IntakeStatus  string `json:"intakeStatus"`
			BlockedReason string `json:"blockedReason"`
			Deliveries    []any  `json:"deliveries"`
		} `json:"items"`
	}
	query := url.Values{"sourceType": {"alarm"}, "sourceId": {alarmID}, "intakeStatus": {"blocked"}, "page": {"1"}, "pageSize": {"20"}}
	status := nativeE2EGetJSON(t, client, proxyServer.URL+"/api/v2/notifications?"+query.Encode(), adminJWT, &blockedPage)
	if status != http.StatusOK || blockedPage.Total != 1 || len(blockedPage.Items) != 1 {
		t.Fatalf("native blocked notification query returned HTTP %d with %d rows", status, blockedPage.Total)
	}
	blocked := blockedPage.Items[0]
	if blocked.ID == "" || blocked.IntakeStatus != "blocked" || blocked.BlockedReason != "group_revision_unavailable" || len(blocked.Deliveries) != 0 {
		t.Fatalf("native Core did not preserve safe blocked diagnostics: status=%q reason=%q deliveries=%d", blocked.IntakeStatus, blocked.BlockedReason, len(blocked.Deliveries))
	}
	if fixtureSMTP.count.Load() != 0 {
		t.Fatalf("legacy SMTP fixture sent despite Encore route ownership: sends=%d", fixtureSMTP.count.Load())
	}
	finalNativeCalls := nativeE2EFixtureSendCalls(t)
	if finalNativeCalls != initialNativeCalls {
		t.Fatalf("native SMTP fixture send delta should be zero, before=%d after=%d", initialNativeCalls, finalNativeCalls)
	}
	t.Logf("native blocked fixture: source=alarm blocked_reason=%s deliveries=0 alarm_rows=%d outbox=handed_off local_smtp_delta=0 native_smtp_delta=0 route_validation=fixture_seed", blocked.BlockedReason, alarmCount)
}
