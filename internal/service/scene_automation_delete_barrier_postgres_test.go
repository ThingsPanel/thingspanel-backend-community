package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

func setupSceneDeleteBarrierDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := brokerEvidenceTestDB(t)
	models := []interface{}{
		&model.SceneAutomation{}, &model.DeviceTriggerCondition{}, &model.OneTimeTask{}, &model.PeriodicTask{}, &model.ActionInfo{},
		&model.DeviceTriggerCondition{}, &model.TelemetryCurrentData{}, &model.TelemetryData{}, &model.TelemetrySetLog{},
		&model.AttributeData{}, &model.AttributeSetLog{}, &model.EventData{}, &model.CommandSetLog{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate isolated scene/delete fixture: %v", err)
	}
	return db
}

func useSceneDeleteBarrierDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	redisAddr := os.Getenv("TEST_DEVICE_DELETE_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("isolated Redis address not configured")
	}
	oldDB, oldRedis, oldQ := global.DB, global.REDIS, *query.Q
	global.DB = db
	query.SetDefault(db)
	global.REDIS = redis.NewClient(&redis.Options{Addr: redisAddr, MaxRetries: 0})
	t.Cleanup(func() {
		_ = global.REDIS.Close()
		global.DB, global.REDIS, *query.Q = oldDB, oldRedis, oldQ
	})
}

func sceneDeleteFixtureDevice(t *testing.T, db *gorm.DB, tenant, id string) {
	t.Helper()
	row := model.Device{ID: id, TenantID: tenant, DeviceNumber: "number-" + id, Voucher: "voucher-" + id, IsEnabled: "enabled", ActivateFlag: "active"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create device %q: %v", id, err)
	}
}

func sceneType10Req(name, deviceID string) *model.CreateSceneAutomationReq {
	return &model.CreateSceneAutomationReq{
		Name: name, Enabled: "N", Actions: []model.Action{},
		TriggerConditionGroups: [][]model.Condition{{{
			TriggerConditionsType: model.DEVICE_TRIGGER_CONDITION_TYPE_ONE,
			TriggerSource:         &deviceID,
		}}},
	}
}

func sceneType10UpdateReq(sceneID, name, deviceID string) *model.UpdateSceneAutomationReq {
	return &model.UpdateSceneAutomationReq{
		ID: sceneID, Name: name, Enabled: "N", Actions: []model.Action{},
		TriggerConditionGroups: [][]model.Condition{{{
			TriggerConditionsType: model.DEVICE_TRIGGER_CONDITION_TYPE_ONE,
			TriggerSource:         &deviceID,
		}}},
	}
}

func assertSceneErrorCode(t *testing.T, err error, want int) {
	t.Helper()
	var got *errcode.Error
	if !errors.As(err, &got) || got.Code != want {
		t.Fatalf("error=%v, want code %d", err, want)
	}
}

