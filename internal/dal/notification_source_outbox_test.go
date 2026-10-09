package dal

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"project/internal/model"
	"project/pkg/global"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const approvedNotificationTestDSN = "postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test"

func openNotificationSourceFixture(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_DSN fixture not supplied")
	}
	if dsn != approvedNotificationTestDSN {
		t.Fatal("refusing non-approved notification fixture DSN")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("could not open approved notification fixture")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("could not access fixture connection pool")
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	var database, user string
	if err := db.Raw(`SELECT current_database(), current_user`).Row().Scan(&database, &user); err != nil || database != "notification_test" || user != "notification_test" {
		t.Fatal("fixture connection did not match approved database identity")
	}
	schema := "t07_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("could not create isolated schema")
	}
	previous := global.DB
	t.Cleanup(func() {
		global.DB = previous
		_ = db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		if pool, poolErr := db.DB(); poolErr == nil {
			_ = pool.Close()
		}
	})
	_ = sqlDB.Close()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("could not parse approved fixture")
	}
	query := parsed.Query()
	query.Set("options", "-c search_path="+schema)
	parsed.RawQuery = query.Encode()
	db, err = gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("could not open isolated fixture schema")
	}
	sqlDB, err = db.DB()
	if err != nil {
		t.Fatal("could not access isolated fixture pool")
	}
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	if err := db.Exec(`CREATE TABLE alarm_info (
		id text PRIMARY KEY, alarm_config_id text NOT NULL, name text NOT NULL,
		alarm_time timestamptz NOT NULL, description text, content text, processor text,
		processing_result text NOT NULL, tenant_id text NOT NULL, remark text, alarm_level text)`).Error; err != nil {
		t.Fatal("could not create fixture alarm table")
	}
	if err := db.Exec(`CREATE TABLE alarm_history (
		id text PRIMARY KEY, alarm_config_id text NOT NULL, group_id text NOT NULL,
		scene_automation_id text NOT NULL, name text NOT NULL, description text, content text,
		alarm_status text NOT NULL, tenant_id text NOT NULL, remark text, create_at timestamptz NOT NULL,
		alarm_device_list text NOT NULL)`).Error; err != nil {
		t.Fatal("could not create fixture history table")
	}
	migration, err := os.ReadFile("../../sql/23.sql")
	if err != nil {
		t.Fatal("could not read source migration")
	}
	migrationSQL := strings.ReplaceAll(string(migration), "public.", `"`+schema+`".`)
	lines := strings.Split(migrationSQL, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			filtered = append(filtered, line)
		}
	}
	migrationSQL = strings.Join(filtered, "\n")
	for _, statement := range strings.Split(migrationSQL, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal("could not apply source migration in isolated schema")
		}
	}
	migration24, err := os.ReadFile("../../sql/24.sql")
	if err != nil {
		t.Fatal("could not read source rollback migration")
	}
	migration24SQL := strings.ReplaceAll(string(migration24), "public.", `"`+schema+`".`)
	if err := db.Exec(migration24SQL).Error; err != nil {
		t.Fatal("could not apply source rollback migration in isolated schema")
	}
	global.DB = db
	return db
}

