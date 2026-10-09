package service

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"project/internal/dal"
	"project/internal/model"
	"project/pkg/global"

	"github.com/google/uuid"
)

func TestAlarmProducerSendsLegacyOnlyAndSuppressesOnSourcePersistenceFailure(t *testing.T) {
	openSourceCompatPGFixture(t)
	db := global.DB
	if err := db.Exec(`CREATE TABLE alarm_config (
		id text PRIMARY KEY, name text NOT NULL, description text, alarm_level text NOT NULL,
		notification_group_id text NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
		tenant_id text NOT NULL, remark text, enabled text NOT NULL)`).Error; err != nil {
		t.Fatal("create isolated alarm config table")
	}
	if err := db.Exec(`CREATE TABLE alarm_info (
		id text PRIMARY KEY, alarm_config_id text NOT NULL, name text NOT NULL, alarm_time timestamptz NOT NULL,
		description text, content text, processor text, processing_result text NOT NULL, tenant_id text NOT NULL,
		remark text, alarm_level text)`).Error; err != nil {
		t.Fatal("create isolated alarm info table")
	}
	if err := db.Exec(`CREATE TABLE notification_histories (
		id text PRIMARY KEY, send_time timestamptz NOT NULL, send_content text, send_target text NOT NULL,
		send_result text, notification_type text NOT NULL, tenant_id text NOT NULL, remark text)`).Error; err != nil {
		t.Fatal("create isolated notification history table")
	}
	for _, groupID := range []string{"native-owned", "native-failing"} {
		config := `{"EMAIL":"ops@example.test"}`
		if err := db.Exec(`INSERT INTO notification_groups (id, name, notification_type, status, notification_config, tenant_id, created_at, updated_at)
			VALUES (?, ?, 'EMAIL', 'OPEN', ?, 'tenant-a', now(), now())`, groupID, groupID, config).Error; err != nil {
			t.Fatal("create isolated source group")
		}
	}
	for _, row := range []struct{ id, groupID string }{
		{"legacy-alarm", "legacy-a"}, {"encore-alarm", "native-owned"}, {"failing-alarm", "native-failing"},
	} {
		if err := db.Exec(`INSERT INTO alarm_config (id, name, alarm_level, notification_group_id, created_at, updated_at, tenant_id, enabled)
			VALUES (?, ?, 'H', ?, now(), now(), 'tenant-a', 'Y')`, row.id, row.id, row.groupID).Error; err != nil {
			t.Fatal("seed isolated alarm config")
		}
	}
	if err := dal.SwitchSourceRoute(context.Background(), dal.SourceRouteKey{DeploymentID: "deployment-test", TenantID: "tenant-a", LegacyGroup: "native-owned"}, 0, 3, "native-group", "projection-owned-0001"); err != nil {
		t.Fatal("make source group Encore-owned")
	}
	if err := dal.SwitchSourceRoute(context.Background(), dal.SourceRouteKey{DeploymentID: "deployment-test", TenantID: "tenant-a", LegacyGroup: "native-failing"}, 0, 5, "native-group-failing", "projection-fail-0001"); err != nil {
		t.Fatal("make SQL-failure group Encore-owned")
	}

	smtp := startCountingFixtureSMTP(t)
	legacySMTP, _ := json.Marshal(model.EmailConfig{Host: smtp.host, Port: smtp.port, FromPassword: "fixture-only-password", FromEmail: "sender@example.test"})
	if err := db.Exec(`UPDATE notification_services_config SET config = ? WHERE id = ?`, string(legacySMTP), "email-config-a").Error; err != nil {
		t.Fatal("configure isolated legacy SMTP endpoint")
	}

	coreRequests := atomic.Int32{}
	core := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		coreRequests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer core.Close()
	bridge := testSourceBridge(t, core)
	defer bridge.Close()
	previousBridge := currentSourceBridge()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(previousBridge) })

	if ok, _ := GroupApp.Alarm.AddAlarmInfo("legacy-alarm", "legacy fixture"); !ok {
		t.Fatal("legacy engine did not persist alarm")
	}
	if !smtp.waitForCount(1, 3*time.Second) {
		t.Fatal("legacy-owned alarm did not reach local SMTP")
	}

	// A failed transactional outbox insert must roll back the alarm and never
	// fall through to the legacy sender.
	if err := db.Exec(`ALTER TABLE notification_source_outbox ADD CONSTRAINT qa_reject_pending CHECK (state <> 'pending')`).Error; err != nil {
		t.Fatal("install isolated outbox failure constraint")
	}
	if ok, _ := GroupApp.Alarm.AddAlarmInfo("failing-alarm", "must roll back"); ok {
		t.Fatal("alarm with failed source transaction reported success")
	}
	if err := db.Exec(`ALTER TABLE notification_source_outbox DROP CONSTRAINT qa_reject_pending`).Error; err != nil {
		t.Fatal("remove isolated outbox failure constraint")
	}
	var failedAlarmCount, failedOutboxCount int64
	if err := db.Raw(`SELECT count(*) FROM alarm_info WHERE alarm_config_id = 'failing-alarm'`).Scan(&failedAlarmCount).Error; err != nil {
		t.Fatal("count rolled back alarm")
	}
	if err := db.Raw(`SELECT count(*) FROM notification_source_outbox WHERE legacy_group_id = 'native-failing'`).Scan(&failedOutboxCount).Error; err != nil {
		t.Fatal("count rolled back outbox")
	}
	if failedAlarmCount != 0 || failedOutboxCount != 0 {
		t.Fatalf("source transaction partially persisted: alarm=%d outbox=%d", failedAlarmCount, failedOutboxCount)
	}

	ok, sourceAlarmID := GroupApp.Alarm.AddAlarmInfo("encore-alarm", "Encore-owned fixture")
	if !ok {
		t.Fatal("Encore-owned alarm was not durably accepted by the source outbox")
	}
	var outbox dal.SourceOutboxRecord
	if err := db.Where("source_event_id = ? AND legacy_group_id = ?", sourceAlarmID, "native-owned").Take(&outbox).Error; err != nil {
		t.Fatal("read frozen Encore-owned alarm source event")
	}
	if err := bridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("relay unavailable Core fixture")
	}
	var state string
	var attempts int
	if err := db.Raw(`SELECT state, attempts FROM notification_source_outbox WHERE legacy_group_id = 'native-owned'`).Row().Scan(&state, &attempts); err != nil || state != "retry_wait" || attempts != 1 {
		t.Fatalf("Core outage did not retain Encore-owned event: state=%q attempts=%d", state, attempts)
	}
	if coreRequests.Load() != 1 {
		t.Fatalf("expected only the Encore relay to reach Core fixture, requests=%d", coreRequests.Load())
	}
	// Preserve representative queued, in-flight, and terminal source rows on
	// this same immutable Encore tuple before rolling future events back.
	now := time.Now().UTC().Truncate(time.Microsecond)
	insertHistoricalSourceState := func(state string) string {
		t.Helper()
		id, eventID, actionID, idemKey := uuid.NewString(), uuid.NewString(), uuid.NewString(), "rollback-"+uuid.NewString()
		var leaseToken any
		var leaseUntil any
		var handedOffAt any
		switch state {
		case "leased":
			leaseToken, leaseUntil = uuid.NewString(), now.Add(time.Minute)
		case "handed_off":
			handedOffAt = now
		}
		if err := db.Exec(`INSERT INTO notification_source_outbox
			(id, source_deployment_id, tenant_id, source_event_id, source_action_id, legacy_group_id, notification_group_id,
			 group_revision, idempotency_key, request_body, body_sha256, occurred_at, expires_at, state, attempts, next_attempt_at,
			 lease_token, lease_until, handed_off_at)
			VALUES (?, ?, ?, ?, ?, 'native-owned', 'native-group', 3, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`, id, bridge.DeploymentID(), "tenant-a",
			eventID, actionID, idemKey, []byte(`{"historicalFixture":true}`), strings.Repeat("b", 64), now, now.Add(time.Hour), state, now, leaseToken, leaseUntil, handedOffAt).Error; err != nil {
			t.Fatalf("insert historical %s source row", state)
		}
		return id
	}
	pendingID := insertHistoricalSourceState("pending")
	leasedID := insertHistoricalSourceState("leased")
	handedOffID := insertHistoricalSourceState("handed_off")
	if count := smtp.count.Load(); count != 1 {
		t.Fatalf("source SQL failure or Core outage fell back to old SMTP sender: fixture sends=%d", count)
	}
	if err := dal.SwitchSourceRouteToLegacy(context.Background(), dal.SourceRouteKey{DeploymentID: "deployment-test", TenantID: "tenant-a", LegacyGroup: "native-owned"}, 1); err != nil {
		t.Fatal("rollback future source ownership to legacy")
	}
	if ok, _ := GroupApp.Alarm.AddAlarmInfo("encore-alarm", "future legacy-owned fixture"); !ok {
		t.Fatal("post-rollback legacy alarm did not persist")
	}
	if !smtp.waitForCount(2, 3*time.Second) {
		t.Fatal("future alarm did not use the explicitly restored legacy sender")
	}
	var historicalState string
	if err := db.Raw(`SELECT state FROM notification_source_outbox WHERE source_event_id = ? AND legacy_group_id = 'native-owned'`, sourceAlarmID).Row().Scan(&historicalState); err != nil || historicalState != "retry_wait" {
		t.Fatalf("rollback rewrote or discarded the historical Encore-owned outbox: state=%q err=%v", historicalState, err)
	}
	for id, want := range map[string]string{pendingID: "pending", leasedID: "leased", handedOffID: "handed_off"} {
		var state, target string
		if err := db.Raw(`SELECT state, notification_group_id FROM notification_source_outbox WHERE id = ?`, id).Row().Scan(&state, &target); err != nil || state != want || target != "native-group" {
			t.Fatalf("rollback changed historical %s source ownership: state=%q target=%q err=%v", want, state, target, err)
		}
	}
	if count := smtp.count.Load(); count != 2 {
		t.Fatalf("historical Encore event was resent by the legacy sender: fixture sends=%d", count)
	}
}

