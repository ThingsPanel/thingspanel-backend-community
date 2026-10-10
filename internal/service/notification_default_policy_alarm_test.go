package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorm.io/gorm"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/global"
)

func TestAlarmInfoAndAlarmExecuteResolveDefaultButPreserveExplicitGroup(t *testing.T) {
	openSourceCompatPGFixture(t)
	db := global.DB
	for _, ddl := range []string{
		`CREATE TABLE alarm_config (id text PRIMARY KEY, name text NOT NULL, description text, alarm_level text NOT NULL, notification_group_id text NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, tenant_id text NOT NULL, remark text, enabled text NOT NULL)`,
		`CREATE TABLE alarm_info (id text PRIMARY KEY, alarm_config_id text NOT NULL, name text NOT NULL, alarm_time timestamptz NOT NULL, description text, content text, processor text, processing_result text NOT NULL, tenant_id text NOT NULL, remark text, alarm_level text)`,
		`CREATE TABLE alarm_history (id text PRIMARY KEY, alarm_config_id text NOT NULL, group_id text NOT NULL, scene_automation_id text NOT NULL, name text NOT NULL, description text, content text, alarm_status text NOT NULL, tenant_id text NOT NULL, remark text, create_at timestamptz NOT NULL, alarm_device_list text NOT NULL)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal("create alarm fixture tables")
		}
	}

	deployment, tenant := "deployment-test", "tenant-a"
	defaultKey := nativePublishRouteKey(deployment, tenant, "native-default-policy")
	explicitKey := nativePublishRouteKey(deployment, tenant, "native-explicit-policy")
	for _, target := range []struct {
		key      dal.SourceRouteKey
		nativeID string
	}{
		{defaultKey, "native-default-policy"}, {explicitKey, "native-explicit-policy"},
	} {
		if err := dal.EnsureNativePublishAlias(context.Background(), target.key, target.key.LegacyGroup, target.nativeID); err != nil {
			t.Fatalf("ensure alias fixture: %v", err)
		}
		if _, _, err := dal.ActivateNativePublishRoute(context.Background(), target.key, target.nativeID, target.nativeID, 1, 0, "projection-default-0001"); err != nil {
			t.Fatalf("activate route fixture: %v", err)
		}
	}
	if _, err := dal.SetTenantDefaultPolicy(context.Background(), deployment, tenant, "native-default-policy", defaultKey.LegacyGroup, 0); err != nil {
		t.Fatalf("set tenant default: %v", err)
	}

	now := time.Now().UTC()
	for _, row := range []struct{ id, group string }{{"default-add", ""}, {"default-execute", ""}, {"explicit-wins", explicitKey.LegacyGroup}, {"default-stopped", ""}} {
		if err := db.Exec(`INSERT INTO alarm_config (id, name, alarm_level, notification_group_id, created_at, updated_at, tenant_id, enabled) VALUES (?, ?, 'H', ?, ?, ?, ?, 'Y')`, row.id, row.id, row.group, now, now, tenant).Error; err != nil {
			t.Fatal("seed alarm config")
		}
	}
	core := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer core.Close()
	bridge := testSourceBridge(t, core)
	defer bridge.Close()
	previous := currentSourceBridge()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(previous) })

	addOK, addID := GroupApp.Alarm.AddAlarmInfo("default-add", "alarm add payload")
	if !addOK {
		t.Fatal("AddAlarmInfo did not persist default-routed alarm")
	}
	executeOK, _, _ := GroupApp.Alarm.AlarmExecute("default-execute", "alarm execute payload", "scene", "group", nil)
	if !executeOK {
		t.Fatal("AlarmExecute did not persist default-routed alarm history")
	}
	var executeID string
	if err := db.Raw(`SELECT id FROM alarm_history WHERE alarm_config_id = 'default-execute'`).Row().Scan(&executeID); err != nil {
		t.Fatal("read AlarmExecute history ID")
	}
	explicitOK, explicitID := GroupApp.Alarm.AddAlarmInfo("explicit-wins", "explicit payload")
	if !explicitOK {
		t.Fatal("explicit alarm was not persisted")
	}
	assertAlarmOutboxTarget(t, db, addID, "native-default-policy", 1, "pending")
	assertAlarmOutboxTarget(t, db, executeID, "native-default-policy", 1, "pending")
	assertAlarmOutboxTarget(t, db, explicitID, "native-explicit-policy", 1, "pending")

	if _, _, err := dal.StopNativePublishRoute(context.Background(), defaultKey, "native-default-policy", 1); err != nil {
		t.Fatalf("stop default route: %v", err)
	}
	blockedOK, blockedID := GroupApp.Alarm.AddAlarmInfo("default-stopped", "blocked payload")
	if !blockedOK {
		t.Fatal("stopped default should preserve the alarm")
	}
	assertAlarmOutboxTarget(t, db, blockedID, "native-default-policy", 1, "blocked")
	var remark string
	if err := db.Raw(`SELECT coalesce(remark,'') FROM alarm_info WHERE id = ?`, blockedID).Row().Scan(&remark); err != nil || remark != "default_policy_unavailable" {
		t.Fatalf("stopped default diagnosis not visible on alarm: %q err=%v", remark, err)
	}
	var savedGroup string
	if err := db.Raw(`SELECT notification_group_id FROM alarm_config WHERE id = 'default-add'`).Row().Scan(&savedGroup); err != nil || savedGroup != "" {
		t.Fatalf("default selection was copied into alarm config: %q err=%v", savedGroup, err)
	}
	emptySelection := ""
	if err := dal.UpdateAlarmConfigFields(&model.AlarmConfig{ID: "explicit-wins", TenantID: tenant, NotificationGroupID: emptySelection, UpdatedAt: time.Now().UTC()}, true); err != nil {
		t.Fatalf("explicit group clear failed: %v", err)
	}
	if err := db.Raw(`SELECT notification_group_id FROM alarm_config WHERE id = 'explicit-wins'`).Row().Scan(&savedGroup); err != nil || savedGroup != "" {
		t.Fatalf("explicit group clear was omitted by GORM update: value=%q err=%v", savedGroup, err)
	}
}

func assertAlarmOutboxTarget(t *testing.T, db *gorm.DB, eventID, wantGroup string, wantRevision int64, wantState string) {
	t.Helper()
	var groupID, state string
	var revision int64
	err := db.Raw(`SELECT notification_group_id, group_revision, state FROM notification_source_outbox WHERE source_event_id = ?`, eventID).Row().Scan(&groupID, &revision, &state)
	if err != nil || groupID != wantGroup || revision != wantRevision || state != wantState {
		t.Fatalf("unexpected event route id=%s group=%s revision=%d state=%s err=%v", eventID, groupID, revision, state, err)
	}
}
