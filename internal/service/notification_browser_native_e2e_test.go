package service

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"project/internal/dal"
	"project/internal/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// This test closes the old alarm producer -> source outbox -> native Core
// loop using IDs created manually through the browser UI. It sends only to
// the approved local Core and its configured mock SMTP adapter.
func TestNativeBrowserAlarmClosesSourceToCoreLoop(t *testing.T) {
	if os.Getenv("NOTIFICATION_NATIVE_E2E") != "1" {
		t.Skip("set NOTIFICATION_NATIVE_E2E=1 to run the browser-created native integration")
	}
	if os.Getenv("NOTIFICATION_TEST_DSN") != nativeE2EDSN {
		t.Fatal("native integration requires the approved isolated PostgreSQL fixture")
	}
	for _, endpoint := range []struct{ raw, port string }{{nativeE2ECoreURL, "19404"}, {nativeE2ESidecarURL, "19583"}} {
		parsed, err := url.Parse(endpoint.raw)
		if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() != endpoint.port {
			t.Fatal("native integration endpoint is outside the approved loopback fixtures")
		}
	}
	fixturePath := os.Getenv("NOTIFICATION_BROWSER_E2E_INPUT")
	fixture, err := readNativeBrowserE2EFixture(fixturePath)
	if err != nil {
		t.Fatalf("read browser-created fixture: %v", err)
	}
	if fixture.SchemaVersion != 1 || fixture.SourceDeploymentID != nativeE2EDeployment || fixture.TenantID != nativeE2ETenant || uuid.Validate(fixture.NativeInstanceID) != nil || uuid.Validate(fixture.NativeGroupID) != nil || fixture.NativeGroupRevision < 1 {
		t.Fatal("browser-created fixture identity is outside the fixed local deployment")
	}
	initialNativeCalls := nativeE2EFixtureSendCalls(t)
	db := openNativeSourceE2EPG(t)
	fixtureSMTP := startCountingFixtureSMTP(t)
	seedNativeBrowserAlarmSource(t, db)

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
	projection := SourceGroupProjectionRequest{
		SourceDeploymentID:  fixture.SourceDeploymentID,
		TenantID:            fixture.TenantID,
		LegacyGroupID:       browserAlarmLegacyGroupID,
		NotificationGroupID: fixture.NativeGroupID,
		GroupRevision:       fixture.NativeGroupRevision,
	}
	snapshot, err := checker.fetchSnapshot(context.Background(), projection)
	if err != nil {
		checker.Close()
		t.Fatal("Core rejected the exact browser-created group snapshot")
	}
	if snapshot.SourceDeploymentID != fixture.SourceDeploymentID || snapshot.TenantID != fixture.TenantID || snapshot.Group.ID != fixture.NativeGroupID || !snapshot.Group.Enabled || snapshot.Group.Revision != fixture.NativeGroupRevision || len(snapshot.Group.Bindings) != 1 || len(snapshot.Instances) != 1 || snapshot.Instances[0].ID != fixture.NativeInstanceID {
		checker.Close()
		t.Fatal("browser-created instance/group snapshot did not match the fixture IDs and revision")
	}
	legacyIdentity, err := nativeBrowserSnapshotSMTPIdentity(snapshot, fixture.NativeInstanceID)
	if err != nil {
		checker.Close()
		t.Fatal("browser-created group does not expose a valid non-secret SMTP identity")
	}
	if err := configureNativeBrowserLegacySMTP(db, legacyIdentity); err != nil {
		checker.Close()
		t.Fatal("seed isolated legacy SMTP identity from the non-secret Core snapshot")
	}
	legacyGroup, err := dal.GetNotificationGroupByTenantID(browserAlarmLegacyGroupID, fixture.TenantID)
	if err != nil || checker.CheckEncoreCompatibility(context.Background(), legacyGroup, projection) != nil {
		checker.Close()
		t.Fatal("browser-created Core group is not fully compatible with the fixture legacy EMAIL group")
	}
	bridge, err := newSourceBridge(SourceBridgeConfig{
		Enabled: true, BaseURL: proxyServer.URL, DeploymentID: fixture.SourceDeploymentID,
		SourceBearerToken: nativeE2ESourceToken, ProjectionBearerToken: nativeE2EProjToken,
		RequestTimeout: 5 * time.Second,
	}, checker, roots)
	if err != nil {
		checker.Close()
		t.Fatal("construct TLS-pinned source bridge")
	}
	t.Cleanup(bridge.Close)
	previousBridge := currentSourceBridge()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(previousBridge) })

	projectionKey := "browser-native-projection-" + uuid.NewString()
	if _, err := bridge.RegisterProjection(context.Background(), projection, projectionKey); err != nil {
		t.Fatal("native Core rejected browser group projection")
	}
	if err := bridge.SwitchToEncore(context.Background(), projection, projectionKey, 0); err != nil {
		t.Fatal("source route did not switch after projection and compatibility checks")
	}
	// After the exact compatibility check and route CAS, point only the isolated
	// legacy sender config at an in-process SMTP sink. If alarm routing regresses,
	// it cannot contact the SMTP identity used by the native browser fixture.
	if err := redirectNativeBrowserLegacySMTPToSink(db, fixtureSMTP); err != nil {
		t.Fatal("redirect isolated legacy SMTP fallback to the in-process sink")
	}

	ok, alarmID := GroupApp.Alarm.AddAlarmInfo(browserAlarmConfigID, "browser E2E fixture event")
	if !ok || alarmID == "" {
		t.Fatal("legacy AddAlarmInfo did not persist the fixture alarm")
	}
	var alarmRows int64
	if err := db.Raw(`SELECT count(*) FROM alarm_info WHERE id = ? AND alarm_config_id = ? AND tenant_id = ?`, alarmID, browserAlarmConfigID, fixture.TenantID).Scan(&alarmRows).Error; err != nil || alarmRows != 1 {
		t.Fatal("legacy AddAlarmInfo alarm row was not retained")
	}
	var outbox dal.SourceOutboxRecord
	if err := db.Where("source_event_id = ? AND tenant_id = ? AND source_deployment_id = ?", alarmID, fixture.TenantID, fixture.SourceDeploymentID).Take(&outbox).Error; err != nil {
		t.Fatal("legacy AddAlarmInfo did not atomically create source outbox")
	}
	if outbox.NotificationGroupID != fixture.NativeGroupID || outbox.GroupRevision != fixture.NativeGroupRevision || outbox.State != "pending" {
		t.Fatal("source outbox did not preserve the UI-created target and exact revision")
	}
	if err := bridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("source relay failed")
	}
	var state string
	if err := db.Raw(`SELECT state FROM notification_source_outbox WHERE id = ?`, outbox.ID).Row().Scan(&state); err != nil || state != "handed_off" {
		t.Fatalf("source event was not durably handed off: state=%q err=%v", state, err)
	}

	client := proxyServer.Client()
	adminJWT := nativeE2EAdminJWT(t)
	query := url.Values{"sourceType": {"alarm"}, "sourceId": {alarmID}, "page": {"1"}, "pageSize": {"20"}}
	var nativePage struct {
		Total int64 `json:"total"`
		Items []struct {
			ID           string `json:"id"`
			IntakeStatus string `json:"intakeStatus"`
			Deliveries   []struct {
				DeliveryID     string `json:"deliveryId"`
				DispatchStatus string `json:"dispatchStatus"`
				DeliveryStatus string `json:"deliveryStatus"`
			} `json:"deliveries"`
		} `json:"items"`
	}
	status := nativeE2EGetJSON(t, client, proxyServer.URL+"/api/v2/notifications?"+query.Encode(), adminJWT, &nativePage)
	if status != http.StatusOK || nativePage.Total != 1 || len(nativePage.Items) != 1 || nativePage.Items[0].ID == "" {
		t.Fatalf("native notification query by old alarm source returned HTTP %d and %d rows", status, nativePage.Total)
	}
	if len(nativePage.Items[0].Deliveries) != 1 || nativePage.Items[0].Deliveries[0].DeliveryID == "" {
		t.Fatal("native Core did not create exactly one delivery for the old alarm")
	}
	dispatchStatus, deliveryStatus := "", ""
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		dispatchStatus, deliveryStatus = nativeE2EReadDelivery(client, proxyServer.URL, adminJWT, nativePage.Items[0].ID, nativePage.Items[0].Deliveries[0].DeliveryID)
		if dispatchStatus == "accepted" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if dispatchStatus != "accepted" {
		t.Fatalf("native Core mock did not accept the delivery: dispatch=%q delivery=%q", dispatchStatus, deliveryStatus)
	}
	finalNativeCalls := nativeE2EFixtureSendCalls(t)
	if finalNativeCalls != initialNativeCalls+1 {
		t.Fatalf("exclusive fixture mock delta should be one for this delivery: before=%d after=%d", initialNativeCalls, finalNativeCalls)
	}
	if fixtureSMTP.count.Load() != 0 {
		t.Fatalf("old legacy SMTP fallback was unexpectedly used: local_sink_delta=%d", fixtureSMTP.count.Load())
	}
	t.Logf("browser E2E fixture: source_event_id=%s notification_id=%s group_revision=%d alarm_rows=%d outbox_state=%s native_mock_delta=1 dispatch=%s delivery=%s legacy_sink_delta=0", alarmID, nativePage.Items[0].ID, fixture.NativeGroupRevision, alarmRows, state, dispatchStatus, deliveryStatus)
}