func TestSceneAutomationDeviceDeleteBarrierPostgres(t *testing.T) {
	db := setupSceneDeleteBarrierDB(t)
	useSceneDeleteBarrierDB(t, db)
	claims := &utils.UserClaims{ID: "scene-owner", TenantID: "tenant-a"}
	qtx := query.Use(db).Begin()
	if err := dal.LockTenantDeviceTriggerSources(claims.TenantID, []string{" \t"}, qtx); !errors.Is(err, dal.ErrInvalidDeviceTriggerSource) {
		_ = qtx.Rollback()
		t.Fatalf("DAL lock helper accepted empty source: %v", err)
	}
	_ = qtx.Rollback()

	// Missing and cross-tenant type 10 sources fail before scene/condition writes.
	foreignID := "foreign-" + uuid.NewString()
	sceneDeleteFixtureDevice(t, db, "tenant-b", foreignID)
	for _, source := range []string{"missing-" + uuid.NewString(), foreignID} {
		if _, err := (&SceneAutomation{}).CreateSceneAutomation(sceneType10Req("invalid-"+uuid.NewString(), source), claims); err == nil {
			t.Fatalf("create accepted unavailable or cross-tenant source %q", source)
		} else {
			assertSceneErrorCode(t, err, errcode.CodeNotFound)
		}
	}
	var scenes int64
	if err := db.Model(&model.SceneAutomation{}).Where("tenant_id=?", claims.TenantID).Count(&scenes).Error; err != nil || scenes != 0 {
		t.Fatalf("invalid source wrote scene rows=%d err=%v", scenes, err)
	}
	if _, err := (&SceneAutomation{}).CreateSceneAutomation(sceneType10Req("missing-claims", "x"), nil); err == nil {
		t.Fatal("create accepted missing claims")
	} else {
		assertSceneErrorCode(t, err, errcode.CodeNoPermission)
	}
	for _, source := range []*string{nil, ptrSceneSource("  ")} {
		invalid := sceneType10Req("empty-source-"+uuid.NewString(), "unused")
		invalid.TriggerConditionGroups[0][0].TriggerSource = source
		if _, err := (&SceneAutomation{}).CreateSceneAutomation(invalid, claims); err == nil {
			t.Fatal("create accepted an empty type 10 source")
		} else {
			assertSceneErrorCode(t, err, errcode.CodeParamError)
		}
	}

	// Type 11 carries a device-config ID and must not be treated as a device row.
	configID := "config-only-" + uuid.NewString()
	type11 := &model.CreateSceneAutomationReq{
		Name: "type11-" + uuid.NewString(), Enabled: "N", Actions: []model.Action{},
		TriggerConditionGroups: [][]model.Condition{{{
			TriggerConditionsType: model.DEVICE_TRIGGER_CONDITION_TYPE_MULTIPLE,
			TriggerSource:         &configID,
		}}},
	}
	if _, err := (&SceneAutomation{}).CreateSceneAutomation(type11, claims); err != nil {
		t.Fatalf("type 11 config source was incorrectly locked as a device: %v", err)
	}
	updateDeviceID := "update-source-" + uuid.NewString()
	sceneDeleteFixtureDevice(t, db, claims.TenantID, updateDeviceID)
	updateSceneID, err := (&SceneAutomation{}).CreateSceneAutomation(sceneType10Req("update-empty-"+uuid.NewString(), updateDeviceID), claims)
	if err != nil {
		t.Fatalf("seed update source scene: %v", err)
	}
	invalidUpdate := sceneType10UpdateReq(updateSceneID, "empty-update", updateDeviceID)
	invalidUpdate.TriggerConditionGroups[0][0].TriggerSource = nil
	if _, err := (&SceneAutomation{}).UpdateSceneAutomation(invalidUpdate, claims); err == nil {
		t.Fatal("update accepted an empty type 10 source")
	} else {
		assertSceneErrorCode(t, err, errcode.CodeParamError)
	}
	var preserved model.DeviceTriggerCondition
	if err := db.Where("scene_automation_id=? AND trigger_condition_type=?", updateSceneID, model.DEVICE_TRIGGER_CONDITION_TYPE_ONE).First(&preserved).Error; err != nil || preserved.TriggerSource == nil || *preserved.TriggerSource != updateDeviceID {
		t.Fatalf("invalid update changed the existing reference: row=%+v err=%v", preserved, err)
	}

	// Tenant-scoped update does not create or take ownership of another scene.
	foreignSceneID := "foreign-scene-" + uuid.NewString()
	foreignScene := model.SceneAutomation{ID: foreignSceneID, Name: "foreign", Enabled: "N", TenantID: "tenant-b", Creator: "b", Updator: "b", CreatedAt: time.Now().UTC()}
	if err := db.Create(&foreignScene).Error; err != nil {
		t.Fatal(err)
	}
	update := sceneType10UpdateReq(foreignSceneID, "takeover", foreignID)
	if _, err := (&SceneAutomation{}).UpdateSceneAutomation(update, claims); err == nil {
		t.Fatal("cross-tenant update succeeded")
	} else {
		assertSceneErrorCode(t, err, errcode.CodeNotFound)
	}
	missingSceneID := "missing-scene-" + uuid.NewString()
	if _, err := (&SceneAutomation{}).UpdateSceneAutomation(sceneType10UpdateReq(missingSceneID, "upsert", foreignID), claims); err == nil {
		t.Fatal("update of missing scene upserted a row")
	} else {
		assertSceneErrorCode(t, err, errcode.CodeNotFound)
	}
	var foreignAfter model.SceneAutomation
	if err := db.Where("id=?", foreignSceneID).First(&foreignAfter).Error; err != nil || foreignAfter.TenantID != "tenant-b" || foreignAfter.Name != "foreign" {
		t.Fatalf("foreign scene was changed: row=%+v err=%v", foreignAfter, err)
	}
	if err := db.Model(&model.SceneAutomation{}).Where("id=?", missingSceneID).Count(&scenes).Error; err != nil || scenes != 0 {
		t.Fatalf("missing scene was upserted count=%d err=%v", scenes, err)
	}
	invalidOldSceneID := "invalid-old-" + uuid.NewString()
	invalidOldScene := model.SceneAutomation{ID: invalidOldSceneID, Name: "invalid-old", Enabled: "N", TenantID: claims.TenantID, Creator: claims.ID, Updator: claims.ID, CreatedAt: time.Now().UTC()}
	if err := db.Create(&invalidOldScene).Error; err != nil {
		t.Fatal(err)
	}
	invalidOldCondition := model.DeviceTriggerCondition{ID: uuid.NewString(), SceneAutomationID: invalidOldSceneID, Enabled: "N", GroupID: uuid.NewString(), TriggerConditionType: model.DEVICE_TRIGGER_CONDITION_TYPE_ONE, TriggerValue: "", TenantID: claims.TenantID}
	if err := db.Create(&invalidOldCondition).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&SceneAutomation{}).UpdateSceneAutomation(sceneType10UpdateReq(invalidOldSceneID, "repair", foreignID), claims); err == nil {
		t.Fatal("update silently repaired an invalid historical type 10 reference")
	} else {
		assertSceneErrorCode(t, err, errcode.CodeParamError)
	}

	// Create path owns the device lock first. Delete waits, then observes the
	// committed reference and refuses to remove the device.
	createWinsID := "create-wins-" + uuid.NewString()
	sceneDeleteFixtureDevice(t, db, claims.TenantID, createWinsID)
	createGate := installSceneDeleteAdvisoryGate(t, db, "scene_automations", "BEFORE INSERT", "scene_insert_gate", "block_scene_insert", nextSceneDeleteAdvisoryKey())
	createDone := make(chan error, 1)
	go func() {
		_, err := (&SceneAutomation{}).CreateSceneAutomation(sceneType10Req("create-wins-"+uuid.NewString(), createWinsID), claims)
		createDone <- err
	}()
	createBlocker := waitForSceneDeleteGate(t, db, "scene_automations")
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- (&Device{}).DeleteDevice(createWinsID, claims) }()
	waitForSceneDeviceLockWait(t, db, createBlocker)
	createGate.release(t)
	if err := <-createDone; err != nil {
		t.Fatalf("create did not commit after gate release: %v", err)
	}
	assertSceneErrorCode(t, <-deleteDone, 200062)

	// Delete path owns the device lock first. A concurrent create waits, then
	// fails closed after the row has been removed.
	deleteWinsID := "delete-wins-" + uuid.NewString()
	sceneDeleteFixtureDevice(t, db, claims.TenantID, deleteWinsID)
	deleteGate := installSceneDeleteAdvisoryGate(t, db, "devices", "BEFORE DELETE", "device_delete_gate", "block_device_delete", nextSceneDeleteAdvisoryKey())
	deleteDone = make(chan error, 1)
	go func() { deleteDone <- (&Device{}).DeleteDevice(deleteWinsID, claims) }()
	deleteBlocker := waitForSceneDeleteGate(t, db, "devices")
	createDone = make(chan error, 1)
	go func() {
		_, err := (&SceneAutomation{}).CreateSceneAutomation(sceneType10Req("delete-wins-"+uuid.NewString(), deleteWinsID), claims)
		createDone <- err
	}()
	waitForSceneDeviceLockWait(t, db, deleteBlocker)
	deleteGate.release(t)
	if err := <-deleteDone; err != nil {
		t.Fatalf("delete did not commit after gate release: %v", err)
	}
	assertSceneErrorCode(t, <-createDone, errcode.CodeNotFound)

	// Update path locks scene, then the union of old and new device IDs. A
	// concurrent delete of the new source waits and then sees the new reference.
	oldID, updateTargetID := "update-old-"+uuid.NewString(), "update-target-"+uuid.NewString()
	sceneDeleteFixtureDevice(t, db, claims.TenantID, oldID)
	sceneDeleteFixtureDevice(t, db, claims.TenantID, updateTargetID)
	sceneID, err := (&SceneAutomation{}).CreateSceneAutomation(sceneType10Req("update-race-"+uuid.NewString(), oldID), claims)
	if err != nil {
		t.Fatalf("seed scene: %v", err)
	}
	updateGate := installSceneDeleteAdvisoryGate(t, db, "device_trigger_condition", "BEFORE DELETE", "condition_delete_gate", "block_condition_delete", nextSceneDeleteAdvisoryKey())
	updateDone := make(chan error, 1)
	go func() {
		_, err := (&SceneAutomation{}).UpdateSceneAutomation(sceneType10UpdateReq(sceneID, "updated", updateTargetID), claims)
		updateDone <- err
	}()
	updateBlocker := waitForSceneDeleteGate(t, db, "device_trigger_condition")
	deleteDone = make(chan error, 1)
	go func() { deleteDone <- (&Device{}).DeleteDevice(updateTargetID, claims) }()
	waitForSceneDeviceLockWait(t, db, updateBlocker)
	updateGate.release(t)
	if err := <-updateDone; err != nil {
		t.Fatalf("update did not commit after gate release: %v", err)
	}
	assertSceneErrorCode(t, <-deleteDone, 200062)

	// Delete path can also win against update. The update must not install a
	// reference to a device deleted while it waited for the row lock.
	deleteUpdateTarget := "delete-update-target-" + uuid.NewString()
	sceneDeleteFixtureDevice(t, db, claims.TenantID, deleteUpdateTarget)
	deleteGate = installSceneDeleteAdvisoryGate(t, db, "devices", "BEFORE DELETE", "device_delete_gate", "block_device_delete", nextSceneDeleteAdvisoryKey())
	deleteDone = make(chan error, 1)
	go func() { deleteDone <- (&Device{}).DeleteDevice(deleteUpdateTarget, claims) }()
	deleteBlocker = waitForSceneDeleteGate(t, db, "devices")
	updateDone = make(chan error, 1)
	go func() {
		_, err := (&SceneAutomation{}).UpdateSceneAutomation(sceneType10UpdateReq(sceneID, "should-not-commit", deleteUpdateTarget), claims)
		updateDone <- err
	}()
	waitForSceneDeviceLockWait(t, db, deleteBlocker)
	deleteGate.release(t)
	if err := <-deleteDone; err != nil {
		t.Fatalf("delete did not commit before update: %v", err)
	}
	assertSceneErrorCode(t, <-updateDone, errcode.CodeNotFound)
	var current model.DeviceTriggerCondition
	if err := db.Where("scene_automation_id=? AND trigger_condition_type=?", sceneID, model.DEVICE_TRIGGER_CONDITION_TYPE_ONE).First(&current).Error; err != nil || current.TriggerSource == nil || *current.TriggerSource != updateTargetID {
		t.Fatalf("failed update replaced existing reference: row=%+v err=%v", current, err)
	}
}