func TestSourceRouteFirstInsertSharesAdvisoryLockWithEventSnapshot(t *testing.T) {
	db := openNotificationSourceFixture(t)
	ctx := context.Background()
	key := SourceRouteKey{DeploymentID: "deploy-lock", TenantID: "tenant-lock", LegacyGroup: "legacy-lock"}
	locked := make(chan struct{})
	release := make(chan struct{})
	eventFinished := make(chan struct{})
	var eventSnapshot SourceRouteSnapshot
	var eventErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		eventSnapshot, eventErr = WithLockedSourceRoute(ctx, key, func(_ *gorm.DB, snapshot SourceRouteSnapshot) error {
			close(locked)
			<-release
			return nil
		})
		close(eventFinished)
	}()
	select {
	case <-locked:
	case <-eventFinished:
		t.Fatalf("initial event lock failed: %v", eventErr)
	}
	finished := make(chan error, 1)
	go func() { finished <- SwitchSourceRoute(ctx, key, 0, 1, "encore-lock", "projection-lock-01") }()
	select {
	case err := <-finished:
		t.Fatalf("route create bypassed tuple lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if eventErr != nil || eventSnapshot.Engine != "legacy" {
		t.Fatalf("first event snapshot=%+v error=%v", eventSnapshot, eventErr)
	}
	if err := <-finished; err != nil {
		t.Fatalf("first route CAS failed: %v", err)
	}
	if err := ValidateSourceBridgeStartup(false, ""); err == nil {
		t.Fatal("startup allowed existing Encore route without relay")
	}
	if err := ValidateSourceBridgeStartup(true, "another-deployment"); err == nil {
		t.Fatal("startup allowed wrong deployment identity")
	}
	if err := ValidateSourceBridgeStartup(true, key.DeploymentID); err != nil {
		t.Fatalf("matching relay startup rejected: %v", err)
	}
	var count int64
	if err := db.Raw(`SELECT count(*) FROM notification_source_group_routes WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND engine = 'encore'`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("route row count=%d err=%v", count, err)
	}
	var next SourceRouteSnapshot
	next, err := WithLockedSourceRoute(ctx, key, func(_ *gorm.DB, snapshot SourceRouteSnapshot) error { return nil })
	if err != nil || next.Engine != "encore" || next.NotificationGroupID != "encore-lock" || next.RouteVersion != 1 {
		t.Fatalf("next event route=%+v err=%v", next, err)
	}
}

func TestSourceAlarmAndOutboxRollbackTogetherOnOutboxConstraintFailure(t *testing.T) {
	db := openNotificationSourceFixture(t)
	ctx := context.Background()
	key := SourceRouteKey{DeploymentID: "deploy-rollback", TenantID: "tenant-rollback", LegacyGroup: "legacy-rollback"}
	if err := SwitchSourceRoute(ctx, key, 0, 1, "encore-rollback", "projection-rollback"); err != nil {
		t.Fatal("create Encore route")
	}
	now := time.Now().UTC()
	alarm := &model.AlarmInfo{ID: "alarm-rollback", AlarmConfigID: "cfg", Name: "alarm", AlarmTime: now, ProcessingResult: "UND", TenantID: key.TenantID}
	_, err := SaveAlarmInfoWithSource(ctx, alarm, key, func(route SourceRouteSnapshot) (*SourceOutboxRecord, error) {
		return &SourceOutboxRecord{ID: uuid.NewString(), SourceDeploymentID: key.DeploymentID, TenantID: key.TenantID, SourceEventID: alarm.ID, SourceActionID: "action-rollback", LegacyGroupID: key.LegacyGroup, NotificationGroupID: route.NotificationGroupID, GroupRevision: route.GroupRevision, IdempotencyKey: "alarm-rollback-key", RequestBody: []byte(`{}`), BodySHA256: strings.Repeat("a", 64), OccurredAt: now, ExpiresAt: now.Add(time.Hour), State: "invalid-state", NextAttemptAt: now}, nil
	})
	if err == nil {
		t.Fatal("invalid outbox state unexpectedly committed")
	}
	var alarmCount, outboxCount int64
	if err := db.Raw(`SELECT count(*) FROM alarm_info WHERE id = ?`, alarm.ID).Scan(&alarmCount).Error; err != nil {
		t.Fatal("count alarm row")
	}
	if err := db.Raw(`SELECT count(*) FROM notification_source_outbox WHERE source_event_id = ?`, alarm.ID).Scan(&outboxCount).Error; err != nil {
		t.Fatal("count outbox row")
	}
	if alarmCount != 0 || outboxCount != 0 {
		t.Fatalf("partial transaction committed alarm=%d outbox=%d", alarmCount, outboxCount)
	}
}