const (
	browserAlarmConfigID      = "fixture-browser-e2e-alarm-config"
	browserAlarmLegacyGroupID = "fixture-browser-e2e-legacy-group"
)

func seedNativeBrowserAlarmSource(t *testing.T, db *gorm.DB) {
	t.Helper()
	seedNativeLegacyEmail(t, db)
	if err := db.Exec(`CREATE TABLE alarm_config (
		id text PRIMARY KEY, name text NOT NULL, description text, alarm_level text NOT NULL,
		notification_group_id text NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
		tenant_id text NOT NULL, remark text, enabled text NOT NULL)`).Error; err != nil {
		t.Fatal("create isolated alarm config table")
	}
	legacyRecipients := `{"EMAIL":"fixture@example.test"}`
	if err := db.Exec(`INSERT INTO notification_groups (id, name, notification_type, status, notification_config, tenant_id, created_at, updated_at)
		VALUES (?, ?, 'EMAIL', 'OPEN', ?, ?, now(), now())`, browserAlarmLegacyGroupID, "browser E2E legacy EMAIL group", legacyRecipients, nativeE2ETenant).Error; err != nil {
		t.Fatal("create isolated legacy EMAIL group")
	}
	if err := db.Exec(`INSERT INTO alarm_config (id, name, description, alarm_level, notification_group_id, created_at, updated_at, tenant_id, enabled)
		VALUES (?, ?, ?, 'H', ?, now(), now(), ?, 'Y')`, browserAlarmConfigID, "browser E2E alarm", "local-only fixture", browserAlarmLegacyGroupID, nativeE2ETenant).Error; err != nil {
		t.Fatal("create isolated alarm config")
	}
}

