package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"project/internal/dal"
	"project/pkg/global"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const approvedNativePublishDSN = "postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test"

func TestNativePublishPublishReplayStopAndResumeAreTenantScoped(t *testing.T) {
	db := openNativePublishFixture(t)
	var projectionCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, sourceGroupSnapshotPath) {
			writeNativePublishSnapshotFixture(w, r, true)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != sourceProjectPath || r.Header.Get("Authorization") != "Bearer projection-test-token" {
			t.Errorf("unexpected projection request: method=%s path=%s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		var request SourceGroupProjectionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode projection request")
			http.Error(w, "invalid", http.StatusBadRequest)
			return
		}
		projectionCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sourceEnvelope[SourceGroupProjection]{
			Code: 200, Message: "Success", RequestID: "fixture-request",
			Data: SourceGroupProjection{SourceDeploymentID: request.SourceDeploymentID, TenantID: request.TenantID,
				LegacyGroupID: request.LegacyGroupID, NotificationGroupID: request.NotificationGroupID,
				GroupRevision: request.GroupRevision, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		})
	}))
	defer server.Close()
	bridge := testSourceBridge(t, server)
	defer bridge.Close()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(nil) })

	ctx := context.Background()
	request := NativePublishRequest{TenantID: "tenant-native-a", NativeGroupID: "native-group-0001", GroupRevision: 1,
		Name: "Alarm notifications", ExpectedRouteVersion: 0, IdempotencyKey: "publish-native-key-0001"}
	first, replayed, err := ApplyNativePublish(ctx, request)
	if err != nil || replayed || !first.Published || first.Status != "published" || first.RouteVersion != 1 || first.EffectiveGroupRevision != 1 || !first.Enabled {
		t.Fatalf("first native publish failed: status=%+v replayed=%v err=%v", first, replayed, err)
	}
	if projectionCalls.Load() != 1 {
		t.Fatalf("first publish projection calls=%d, want 1", projectionCalls.Load())
	}

	replay, replayed, err := ApplyNativePublish(ctx, request)
	if err != nil || !replayed || replay.RouteVersion != first.RouteVersion || projectionCalls.Load() != 1 {
		t.Fatalf("same target/revision replay was not stable: status=%+v replayed=%v calls=%d err=%v", replay, replayed, projectionCalls.Load(), err)
	}

	key := nativePublishRouteKey(bridge.DeploymentID(), request.TenantID, request.NativeGroupID)
	var outbox dal.SourceOutboxRecord
	now := time.Now().UTC()
	outbox = dal.SourceOutboxRecord{ID: uuid.NewString(), SourceDeploymentID: key.DeploymentID, TenantID: key.TenantID,
		SourceEventID: "event-native-stop-0001", SourceActionID: "action-native-stop-01", LegacyGroupID: key.LegacyGroup,
		NotificationGroupID: request.NativeGroupID, GroupRevision: 1, IdempotencyKey: "event-native-stop-key-0001",
		RequestBody: []byte(`{"fixture":true}`), BodySHA256: strings.Repeat("a", 64), OccurredAt: now,
		ExpiresAt: now.Add(time.Hour), State: "pending", NextAttemptAt: now}
	if err := db.Create(&outbox).Error; err != nil {
		t.Fatal("seed pending source event")
	}

	stopped, replayed, err := ApplyNativePublish(ctx, NativePublishRequest{Operation: "unpublish", TenantID: request.TenantID,
		NativeGroupID: request.NativeGroupID, ExpectedRouteVersion: first.RouteVersion, IdempotencyKey: "stop-native-key-0001"})
	if err != nil || replayed || stopped.Published || stopped.Status != "stopped" || stopped.Engine != "legacy" || stopped.Enabled || stopped.EffectiveGroupRevision != 0 || stopped.GroupRevision != 1 || stopped.RouteVersion != 2 {
		t.Fatalf("native unpublish failed: status=%+v replayed=%v err=%v", stopped, replayed, err)
	}
	var queuedState string
	if err := db.Raw(`SELECT state FROM notification_source_outbox WHERE id = ?`, outbox.ID).Row().Scan(&queuedState); err != nil || queuedState != "pending" {
		t.Fatalf("stop changed the already queued source event: state=%q err=%v", queuedState, err)
	}
	stopReplay, replayed, err := ApplyNativePublish(ctx, NativePublishRequest{Operation: "unpublish", TenantID: request.TenantID,
		NativeGroupID: request.NativeGroupID, ExpectedRouteVersion: first.RouteVersion, IdempotencyKey: "stop-native-key-0001"})
	if err != nil || !replayed || stopReplay.RouteVersion != stopped.RouteVersion {
		t.Fatalf("lost stop response replay failed: status=%+v replayed=%v err=%v", stopReplay, replayed, err)
	}

	otherTenant, err := GetNativePublishStatus(ctx, "tenant-native-b", request.NativeGroupID)
	if err != nil || otherTenant.Published || otherTenant.RouteVersion != 0 || otherTenant.Status != "unpublished" {
		t.Fatalf("other tenant observed publish state: status=%+v err=%v", otherTenant, err)
	}

	request.GroupRevision = 2
	request.ExpectedRouteVersion = stopped.RouteVersion
	request.IdempotencyKey = "publish-native-key-0002"
	resumed, replayed, err := ApplyNativePublish(ctx, request)
	if err != nil || replayed || !resumed.Published || resumed.RouteVersion != 3 || resumed.GroupRevision != 2 || resumed.EffectiveGroupRevision != 2 {
		t.Fatalf("republish of new revision failed: status=%+v replayed=%v err=%v", resumed, replayed, err)
	}
	if projectionCalls.Load() != 2 {
		t.Fatalf("projection call count after revision 2=%d, want 2", projectionCalls.Load())
	}
	var revisionCount int64
	if err := db.Raw(`SELECT count(*) FROM notification_source_group_route_revisions WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ?`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&revisionCount).Error; err != nil || revisionCount != 2 {
		t.Fatalf("projection history count=%d err=%v", revisionCount, err)
	}
}