func ptrSceneSource(value string) *string { return &value }

func nextSceneDeleteAdvisoryKey() int64 { return time.Now().UnixNano() }

type sceneDeleteGate struct {
	conn *sql.Conn
	key  int64
}

func installSceneDeleteAdvisoryGate(t *testing.T, db *gorm.DB, table, timing, triggerName, functionName string, key int64) *sceneDeleteGate {
	t.Helper()
	conn, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	lockedConn, err := conn.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockedConn.ExecContext(context.Background(), "SELECT pg_advisory_lock($1)", key); err != nil {
		_ = lockedConn.Close()
		t.Fatalf("lock advisory barrier: %v", err)
	}
	if err := db.Exec(fmt.Sprintf(`CREATE OR REPLACE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_lock(%d); IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF; END $$`, functionName, key)).Error; err != nil {
		_, _ = lockedConn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", key)
		_ = lockedConn.Close()
		t.Fatalf("create barrier function: %v", err)
	}
	if err := db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s", triggerName, table)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf("CREATE TRIGGER %s %s ON %s FOR EACH ROW EXECUTE FUNCTION %s()", triggerName, timing, table, functionName)).Error; err != nil {
		t.Fatal(err)
	}
	gate := &sceneDeleteGate{conn: lockedConn, key: key}
	t.Cleanup(func() { gate.release(t) })
	return gate
}