func configureNativeBrowserLegacySMTP(db *gorm.DB, identity model.EmailConfig) error {
	legacySMTP, err := json.Marshal(struct {
		Host         string `json:"host"`
		Port         int    `json:"port"`
		FromPassword string `json:"from_password"`
		FromEmail    string `json:"from_email"`
		SSL          bool   `json:"ssl"`
	}{Host: identity.Host, Port: identity.Port, FromPassword: "FIXTURE_SMTP_SECRET", FromEmail: identity.FromEmail, SSL: identity.SSL != nil && *identity.SSL})
	if err != nil {
		return err
	}
	if err := db.Exec(`UPDATE notification_services_config SET config = ? WHERE id = ?`, string(legacySMTP), "legacy-email-config").Error; err != nil {
		return err
	}
	return nil
}

func redirectNativeBrowserLegacySMTPToSink(db *gorm.DB, sink *countingSMTPFixture) error {
	legacySMTP, err := json.Marshal(struct {
		Host         string `json:"host"`
		Port         int    `json:"port"`
		FromPassword string `json:"from_password"`
		FromEmail    string `json:"from_email"`
		SSL          bool   `json:"ssl"`
	}{Host: "127.0.0.1", Port: sink.port, FromPassword: "fixture-only-password", FromEmail: "fixture@example.test", SSL: false})
	if err != nil {
		return err
	}
	return db.Exec(`UPDATE notification_services_config SET config = ? WHERE id = ?`, string(legacySMTP), "legacy-email-config").Error
}

func nativeBrowserSnapshotSMTPIdentity(snapshot sourceGroupSnapshot, expectedInstanceID string) (model.EmailConfig, error) {
	for _, instance := range snapshot.Instances {
		if instance.ID != expectedInstanceID {
			continue
		}
		var identity model.EmailConfig
		if json.Unmarshal(instance.Config["host"], &identity.Host) != nil || json.Unmarshal(instance.Config["port"], &identity.Port) != nil || json.Unmarshal(instance.Config["from_email"], &identity.FromEmail) != nil {
			return model.EmailConfig{}, errors.New("native SMTP identity unavailable")
		}
		identity.FromPassword = "fixture-only-password"
		if raw, ok := instance.Config["ssl"]; ok {
			var enabled bool
			if json.Unmarshal(raw, &enabled) != nil {
				return model.EmailConfig{}, errors.New("native SMTP identity unavailable")
			}
			identity.SSL = &enabled
		}
		if identity.Host == "" || identity.Port < 1 || identity.Port > 65535 || strings.TrimSpace(identity.FromEmail) != identity.FromEmail || identity.FromEmail == "" {
			return model.EmailConfig{}, errors.New("native SMTP identity invalid")
		}
		return identity, nil
	}
	return model.EmailConfig{}, errors.New("native SMTP instance missing")
}
