package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestCreateSonDeviceRejectsInvalidIdentity(t *testing.T) {
	d := &Device{}
	req := &model.CreateSonDeviceRes{ID: "parent", SonID: "child"}
	for _, claims := range []*utils.UserClaims{nil, {}, {ID: "user"}} {
		if err := d.CreateSonDevice(context.Background(), req, claims); err == nil {
			t.Fatal("accepted absent tenant identity")
		}
	}
	claims := &utils.UserClaims{ID: "user", TenantID: "tenant-a"}
	for _, req := range []*model.CreateSonDeviceRes{nil, {ID: "", SonID: "child"}, {ID: "parent", SonID: ""}, {ID: "parent", SonID: "child,,other"}, {ID: "parent", SonID: "parent"}} {
		if err := d.CreateSonDevice(context.Background(), req, claims); err == nil {
			t.Fatal("accepted malformed binding")
		}
	}
}

func TestDeviceParentBindPostgres(t *testing.T) {
	db := setupSceneDeleteBarrierDB(t)
	useSceneDeleteBarrierDB(t, db)
	claims := &utils.UserClaims{ID: "binder", TenantID: "tenant-a"}
	protocol := "c02-observe-disconnect"
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	gatewayCfg, childCfg, invalidCfg, foreignCfg := "bind-gateway-"+suffix, "bind-child-"+suffix, "bind-direct-"+suffix, "bind-foreign-"+suffix
	t.Cleanup(func() {
		global.REDIS.Del(context.Background(), gatewayCfg+"_config", childCfg+"_config", invalidCfg+"_config", foreignCfg+"_config")
	})
	for _, c := range []model.DeviceConfig{
		{ID: gatewayCfg, TenantID: "tenant-a", DeviceType: "2", ProtocolType: &protocol},
		{ID: childCfg, TenantID: "tenant-a", DeviceType: "3"},
		{ID: invalidCfg, TenantID: "tenant-a", DeviceType: "1"},
		{ID: foreignCfg, TenantID: "tenant-b", DeviceType: "3"},
	} {
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
	}
	var pluginLookups atomic.Int32
	callback := "c02-observe-plugin-lookup"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "service_plugins" {
			pluginLookups.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	seed := func(tenant, config string) string {
		t.Helper()
		id := "bind-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:25]
		d := model.Device{ID: id, TenantID: tenant, DeviceNumber: id, Voucher: id, IsEnabled: "enabled", ActivateFlag: "active", DeviceConfigID: &config}
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	parentOf := func(id string) *string {
		t.Helper()
		var d model.Device
		if err := db.Where("id=?", id).Take(&d).Error; err != nil {
			t.Fatal(err)
		}
		return d.ParentID
	}
	bind := func(parent string, children ...string) error {
		return (&Device{}).CreateSonDevice(context.Background(), &model.CreateSonDeviceRes{ID: parent, SonID: strings.Join(children, ",")}, claims)
	}
	assertNoEffect := func(before int32) {
		t.Helper()
		if pluginLookups.Load() != before {
			t.Fatal("failed/unknown binding notified plugin")
		}
	}
	waitDeviceLock := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var n int64
			err := db.Raw(`SELECT count(*) FROM pg_stat_activity a WHERE a.datname=current_database() AND a.wait_event_type='Lock' AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.relation='devices'::regclass)`).Scan(&n).Error
			if err != nil {
				t.Fatal(err)
			}
			if n > 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("no device row lock waiter observed")
	}

	t.Run("all_children_and_config_tenant_scope", func(t *testing.T) {
		parent := seed("tenant-a", gatewayCfg)
		for _, bad := range []string{seed("tenant-b", childCfg), seed("tenant-a", foreignCfg), seed("tenant-a", invalidCfg), "missing-child"} {
			child := seed("tenant-a", childCfg)
			before := pluginLookups.Load()
			if err := bind(parent, child, bad); err == nil {
				t.Fatal("accepted invalid child set")
			}
			if parentOf(child) != nil {
				t.Fatal("partial binding persisted")
			}
			assertNoEffect(before)
		}
		child := seed("tenant-a", childCfg)
		before := pluginLookups.Load()
		if bind(seed("tenant-b", gatewayCfg), child) == nil {
			t.Fatal("accepted foreign parent")
		}
		assertNoEffect(before)
		if bind(seed("tenant-a", invalidCfg), child) == nil {
			t.Fatal("accepted direct-device parent")
		}
		assertNoEffect(before)
	})
	t.Run("batch_deduplicated_success_and_existing_binding_rejected", func(t *testing.T) {
		parent, a, b := seed("tenant-a", gatewayCfg), seed("tenant-a", childCfg), seed("tenant-a", gatewayCfg)
		before := pluginLookups.Load()
		if err := bind(parent, a, b, a); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{a, b} {
			if p := parentOf(id); p == nil || *p != parent {
				t.Fatal("missing committed child")
			}
		}
		if pluginLookups.Load() != before+1 {
			t.Fatal("expected one post-commit parent disconnect attempt")
		}
		before = pluginLookups.Load()
		if bind(seed("tenant-a", gatewayCfg), a) == nil {
			t.Fatal("rebound existing child")
		}
		assertNoEffect(before)
	})
	t.Run("cycle_and_simultaneous_reverse_binding", func(t *testing.T) {
		a, b := seed("tenant-a", gatewayCfg), seed("tenant-a", gatewayCfg)
		if err := bind(a, b); err != nil {
			t.Fatal(err)
		}
		before := pluginLookups.Load()
		if bind(b, a) == nil {
			t.Fatal("accepted ancestor cycle")
		}
		assertNoEffect(before)
		x, y := seed("tenant-a", gatewayCfg), seed("tenant-a", gatewayCfg)
		done := make(chan error, 2)
		go func() { done <- bind(x, y) }()
		go func() { done <- bind(y, x) }()
		successes := 0
		for i := 0; i < 2; i++ {
			select {
			case err := <-done:
				if err == nil {
					successes++
				}
			case <-time.After(5 * time.Second):
				t.Fatal("reverse binding deadlocked")
			}
		}
		if successes != 1 {
			t.Fatalf("reverse binding successes=%d", successes)
		}
	})
	t.Run("late_update_error_rolls_back_whole_batch", func(t *testing.T) {
		parent, a, b := seed("tenant-a", gatewayCfg), seed("tenant-a", childCfg), seed("tenant-a", childCfg)
		if err := db.Exec(`CREATE FUNCTION reject_bind_child() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='` + b + `' THEN RAISE EXCEPTION 'injected binding failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_bind BEFORE UPDATE ON devices FOR EACH ROW EXECUTE FUNCTION reject_bind_child();`).Error; err != nil {
			t.Fatal(err)
		}
		before := pluginLookups.Load()
		err := bind(parent, a, b)
		if dropErr := db.Exec(`DROP TRIGGER reject_bind ON devices; DROP FUNCTION reject_bind_child();`).Error; dropErr != nil {
			t.Fatal(dropErr)
		}
		if err == nil {
			t.Fatal("accepted injected update failure")
		}
		if parentOf(a) != nil || parentOf(b) != nil {
			t.Fatal("partial batch committed")
		}
		assertNoEffect(before)
	})
	t.Run("zero_row_child_update_rolls_back_batch", func(t *testing.T) {
		parent, a, b := seed("tenant-a", gatewayCfg), seed("tenant-a", childCfg), seed("tenant-a", childCfg)
		if err := db.Exec(`CREATE FUNCTION skip_bind_child() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='` + b + `' THEN RETURN NULL; END IF; RETURN NEW; END $$; CREATE TRIGGER skip_bind BEFORE UPDATE ON devices FOR EACH ROW EXECUTE FUNCTION skip_bind_child();`).Error; err != nil {
			t.Fatal(err)
		}
		before := pluginLookups.Load()
		err := bind(parent, a, b)
		if dropErr := db.Exec(`DROP TRIGGER skip_bind ON devices; DROP FUNCTION skip_bind_child();`).Error; dropErr != nil {
			t.Fatal(dropErr)
		}
		if err == nil {
			t.Fatal("accepted zero affected child update")
		}
		if parentOf(a) != nil || parentOf(b) != nil {
			t.Fatal("partial batch committed")
		}
		assertNoEffect(before)
	})

	t.Run("delete_first_binding_waits_then_rejects", func(t *testing.T) {
		parent, child := seed("tenant-a", gatewayCfg), seed("tenant-a", childCfg)
		gate := db.Begin()
		if gate.Error != nil {
			t.Fatal(gate.Error)
		}
		defer gate.Rollback()
		if err := gate.Exec("SELECT id FROM devices WHERE id=? FOR UPDATE", parent).Error; err != nil {
			t.Fatal(err)
		}
		before := pluginLookups.Load()
		done := make(chan error, 1)
		go func() { done <- bind(parent, child) }()
		// A lock held by the deleting transaction prevents any binding commit.
		select {
		case err := <-done:
			t.Fatalf("binding escaped parent lock: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
		waitDeviceLock()
		if err := gate.Where("id=?", parent).Delete(&model.Device{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := gate.Commit().Error; err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("bound deleted parent")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("binding did not resume")
		}
		if parentOf(child) != nil {
			t.Fatal("orphan binding")
		}
		assertNoEffect(before)
	})
	t.Run("automatic_registration_create_uses_parent_delete_barrier", func(t *testing.T) {
		parent := seed("tenant-a", gatewayCfg)
		child := model.Device{ID: "auto-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:25], TenantID: "tenant-a", ParentID: &parent, Voucher: "auto", DeviceNumber: "auto", IsEnabled: "enabled", ActivateFlag: "active"}
		gate := db.Begin()
		if gate.Error != nil {
			t.Fatal(gate.Error)
		}
		defer gate.Rollback()
		if err := gate.Exec("SELECT id FROM devices WHERE id=? FOR UPDATE", parent).Error; err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- dal.CreateDevice(&child) }()
		select {
		case err := <-done:
			t.Fatalf("automatic child escaped parent lock: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
		waitDeviceLock()
		if err := gate.Where("id=?", parent).Delete(&model.Device{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := gate.Commit().Error; err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if !errors.Is(err, dal.ErrParentDeviceUnavailable) {
				t.Fatalf("automatic child err=%v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("automatic child did not resume")
		}
		var count int64
		if err := db.Model(&model.Device{}).Where("id=?", child.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("automatic orphan count=%d err=%v", count, err)
		}
	})

	t.Run("binding_first_real_delete_waits_then_observes_dependency", func(t *testing.T) {
		parent, child := seed("tenant-a", gatewayCfg), seed("tenant-a", childCfg)
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		defer unblock()
		callback := "c02-pause-before-child-update"
		if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			updates, ok := tx.Statement.Dest.(map[string]interface{})
			if ok && updates["parent_id"] == parent {
				close(entered)
				<-release
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Update().Remove(callback)
		bindDone := make(chan error, 1)
		go func() { bindDone <- bind(parent, child) }()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("binding never reached locked update")
		}
		deleteDone := make(chan error, 1)
		go func() { deleteDone <- (&Device{}).DeleteDevice(parent, claims) }()
		select {
		case err := <-deleteDone:
			unblock()
			t.Fatalf("delete escaped binding lock: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
		waitDeviceLock()
		unblock()
		select {
		case err := <-bindDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("binding did not commit")
		}
		select {
		case err := <-deleteDone:
			var e *errcode.Error
			if !errors.As(err, &e) || e.Code != 200063 {
				t.Fatalf("unexpected dependency error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("delete did not resume")
		}
	})
	t.Run("reparent_update_fence_and_activation_snapshot", func(t *testing.T) {
		parent, child := seed("tenant-a", gatewayCfg), seed("tenant-a", childCfg)
		if _, err := dal.UpdateDeviceByMap(child, "tenant-a", map[string]interface{}{"parent_id": &parent}); err != nil {
			t.Fatal(err)
		}
		foreign := seed("tenant-b", gatewayCfg)
		if _, err := dal.UpdateDeviceByMap(child, "tenant-a", map[string]interface{}{"parent_id": &foreign}); err == nil {
			t.Fatal("cross tenant reparent")
		}
		if _, err := dal.UpdateDeviceByMap(child, "tenant-a", map[string]interface{}{"parent_id": &parent, "device_config_id": &foreignCfg}); err == nil {
			t.Fatal("foreign effective configuration")
		}
		stale := model.Device{ID: child, TenantID: "tenant-a", ParentID: &parent, ActivateFlag: "active", IsEnabled: "enabled"}
		if err := dal.RemoveSubDevice(child, "tenant-a"); err != nil {
			t.Fatal(err)
		}
		if _, err := dal.UpdateDevice(&stale); err != nil {
			t.Fatal(err)
		}
		if parentOf(child) != nil {
			t.Fatal("activation restored stale parent")
		}
	})
	t.Run("commit_reply_loss_has_no_plugin_effect", func(t *testing.T) {
		parent, child := seed("tenant-a", gatewayCfg), seed("tenant-a", childCfg)
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		loss := &parentCommitLossPool{DB: pool}
		wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		before := pluginLookups.Load()
		global.DB = wrapped
		err = bind(parent, child)
		global.DB = db
		var code *errcode.Error
		if !errors.As(err, &code) || code.Code != errcode.CodeDBError || !strings.Contains(code.CustomMsg, "unknown") || loss.commits.Load() != 1 {
			t.Fatalf("unknown commit err=%v count=%d", err, loss.commits.Load())
		}
		if p := parentOf(child); p == nil || *p != parent {
			t.Fatal("injected successful commit missing")
		}
		assertNoEffect(before)
	})
	t.Run("rollback_reply_loss_has_no_plugin_effect", func(t *testing.T) {
		parent, child := seed("tenant-a", gatewayCfg), seed("tenant-a", childCfg)
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		loss := &parentCommitLossPool{DB: pool, rollbackLoss: true}
		wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		before := pluginLookups.Load()
		global.DB = wrapped
		err = bind(parent, child, "missing-child")
		global.DB = db
		var code *errcode.Error
		if !errors.As(err, &code) || code.Code != errcode.CodeDBError || !strings.Contains(code.CustomMsg, "unknown") || loss.commits.Load() != 0 {
			t.Fatalf("unknown rollback err=%v commits=%d", err, loss.commits.Load())
		}
		if parentOf(child) != nil {
			t.Fatal("failed rollback bound child")
		}
		assertNoEffect(before)
	})

}