func (g *sceneDeleteGate) release(t *testing.T) {
	t.Helper()
	if g.conn == nil {
		return
	}
	if _, err := g.conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", g.key); err != nil {
		t.Errorf("release advisory barrier: %v", err)
	}
	if err := g.conn.Close(); err != nil {
		t.Errorf("close advisory barrier connection: %v", err)
	}
	g.conn = nil
}

func waitForSceneDeleteGate(t *testing.T, db *gorm.DB, queryFragment string) int {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var pid int
		if err := db.Raw(`SELECT pid FROM pg_stat_activity WHERE wait_event='advisory' AND query ILIKE ? ORDER BY query_start DESC LIMIT 1`, "%"+queryFragment+"%").Scan(&pid).Error; err != nil {
			t.Fatal(err)
		}
		if pid != 0 {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("operation did not reach advisory gate for %q", queryFragment)
	return 0
}

func waitForSceneDeviceLockWait(t *testing.T, db *gorm.DB, blockerPID int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var found bool
		if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE a.wait_event_type='Lock' AND ? = ANY(pg_blocking_pids(a.pid)))`, blockerPID).Scan(&found).Error; err != nil {
			t.Fatal(err)
		}
		if found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("competing operation did not wait on a device row lock")
}

func TestSceneAutomationCommitReplyLossPostgres(t *testing.T) {
	db := setupSceneDeleteBarrierDB(t)
	useSceneDeleteBarrierDB(t, db)
	deviceID := "commit-loss-" + uuid.NewString()
	sceneDeleteFixtureDevice(t, db, "tenant-a", deviceID)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool := &deleteCommitReplyLossPool{DB: sqlDB}
	wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	global.DB = wrapped
	query.SetDefault(wrapped)
	_, err = (&SceneAutomation{}).CreateSceneAutomation(sceneType10Req("commit-loss-"+uuid.NewString(), deviceID), &utils.UserClaims{ID: "scene-owner", TenantID: "tenant-a"})
	assertSceneErrorCode(t, err, errcode.CodeDBError)
	var code *errcode.Error
	if !errors.As(err, &code) || !strings.Contains(code.CustomMsg, "unknown") {
		t.Fatalf("commit error did not report unknown outcome: %v", err)
	}
	if got := pool.commits.Load(); got != 1 {
		t.Fatalf("commit calls=%d, want 1", got)
	}
	global.DB = db
	query.SetDefault(db)
	var count int64
	if err := db.Model(&model.SceneAutomation{}).Where("tenant_id=? AND name LIKE ?", "tenant-a", "commit-loss-%").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("scene was not durably committed despite lost reply: count=%d err=%v", count, err)
	}
}