func TestSourceRouteUnavailablePreservesAlarmAndSuppressesRouteGuess(t *testing.T) {
	db := openNotificationSourceFixture(t)
	if err := db.Exec(`DROP TABLE notification_source_group_route_revisions CASCADE`).Error; err != nil {
		t.Fatal("drop isolated projection table")
	}
	if err := db.Exec(`DROP TABLE notification_source_outbox CASCADE`).Error; err != nil {
		t.Fatal("drop isolated outbox table")
	}
	if err := db.Exec(`DROP TABLE notification_source_group_routes CASCADE`).Error; err != nil {
		t.Fatal("drop isolated route table")
	}
	key := SourceRouteKey{DeploymentID: "deploy-missing", TenantID: "tenant-missing", LegacyGroup: "legacy-missing"}
	alarm := &model.AlarmInfo{ID: "alarm-missing-route", AlarmConfigID: "cfg", Name: "alarm", AlarmTime: time.Now().UTC(), ProcessingResult: "UND", TenantID: key.TenantID}
	buildCalled := false
	_, err := SaveAlarmInfoWithSource(context.Background(), alarm, key, func(SourceRouteSnapshot) (*SourceOutboxRecord, error) {
		buildCalled = true
		return nil, nil
	})
	if !errors.Is(err, ErrSourceRouteUnavailable) || buildCalled {
		t.Fatalf("missing schema was treated as route: err=%v buildCalled=%v", err, buildCalled)
	}
	remark := "source_route_unavailable"
	alarm.Remark = &remark
	if err := SaveAlarmInfoQuietly(context.Background(), alarm); err != nil {
		t.Fatal("alarm preservation failed")
	}
	var got string
	if err := db.Raw(`SELECT remark FROM alarm_info WHERE id = ?`, alarm.ID).Row().Scan(&got); err != nil || got != remark {
		t.Fatalf("safe source diagnostic was not persisted: %q err=%v", got, err)
	}
}

func TestSourceRouteCASRejectsStaleDowngradeAndTargetRebind(t *testing.T) {
	openNotificationSourceFixture(t)
	ctx := context.Background()
	key := SourceRouteKey{DeploymentID: "deploy-cas", TenantID: "tenant-cas", LegacyGroup: "legacy-cas"}
	if err := SwitchSourceRoute(ctx, key, 0, 4, "encore-cas", "projection-cas-001"); err != nil {
		t.Fatal("initial route CAS")
	}
	if err := SwitchSourceRoute(ctx, key, 0, 5, "encore-cas", "projection-cas-002"); !errors.Is(err, ErrSourceRouteConflict) {
		t.Fatalf("stale expected version accepted: %v", err)
	}
	if err := SwitchSourceRoute(ctx, key, 1, 3, "encore-cas", "projection-cas-003"); !errors.Is(err, ErrSourceRouteConflict) {
		t.Fatalf("revision downgrade accepted: %v", err)
	}
	if err := SwitchSourceRoute(ctx, key, 1, 5, "encore-rebound", "projection-cas-004"); !errors.Is(err, ErrSourceRouteConflict) {
		t.Fatalf("tuple target rebind accepted: %v", err)
	}
	if err := SwitchSourceRoute(ctx, key, 1, 5, "encore-cas", "projection-cas-005"); err != nil {
		t.Fatalf("valid projection revision update rejected: %v", err)
	}
}

func TestSourceOutboxClaimUsesUniqueLeases(t *testing.T) {
	db := openNotificationSourceFixture(t)
	ctx := context.Background()
	key := SourceRouteKey{DeploymentID: "deploy-claim", TenantID: "tenant-claim", LegacyGroup: "legacy-claim"}
	if err := SwitchSourceRoute(ctx, key, 0, 1, "encore-claim", "projection-claim-1"); err != nil {
		t.Fatal("create route")
	}
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		row := SourceOutboxRecord{ID: uuid.NewString(), SourceDeploymentID: key.DeploymentID, TenantID: key.TenantID, SourceEventID: "claim-event-" + string(rune('0'+i)), SourceActionID: uuid.NewString(), LegacyGroupID: key.LegacyGroup, NotificationGroupID: "encore-claim", GroupRevision: 1, IdempotencyKey: "claim-key-" + string(rune('a'+i)), RequestBody: []byte(`{}`), BodySHA256: strings.Repeat("b", 64), OccurredAt: now, ExpiresAt: now.Add(time.Hour), State: "pending", NextAttemptAt: now}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal("insert claim fixture")
		}
	}
	var wg sync.WaitGroup
	results := make(chan []SourceOutboxRecord, 3)
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := ClaimSourceOutbox(ctx, 1, time.Minute, 4, time.Now().UTC())
			results <- rows
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	seen := map[string]bool{}
	for err := range errs {
		if err != nil {
			t.Fatalf("claim failed: %v", err)
		}
	}
	for rows := range results {
		if len(rows) != 1 || rows[0].LeaseToken == nil {
			t.Fatalf("claim result missing lease: %+v", rows)
		}
		if seen[rows[0].ID] {
			t.Fatalf("outbox row claimed twice: %s", rows[0].ID)
		}
		seen[rows[0].ID] = true
		result := SourceAttemptResult{StatusCode: 503, SafeCode: "relay_retry"}
		if rows[0].SourceEventID == "claim-event-0" {
			result = SourceAttemptResult{StatusCode: 202, Accepted: true}
		}
		if rows[0].SourceEventID == "claim-event-2" {
			result = SourceAttemptResult{StatusCode: 403, SafeCode: "source_scope_mismatch"}
		}
		if err := FinishSourceOutboxAttempt(ctx, rows[0], result, time.Now().UTC(), 4); err != nil {
			t.Fatalf("finish claim: %v", err)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("claimed %d unique rows", len(seen))
	}
	var handedOff, retryWait, blocked int64
	if err := db.Raw(`SELECT count(*) FROM notification_source_outbox WHERE state = 'handed_off' AND handed_off_at IS NOT NULL AND lease_token IS NULL`).Scan(&handedOff).Error; err != nil {
		t.Fatal("read handed-off state")
	}
	if err := db.Raw(`SELECT count(*) FROM notification_source_outbox WHERE state = 'retry_wait' AND next_attempt_at > ? AND lease_token IS NULL AND failure_code = 'relay_retry'`, time.Now().Add(-time.Second)).Scan(&retryWait).Error; err != nil {
		t.Fatal("read retry state")
	}
	if err := db.Raw(`SELECT count(*) FROM notification_source_outbox WHERE state = 'blocked' AND failure_code = 'source_scope_mismatch' AND lease_token IS NULL`).Scan(&blocked).Error; err != nil {
		t.Fatal("read blocked state")
	}
	if handedOff != 1 || retryWait != 1 || blocked != 1 {
		t.Fatalf("relay outcome states handed_off=%d retry_wait=%d blocked=%d", handedOff, retryWait, blocked)
	}
}

