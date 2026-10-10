package dal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"project/internal/model"
	"project/pkg/global"

	"github.com/google/uuid"
)

func activateDefaultPolicyFixture(t *testing.T, tenantID, aliasID, nativeID string, revision int64) SourceRouteKey {
	t.Helper()
	key := SourceRouteKey{DeploymentID: "deployment-default-test", TenantID: tenantID, LegacyGroup: aliasID}
	if err := global.DB.Exec(`INSERT INTO notification_groups (id, name, notification_type, status, notification_config, tenant_id, created_at, updated_at)
		VALUES (?, 'policy', 'ENCORE', 'CLOSE', '{}', ?, now(), now())`, aliasID, tenantID).Error; err != nil {
		t.Fatalf("create native alias fixture: %v", err)
	}
	if _, _, err := ActivateNativePublishRoute(context.Background(), key, nativeID, "policy", revision, 0, "projection-key-default-001"); err != nil {
		t.Fatal("activate native route")
	}
	return key
}

func defaultPolicyOutbox(t *testing.T, route SourceRouteSnapshot, aliasID, tenantID, eventID string) *SourceOutboxRecord {
	t.Helper()
	now := time.Now().UTC()
	body, _ := json.Marshal(map[string]any{"notificationGroupId": route.NotificationGroupID, "groupRevision": route.GroupRevision})
	return &SourceOutboxRecord{ID: uuid.NewString(), SourceDeploymentID: "deployment-default-test", TenantID: tenantID,
		SourceEventID: eventID, SourceActionID: "action-" + eventID, LegacyGroupID: aliasID, NotificationGroupID: route.NotificationGroupID,
		GroupRevision: route.GroupRevision, IdempotencyKey: "default-key-" + eventID, RequestBody: body,
		BodySHA256: strings.Repeat("a", 64), OccurredAt: now, ExpiresAt: now.Add(time.Hour), State: "pending",
		NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
}

func TestDefaultPolicyCASSnapshotClearAndStoppedDiagnostic(t *testing.T) {
	db := openNotificationSourceFixture(t)
	createNotificationGroupFixture(t)
	var resourceRules, tenantAdminRules int64
	if err := db.Raw(`SELECT count(*) FROM casbin_rule WHERE ptype = 'g2' AND v0 = 'api/v1/notification-default-policy' AND v1 = 'api/v1/notification-default-policy'`).Scan(&resourceRules).Error; err != nil || resourceRules != 1 {
		t.Fatalf("default policy Casbin resource was not registered: count=%d err=%v", resourceRules, err)
	}
	if err := db.Raw(`SELECT count(*) FROM casbin_rule WHERE ptype = 'p' AND v0 = 'TENANT_ADMIN' AND v1 = 'api/v1/notification-default-policy' AND v2 = 'allow'`).Scan(&tenantAdminRules).Error; err != nil || tenantAdminRules != 1 {
		t.Fatalf("tenant administrator Casbin rule was not registered: count=%d err=%v", tenantAdminRules, err)
	}
	now := time.Now().UTC()
	aliasID, nativeID := "default-alias-a", "native-policy-a"
	key := activateDefaultPolicyFixture(t, "tenant-default-a", aliasID, nativeID, 1)
	first, err := SetTenantDefaultPolicy(context.Background(), key.DeploymentID, key.TenantID, nativeID, aliasID, 0)
	if err != nil || first.Version != 1 {
		t.Fatalf("set default failed: version=%d err=%v", first.Version, err)
	}
	if _, err := SetTenantDefaultPolicy(context.Background(), key.DeploymentID, key.TenantID, "", "", 0); !errors.Is(err, ErrDefaultPolicyConflict) {
		t.Fatalf("stale default write should conflict, got %v", err)
	}
	if _, err := SetTenantDefaultPolicy(context.Background(), key.DeploymentID, "tenant-default-b", nativeID, aliasID, 0); !errors.Is(err, ErrDefaultPolicyInvalid) {
		t.Fatalf("cross-tenant target should be masked unavailable, got %v", err)
	}
	otherTenant, _, err := GetTenantDefaultPolicy(context.Background(), key.DeploymentID, "tenant-default-b")
	if err != nil || otherTenant.NativeGroupID != nil || otherTenant.Version != 0 {
		t.Fatalf("cross-tenant read leaked default: %+v err=%v", otherTenant, err)
	}

	alarm := &model.AlarmInfo{ID: "default-event-1", AlarmConfigID: "alarm-config", Name: "alarm", AlarmTime: now,
		ProcessingResult: "UND", TenantID: key.TenantID}
	route, alias, err := SaveAlarmInfoWithDefaultSource(context.Background(), alarm, key.DeploymentID, key.TenantID, func(route SourceRouteSnapshot, aliasID string) (*SourceOutboxRecord, error) {
		return defaultPolicyOutbox(t, route, aliasID, key.TenantID, alarm.ID), nil
	})
	if err != nil || route.Engine != "encore" || route.GroupRevision != 1 || alias != aliasID {
		t.Fatalf("default did not resolve: route=%+v alias=%s err=%v", route, alias, err)
	}

	// A newer native publish is followed by future events, while prior outbox
	// snapshots remain immutable.
	if _, _, err := ActivateNativePublishRoute(context.Background(), key, nativeID, "policy", 2, 1, "projection-key-default-002"); err != nil {
		t.Fatal("republish policy")
	}
	history := &model.AlarmHistory{ID: "default-event-2", AlarmConfigID: "alarm-config", GroupID: "group", SceneAutomationID: "scene",
		Name: "alarm", AlarmStatus: "H", TenantID: key.TenantID, CreateAt: now, AlarmDeviceList: "[]"}
	route, _, err = SaveAlarmHistoryWithDefaultSource(context.Background(), history, key.DeploymentID, key.TenantID, func(route SourceRouteSnapshot, aliasID string) (*SourceOutboxRecord, error) {
		return defaultPolicyOutbox(t, route, aliasID, key.TenantID, history.ID), nil
	})
	if err != nil || route.GroupRevision != 2 {
		t.Fatalf("natural history path did not resolve latest published revision: %+v %v", route, err)
	}
	if _, err := SetTenantDefaultPolicy(context.Background(), key.DeploymentID, key.TenantID, "", "", 1); err != nil {
		t.Fatalf("clear default failed: %v", err)
	}
	var preserved struct {
		GroupRevision int64  `gorm:"column:group_revision"`
		State         string `gorm:"column:state"`
	}
	if err := db.Raw(`SELECT group_revision, state FROM notification_source_outbox WHERE source_event_id = 'default-event-1'`).Scan(&preserved).Error; err != nil || preserved.GroupRevision != 1 || preserved.State != "pending" {
		t.Fatalf("changing default mutated frozen event: %+v err=%v", preserved, err)
	}

	if _, err := SetTenantDefaultPolicy(context.Background(), key.DeploymentID, key.TenantID, nativeID, aliasID, 2); err != nil {
		t.Fatalf("restore default failed: %v", err)
	}
	if _, _, err := StopNativePublishRoute(context.Background(), key, nativeID, 2); err != nil {
		t.Fatalf("stop published target: %v", err)
	}
	blocked := &model.AlarmInfo{ID: "default-event-blocked", AlarmConfigID: "alarm-config", Name: "alarm", AlarmTime: now,
		ProcessingResult: "UND", TenantID: key.TenantID}
	blockedRoute, _, err := SaveAlarmInfoWithDefaultSource(context.Background(), blocked, key.DeploymentID, key.TenantID, func(route SourceRouteSnapshot, aliasID string) (*SourceOutboxRecord, error) {
		return defaultPolicyOutbox(t, route, aliasID, key.TenantID, blocked.ID), nil
	})
	if err != nil || blockedRoute.Engine != "blocked" {
		t.Fatalf("stopped default did not create blocked diagnostic: %+v err=%v", blockedRoute, err)
	}
	var state, failure string
	if err := db.Raw(`SELECT state, failure_code FROM notification_source_outbox WHERE source_event_id = ?`, blocked.ID).Row().Scan(&state, &failure); err != nil || state != "blocked" || failure != "default_policy_unavailable" {
		t.Fatalf("blocked diagnostic missing: state=%s failure=%s err=%v", state, failure, err)
	}
	claimed, err := ClaimSourceOutbox(context.Background(), 10, time.Minute, 3, now.Add(time.Second))
	if err != nil {
		t.Fatal("claim source events")
	}
	for _, record := range claimed {
		if record.SourceEventID == blocked.ID {
			t.Fatal("blocked default was claimable")
		}
	}

	cleared, err := SetTenantDefaultPolicy(context.Background(), key.DeploymentID, key.TenantID, "", "", 3)
	if err != nil || cleared.Version != 4 || cleared.NativeGroupID != nil {
		t.Fatalf("clear did not advance version: %+v err=%v", cleared, err)
	}
	noDefault := &model.AlarmInfo{ID: "default-event-none", AlarmConfigID: "alarm-config", Name: "alarm", AlarmTime: now,
		ProcessingResult: "UND", TenantID: key.TenantID}
	noPolicyRoute, _, err := SaveAlarmInfoWithDefaultSource(context.Background(), noDefault, key.DeploymentID, key.TenantID, func(route SourceRouteSnapshot, aliasID string) (*SourceOutboxRecord, error) {
		return defaultPolicyOutbox(t, route, aliasID, key.TenantID, noDefault.ID), nil
	})
	if err != nil || noPolicyRoute.Engine != "none" {
		t.Fatalf("no-default should persist without routing: %+v err=%v", noPolicyRoute, err)
	}
	var outboxCount int64
	if err := db.Raw(`SELECT count(*) FROM notification_source_outbox`).Scan(&outboxCount).Error; err != nil || outboxCount != 3 {
		t.Fatalf("unexpected outbox count after clear: %d err=%v", outboxCount, err)
	}
}

func TestDefaultPolicyChangeWaitsForAlarmSnapshotTransaction(t *testing.T) {
	openNotificationSourceFixture(t)
	createNotificationGroupFixture(t)
	aliasID, nativeID := "default-alias-race", "native-policy-race"
	key := activateDefaultPolicyFixture(t, "tenant-default-race", aliasID, nativeID, 1)
	if _, err := SetTenantDefaultPolicy(context.Background(), key.DeploymentID, key.TenantID, nativeID, aliasID, 0); err != nil {
		t.Fatal("seed default")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	eventDone := make(chan error, 1)
	go func() {
		alarm := &model.AlarmInfo{ID: "default-race-event", AlarmConfigID: "alarm-config", Name: "alarm", AlarmTime: time.Now().UTC(), ProcessingResult: "UND", TenantID: key.TenantID}
		_, _, err := SaveAlarmInfoWithDefaultSource(context.Background(), alarm, key.DeploymentID, key.TenantID, func(route SourceRouteSnapshot, alias string) (*SourceOutboxRecord, error) {
			close(entered)
			<-release
			return defaultPolicyOutbox(t, route, alias, key.TenantID, alarm.ID), nil
		})
		eventDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("event did not reach its locked snapshot")
	}
	changeDone := make(chan error, 1)
	go func() {
		_, err := SetTenantDefaultPolicy(context.Background(), key.DeploymentID, key.TenantID, "", "", 1)
		changeDone <- err
	}()
	select {
	case err := <-changeDone:
		t.Fatalf("default changed before event commit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-eventDone; err != nil {
		t.Fatalf("event transaction failed: %v", err)
	}
	select {
	case err := <-changeDone:
		if err != nil {
			t.Fatalf("default clear failed after snapshot commit: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("default change stayed blocked after event commit")
	}
	var outboxNativeID string
	if err := global.DB.Raw(`SELECT notification_group_id FROM notification_source_outbox WHERE source_event_id = 'default-race-event'`).Row().Scan(&outboxNativeID); err != nil || outboxNativeID != "native-policy-race" {
		t.Fatalf("event did not retain pre-change default: %s err=%v", outboxNativeID, err)
	}
}