func TestNativePublishProjectionFailureLeavesAliasClosedAndRouteLegacy(t *testing.T) {
	db := openNativePublishFixture(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, sourceGroupSnapshotPath) {
			writeNativePublishSnapshotFixture(w, r, true)
			return
		}
		http.Error(w, "fixture failure", http.StatusInternalServerError)
	}))
	defer server.Close()
	bridge := testSourceBridge(t, server)
	defer bridge.Close()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(nil) })

	request := NativePublishRequest{TenantID: "tenant-native-fail", NativeGroupID: "native-group-fail", GroupRevision: 3,
		Name: "Closed alias", ExpectedRouteVersion: 0, IdempotencyKey: "publish-native-failure-001"}
	if _, _, err := ApplyNativePublish(context.Background(), request); !errors.Is(err, ErrNativePublishUnavailable) {
		t.Fatalf("Core projection failure should fail closed: %v", err)
	}
	key := nativePublishRouteKey(bridge.DeploymentID(), request.TenantID, request.NativeGroupID)
	var status, kind string
	if err := db.Raw(`SELECT status, notification_type FROM notification_groups WHERE id = ? AND tenant_id = ?`, key.LegacyGroup, key.TenantID).Row().Scan(&status, &kind); err != nil || status != "CLOSE" || kind != "ENCORE" {
		t.Fatalf("failed projection alias was not closed: status=%q kind=%q err=%v", status, kind, err)
	}
	state, err := GetNativePublishStatus(context.Background(), request.TenantID, request.NativeGroupID)
	if err != nil || state.Published || state.RouteVersion != 0 || state.Status != "unpublished" {
		t.Fatalf("failed projection changed source route: %+v err=%v", state, err)
	}
}

func TestNativePublishDisabledCoreTargetFailsBeforeAliasOrProjection(t *testing.T) {
	db := openNativePublishFixture(t)
	var projectionCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, sourceGroupSnapshotPath) {
			writeNativePublishSnapshotFixture(w, r, false)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == sourceProjectPath {
			projectionCalls.Add(1)
		}
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()
	bridge := testSourceBridge(t, server)
	defer bridge.Close()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(nil) })

	request := NativePublishRequest{TenantID: "tenant-disabled-target", NativeGroupID: "native-disabled-target", GroupRevision: 1,
		Name: "Disabled group", ExpectedRouteVersion: 0, IdempotencyKey: "publish-disabled-target-001"}
	if _, _, err := ApplyNativePublish(context.Background(), request); !errors.Is(err, ErrNativePublishUnavailable) {
		t.Fatalf("disabled Core group was publishable: %v", err)
	}
	if projectionCalls.Load() != 0 {
		t.Fatalf("projection was written for disabled Core group: calls=%d", projectionCalls.Load())
	}
	key := nativePublishRouteKey(bridge.DeploymentID(), request.TenantID, request.NativeGroupID)
	var count int64
	if err := db.Raw(`SELECT count(*) FROM notification_groups WHERE id = ?`, key.LegacyGroup).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("disabled group created a legacy alias: count=%d err=%v", count, err)
	}
}