func TestSourceOutboxIdentityIncludesDeploymentTenantEventAndAction(t *testing.T) {
	db := openNotificationSourceFixture(t)
	now := time.Now().UTC()
	rows := []SourceOutboxRecord{
		{ID: uuid.NewString(), SourceDeploymentID: "deployment-a", TenantID: "tenant-a", SourceEventID: "shared-event", SourceActionID: "shared-action", LegacyGroupID: "legacy", NotificationGroupID: "encore", GroupRevision: 1, IdempotencyKey: "identity-a-0001", RequestBody: []byte(`{}`), BodySHA256: strings.Repeat("a", 64), OccurredAt: now, ExpiresAt: now.Add(time.Hour), State: "pending", NextAttemptAt: now},
		{ID: uuid.NewString(), SourceDeploymentID: "deployment-a", TenantID: "tenant-b", SourceEventID: "shared-event", SourceActionID: "shared-action", LegacyGroupID: "legacy", NotificationGroupID: "encore", GroupRevision: 1, IdempotencyKey: "identity-b-0001", RequestBody: []byte(`{}`), BodySHA256: strings.Repeat("b", 64), OccurredAt: now, ExpiresAt: now.Add(time.Hour), State: "pending", NextAttemptAt: now},
		{ID: uuid.NewString(), SourceDeploymentID: "deployment-b", TenantID: "tenant-a", SourceEventID: "shared-event", SourceActionID: "shared-action", LegacyGroupID: "legacy", NotificationGroupID: "encore", GroupRevision: 1, IdempotencyKey: "identity-c-0001", RequestBody: []byte(`{}`), BodySHA256: strings.Repeat("c", 64), OccurredAt: now, ExpiresAt: now.Add(time.Hour), State: "pending", NextAttemptAt: now},
		{ID: uuid.NewString(), SourceDeploymentID: "deployment-a", TenantID: "tenant-a", SourceEventID: "shared-event", SourceActionID: "other-action", LegacyGroupID: "legacy", NotificationGroupID: "encore", GroupRevision: 1, IdempotencyKey: "identity-d-0001", RequestBody: []byte(`{}`), BodySHA256: strings.Repeat("d", 64), OccurredAt: now, ExpiresAt: now.Add(time.Hour), State: "pending", NextAttemptAt: now},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("insert independent identity %d: %v", i, err)
		}
	}
	duplicate := rows[0]
	duplicate.ID = uuid.NewString()
	duplicate.IdempotencyKey = "identity-dup-0001"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate full deployment/tenant/event/action tuple accepted")
	}
}