type countingSMTPFixture struct {
	host  string
	port  int
	count atomic.Int32
}

func startCountingFixtureSMTP(t *testing.T) *countingSMTPFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("start isolated multi-session SMTP fixture")
	}
	t.Cleanup(func() { _ = listener.Close() })
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	fixture := &countingSMTPFixture{host: "127.0.0.1", port: port}
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go fixture.serve(conn)
		}
	}()
	return fixture
}

func (f *countingSMTPFixture) serve(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	write := func(value string) bool {
		_, err := io.WriteString(conn, value)
		return err == nil
	}
	if !write("220 fixture.local ESMTP\r\n") {
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO ") || strings.HasPrefix(command, "HELO "):
			if !write("250-fixture.local\r\n250 AUTH PLAIN\r\n") {
				return
			}
		case command == "AUTH PLAIN":
			if !write("334 \r\n") {
				return
			}
			if _, err = reader.ReadString('\n'); err != nil || !write("235 2.7.0 authenticated\r\n") {
				return
			}
		case strings.HasPrefix(command, "AUTH PLAIN "):
			if !write("235 2.7.0 authenticated\r\n") {
				return
			}
		case strings.HasPrefix(command, "MAIL FROM:") || strings.HasPrefix(command, "RCPT TO:") || command == "RSET":
			if !write("250 2.1.0 ok\r\n") {
				return
			}
		case command == "DATA":
			if !write("354 end with dot\r\n") {
				return
			}
			for {
				dataLine, dataErr := reader.ReadString('\n')
				if dataErr != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
			}
			f.count.Add(1)
			if !write("250 2.0.0 queued\r\n") {
				return
			}
		case command == "QUIT":
			_ = write("221 2.0.0 bye\r\n")
			return
		default:
			if !write("250 2.0.0 ok\r\n") {
				return
			}
		}
	}
}

func (f *countingSMTPFixture) waitForCount(want int32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f.count.Load() >= want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return f.count.Load() >= want
}