func TestNativePublishDisabledInstanceOrGrantFailsClosed(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		instanceEnabled bool
		pluginEnabled   bool
	}{
		{name: "disabled instance", instanceEnabled: false, pluginEnabled: true},
		{name: "revoked tenant plugin grant", instanceEnabled: true, pluginEnabled: false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := openNativePublishFixture(t)
			var projectionCalls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, sourceGroupSnapshotPath) {
					writeNativePublishSnapshotFixtureWithTargets(w, r, true, scenario.instanceEnabled, scenario.pluginEnabled)
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == sourceProjectPath {
					projectionCalls.Add(1)
				}
				http.Error(w, "unexpected request", http.StatusBadRequest)
			}))
			defer server.Close()
			bridge := testSourceBridge(t, server)
			defer bridge.Close()
			SetActiveSourceBridge(bridge)
			t.Cleanup(func() { SetActiveSourceBridge(nil) })

			request := NativePublishRequest{TenantID: "tenant-inactive-target", NativeGroupID: "native-inactive-target", GroupRevision: 1,
				Name: "Inactive target group", ExpectedRouteVersion: 0, IdempotencyKey: "publish-inactive-target-001"}
			if _, _, err := ApplyNativePublish(context.Background(), request); !errors.Is(err, ErrNativePublishUnavailable) {
				t.Fatalf("inactive target was publishable: %v", err)
			}
			if projectionCalls.Load() != 0 {
				t.Fatalf("projection was written for inactive target: calls=%d", projectionCalls.Load())
			}
			key := nativePublishRouteKey(bridge.DeploymentID(), request.TenantID, request.NativeGroupID)
			var count int64
			if err := db.Raw(`SELECT count(*) FROM notification_groups WHERE id = ?`, key.LegacyGroup).Scan(&count).Error; err != nil || count != 0 {
				t.Fatalf("inactive target created a legacy alias: count=%d err=%v", count, err)
			}
		})
	}
}

func TestNativePublishConcurrentRevisionCASHasSingleWinner(t *testing.T) {
	openNativePublishFixture(t)
	var blockSnapshots atomic.Bool
	var snapshotCount atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, sourceGroupSnapshotPath) {
			if blockSnapshots.Load() {
				if snapshotCount.Add(1) == 2 {
					releaseOnce.Do(func() { close(release) })
				}
				select {
				case <-release:
				case <-r.Context().Done():
					http.Error(w, "cancelled", http.StatusRequestTimeout)
					return
				}
			}
			writeNativePublishSnapshotFixture(w, r, true)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == sourceProjectPath {
			var request SourceGroupProjectionRequest
			if json.NewDecoder(r.Body).Decode(&request) != nil {
				http.Error(w, "invalid", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sourceEnvelope[SourceGroupProjection]{Code: 200, Message: "Success", RequestID: "projection-race",
				Data: SourceGroupProjection{SourceDeploymentID: request.SourceDeploymentID, TenantID: request.TenantID, LegacyGroupID: request.LegacyGroupID,
					NotificationGroupID: request.NotificationGroupID, GroupRevision: request.GroupRevision, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}})
			return
		}
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()
	bridge := testSourceBridge(t, server)
	defer bridge.Close()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(nil) })

	ctx := context.Background()
	base := NativePublishRequest{TenantID: "tenant-publish-race", NativeGroupID: "native-group-race", GroupRevision: 1,
		Name: "Race group", ExpectedRouteVersion: 0, IdempotencyKey: "publish-race-initial-001"}
	first, replayed, err := ApplyNativePublish(ctx, base)
	if err != nil || replayed || first.RouteVersion != 1 {
		t.Fatalf("seed route failed: %+v replayed=%v err=%v", first, replayed, err)
	}
	blockSnapshots.Store(true)

	type outcome struct {
		status NativePublishStatus
		err    error
	}
	outcomes := make(chan outcome, 2)
	for index, revision := range []int64{2, 3} {
		request := NativePublishRequest{TenantID: base.TenantID, NativeGroupID: base.NativeGroupID, GroupRevision: revision,
			Name: base.Name, ExpectedRouteVersion: first.RouteVersion, IdempotencyKey: fmt.Sprintf("publish-race-revision-%d", index+2)}
		go func() {
			status, _, err := ApplyNativePublish(ctx, request)
			outcomes <- outcome{status: status, err: err}
		}()
	}
	winners, conflicts := 0, 0
	for range 2 {
		result := <-outcomes
		if result.err == nil && result.status.Published && result.status.RouteVersion == 2 {
			winners++
		} else if errors.Is(result.err, ErrNativePublishConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent publish result: status=%+v err=%v", result.status, result.err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("route CAS outcomes: winners=%d conflicts=%d", winners, conflicts)
	}
	state, err := GetNativePublishStatus(ctx, base.TenantID, base.NativeGroupID)
	if err != nil || state.RouteVersion != 2 || (state.GroupRevision != 2 && state.GroupRevision != 3) {
		t.Fatalf("route did not retain exactly one winner: %+v err=%v", state, err)
	}
}

func TestDeterministicNativeAliasSeparatesDeploymentAndTenant(t *testing.T) {
	base := deterministicNativeAliasID("deployment-a", "tenant-a", "group-a")
	if base == deterministicNativeAliasID("deployment-b", "tenant-a", "group-a") || base == deterministicNativeAliasID("deployment-a", "tenant-b", "group-a") || base == deterministicNativeAliasID("deployment-a", "tenant-a", "group-b") {
		t.Fatal("native alias identity did not include deployment, tenant, and group")
	}
}

func openNativePublishFixture(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_DSN fixture not supplied")
	}
	if dsn != approvedNativePublishDSN {
		t.Fatal("refusing non-approved notification fixture DSN")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open approved native publish fixture")
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal("read approved native publish fixture pool")
	}
	var database, user string
	if err := admin.Raw(`SELECT current_database(), current_user`).Row().Scan(&database, &user); err != nil || database != "notification_test" || user != "notification_test" {
		t.Fatal("notification fixture database identity mismatch")
	}
	schema := "native_publish_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("create isolated native publish schema")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse approved notification fixture DSN")
	}
	query := parsed.Query()
	query.Set("options", "-c search_path="+schema)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated native publish schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("read isolated native publish pool")
	}
	previous := global.DB
	global.DB = db
	t.Cleanup(func() {
		global.DB = previous
		_ = pool.Close()
		_ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		_ = adminPool.Close()
	})
	if err := db.Exec(`CREATE TABLE notification_groups (
		id text PRIMARY KEY, name text NOT NULL, notification_type text NOT NULL, status text NOT NULL,
		notification_config text, description text, tenant_id text NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, remark text)`).Error; err != nil {
		t.Fatal("create isolated notification group table")
	}
	if err := db.Exec(`CREATE TABLE alarm_info (id text PRIMARY KEY)`).Error; err != nil {
		t.Fatal("create isolated alarm table")
	}
	if err := db.Exec(`CREATE TABLE alarm_history (id text PRIMARY KEY)`).Error; err != nil {
		t.Fatal("create isolated alarm history table")
	}
	for _, statement := range strings.Split(stripSQLComments(readSourceMigration(t, 23)), ";") {
		statement = strings.TrimSpace(statement)
		if statement != "" {
			statement = strings.ReplaceAll(statement, "public.", `"`+schema+`".`)
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal("apply isolated source route migration")
			}
		}
	}
	migration24 := strings.ReplaceAll(readSourceMigration(t, 24), "public.", `"`+schema+`".`)
	if err := db.Exec(migration24).Error; err != nil {
		t.Fatal("apply isolated source route state migration")
	}
	return db
}

func readSourceMigration(t *testing.T, version int) string {
	t.Helper()
	path := filepath.Join("..", "..", "sql", fmt.Sprintf("%d.sql", version))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read source migration")
	}
	return string(data)
}

func writeNativePublishSnapshotFixture(w http.ResponseWriter, r *http.Request, enabled bool) {
	writeNativePublishSnapshotFixtureWithTargets(w, r, enabled, true, true)
}

func writeNativePublishSnapshotFixtureWithTargets(w http.ResponseWriter, r *http.Request, groupEnabled, instanceEnabled, pluginEnabled bool) {
	groupID := strings.TrimPrefix(r.URL.Path, sourceGroupSnapshotPath)
	var revision int64
	_, _ = fmt.Sscan(r.URL.Query().Get("groupRevision"), &revision)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sourceGroupSnapshotEnvelope{Code: 200, Message: "Success", RequestID: "snapshot-fixture",
		Data: sourceGroupSnapshot{SourceDeploymentID: "deployment-test", TenantID: r.URL.Query().Get("tenantId"),
			Group: sourceSnapshotGroup{ID: groupID, Enabled: groupEnabled, Revision: revision,
				Bindings: []sourceSnapshotBinding{{BindingID: "binding-1", InstanceID: "instance-1", RecipientSource: json.RawMessage(`{"kind":"literal"}`), ContentBinding: json.RawMessage(`{"kind":"text"}`)}}},
			Instances: []sourceSnapshotInstance{{ID: "instance-1", PluginRegistrationID: "registration-1", Channel: "email", Enabled: instanceEnabled, ConfigVersion: 1}},
			Plugins:   []sourceSnapshotPlugin{{ID: "registration-1", PluginID: "fixture.smtp", PluginVersion: "1", Enabled: pluginEnabled}},
		},
	})
}

func stripSQLComments(value string) string {
	lines := strings.Split(value, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
