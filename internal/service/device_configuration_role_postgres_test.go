package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestDeviceConfigurationRolePostgres(t *testing.T) {
	db := setupSceneDeleteBarrierDB(t)
	useSceneDeleteBarrierDB(t, db)
	if err := db.AutoMigrate(&model.Group{}, &model.RGroupDevice{}); err != nil {
		t.Fatal(err)
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	gateway, otherGateway, childConfig, direct, foreign := "cfg-gw-"+suffix, "cfg-gw2-"+suffix, "cfg-child-"+suffix, "cfg-direct-"+suffix, "cfg-foreign-"+suffix
	mqtt := "MQTT"
	for _, c := range []model.DeviceConfig{{ID: gateway, TenantID: "tenant-a", DeviceType: "2", ProtocolType: &mqtt}, {ID: otherGateway, TenantID: "tenant-a", DeviceType: "2", ProtocolType: &mqtt}, {ID: childConfig, TenantID: "tenant-a", DeviceType: "3", ProtocolType: &mqtt}, {ID: direct, TenantID: "tenant-a", DeviceType: "1", ProtocolType: &mqtt}, {ID: foreign, TenantID: "tenant-b", DeviceType: "2", ProtocolType: &mqtt}} {
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		global.REDIS.Del(context.Background(), gateway+"_config", otherGateway+"_config", childConfig+"_config", direct+"_config", foreign+"_config")
	})
	claims := &utils.UserClaims{ID: "config-owner", TenantID: "tenant-a"}
	seed := func(tenant string, config *string) string {
		t.Helper()
		id := "cfgdev-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
		d := model.Device{ID: id, TenantID: tenant, DeviceNumber: id, Voucher: id, IsEnabled: "enabled", ActivateFlag: "active", DeviceConfigID: config}
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	get := func(id string) model.Device {
		t.Helper()
		var d model.Device
		if err := db.Where("id=?", id).Take(&d).Error; err != nil {
			t.Fatal(err)
		}
		return d
	}
	bind := func(parent, child string) error {
		return (&Device{}).CreateSonDevice(context.Background(), &model.CreateSonDeviceRes{ID: parent, SonID: child}, claims)
	}
	waitLock := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var n int64
			if err := db.Raw(`SELECT count(*) FROM pg_stat_activity a WHERE a.datname=current_database() AND a.wait_event_type='Lock' AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.relation IN ('devices'::regclass,'device_configs'::regclass))`).Scan(&n).Error; err != nil {
				t.Fatal(err)
			}
			if n > 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("configuration/binding lock waiter absent")
	}
	receive := func(ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("configuration operation did not resume")
			return nil
		}
	}
	t.Run("configuration_switch_preserves_legal_roles_and_tenant", func(t *testing.T) {
		parent, child := seed("tenant-a", &gateway), seed("tenant-a", &childConfig)
		if err := bind(parent, child); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{parent, child} {
			for _, config := range []*string{&direct, &foreign, nil} {
				if err := dal.ReplaceTenantDeviceConfigurations(context.Background(), "tenant-a", []string{id}, config, false); err == nil {
					t.Fatal("accepted invalid role or foreign configuration")
				}
			}
		}
		if _, err := dal.UpdateDeviceByMap(child, "tenant-a", map[string]interface{}{"device_config_id": &direct}); err == nil {
			t.Fatal("ordinary update bypassed child role")
		}
		if err := (&Device{}).UpdateDeviceConfig(&model.ChangeDeviceConfigReq{DeviceID: parent, DeviceConfigID: &otherGateway}, claims); err != nil {
			t.Fatal(err)
		}
		if err := (&Device{}).UpdateDeviceConfig(&model.ChangeDeviceConfigReq{DeviceID: child, DeviceConfigID: &gateway}, claims); err != nil {
			t.Fatal(err)
		}
		if err := (&Device{}).UpdateDeviceConfig(&model.ChangeDeviceConfigReq{DeviceID: child, DeviceConfigID: &childConfig}, &utils.UserClaims{TenantID: "tenant-b"}); err == nil {
			t.Fatal("foreign tenant configured device")
		}
		if err := dal.UpdateDeviceConfigForTenant(otherGateway, "tenant-a", map[string]interface{}{"device_type": "1"}); !errors.Is(err, dal.ErrDeviceBindingInvalid) {
			t.Fatalf("shared parent type guard: %v", err)
		}
		if err := dal.UpdateDeviceConfigForTenant(gateway, "tenant-a", map[string]interface{}{"device_type": "1"}); !errors.Is(err, dal.ErrDeviceBindingInvalid) {
			t.Fatalf("shared child type guard: %v", err)
		}
		if err := dal.UpdateDeviceConfigWithBindingCAS(gateway, "tenant-a", 0, map[string]interface{}{"device_type": "3"}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("batch_rechecks_ownership_and_empty_configuration_atomically", func(t *testing.T) {
		a, b := seed("tenant-a", nil), seed("tenant-b", nil)
		req := &model.BatchUpdateDeviceConfigReq{DeviceIds: []string{a, b}, DeviceConfigID: gateway}
		if err := (&DeviceConfig{}).BatchUpdateDeviceConfig(req, claims); err == nil {
			t.Fatal("batch accepted foreign device")
		}
		if get(a).DeviceConfigID != nil {
			t.Fatal("partial mixed tenant batch")
		}
		b = seed("tenant-a", &childConfig)
		req.DeviceIds = []string{a, b}
		if err := (&DeviceConfig{}).BatchUpdateDeviceConfig(req, claims); err == nil {
			t.Fatal("batch overwrote configured device")
		}
		if get(a).DeviceConfigID != nil {
			t.Fatal("partial already-configured batch")
		}
		req.DeviceIds = []string{a}
		if err := (&DeviceConfig{}).BatchUpdateDeviceConfig(req, claims); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("binding_lock_makes_configuration_revalidate_new_role", func(t *testing.T) {
		parent, child := seed("tenant-a", &otherGateway), seed("tenant-a", &childConfig)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		name := "config-pause-binding"
		if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			v, ok := tx.Statement.Dest.(map[string]interface{})
			if ok && v["parent_id"] == parent {
				close(entered)
				<-release
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Update().Remove(name)
		bound := make(chan error, 1)
		go func() { bound <- bind(parent, child) }()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("binding did not acquire role locks")
		}
		changed := make(chan error, 1)
		go func() {
			changed <- dal.ReplaceTenantDeviceConfigurations(context.Background(), "tenant-a", []string{child}, &direct, false)
		}()
		waitLock()
		unblock()
		if err := receive(bound); err != nil {
			t.Fatal(err)
		}
		if err := receive(changed); !errors.Is(err, dal.ErrDeviceBindingInvalid) {
			t.Fatalf("configuration escaped committed child role: %v", err)
		}
		if d := get(child); d.DeviceConfigID == nil || *d.DeviceConfigID != childConfig || d.ParentID == nil {
			t.Fatal("invalid role persisted")
		}
	})
	t.Run("shared_role_update_rejects_new_unlocked_reference", func(t *testing.T) {
		config := "cfg-race-" + suffix
		if err := db.Create(&model.DeviceConfig{ID: config, TenantID: "tenant-a", DeviceType: "2"}).Error; err != nil {
			t.Fatal(err)
		}
		_ = seed("tenant-a", &config)
		newRef := seed("tenant-a", nil)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		var blockOnce sync.Once
		name := "config-pause-reference-snapshot"
		if err := db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table != "devices" || !strings.Contains(tx.Statement.SQL.String(), "device_config_id") {
				return
			}
			for _, v := range tx.Statement.Vars {
				if value, ok := v.(string); ok && value == config {
					blockOnce.Do(func() { close(entered); <-release })
					return
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Query().Remove(name)
		done := make(chan error, 1)
		go func() {
			done <- dal.UpdateDeviceConfigForTenant(config, "tenant-a", map[string]interface{}{"device_type": "1"})
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("reference snapshot not reached")
		}
		if err := dal.ReplaceTenantDeviceConfigurations(context.Background(), "tenant-a", []string{newRef}, &config, false); err != nil {
			t.Fatal(err)
		}
		unblock()
		if err := receive(done); !errors.Is(err, dal.ErrDeviceConfigurationConflict) {
			t.Fatalf("missing reference-plan conflict: %v", err)
		}
		var got model.DeviceConfig
		if err := db.Where("id=?", config).Take(&got).Error; err != nil || got.DeviceType != "2" {
			t.Fatalf("shared type changed across reference race: %+v %v", got, err)
		}
	})
	t.Run("new_root_reference_and_configuration_delete_serialize", func(t *testing.T) {
		config := "cfg-delete-" + suffix
		if err := db.Create(&model.DeviceConfig{ID: config, TenantID: "tenant-a", DeviceType: "1"}).Error; err != nil {
			t.Fatal(err)
		}
		id := "root-new-" + suffix
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		name := "config-pause-root-create"
		if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if d, ok := tx.Statement.Dest.([]*model.Device); ok && len(d) == 1 && d[0].ID == id {
				close(entered)
				<-release
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Create().Remove(name)
		created := make(chan error, 1)
		go func() {
			created <- dal.CreateDevice(&model.Device{ID: id, TenantID: "tenant-a", DeviceConfigID: &config, DeviceNumber: id, Voucher: id, IsEnabled: "enabled", ActivateFlag: "active"})
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("root creation did not hold configuration reference lock")
		}
		deleted := make(chan error, 1)
		go func() { deleted <- dal.DeleteDeviceConfigForTenant(config, "tenant-a") }()
		waitLock()
		unblock()
		if err := receive(created); err != nil {
			t.Fatal(err)
		}
		var inUse *dal.DeviceConfigurationInUseError
		if err := receive(deleted); !errors.As(err, &inUse) || inUse.Count != 1 {
			t.Fatalf("configuration deletion ignored committed root reference: %v", err)
		}
	})
	t.Run("configuration_delete_first_rejects_waiting_root_create", func(t *testing.T) {
		config := "cfg-deletefirst-" + suffix
		if err := db.Create(&model.DeviceConfig{ID: config, TenantID: "tenant-a", DeviceType: "1"}).Error; err != nil {
			t.Fatal(err)
		}
		id := "root-late-" + suffix
		gate := db.Begin()
		if gate.Error != nil {
			t.Fatal(gate.Error)
		}
		defer gate.Rollback()
		if err := gate.Exec("SELECT id FROM device_configs WHERE id=? FOR UPDATE", config).Error; err != nil {
			t.Fatal(err)
		}
		created := make(chan error, 1)
		go func() {
			created <- dal.CreateDevice(&model.Device{ID: id, TenantID: "tenant-a", DeviceConfigID: &config, DeviceNumber: id, Voucher: id, IsEnabled: "enabled", ActivateFlag: "active"})
		}()
		waitLock()
		if err := gate.Where("id=?", config).Delete(&model.DeviceConfig{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := gate.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if err := receive(created); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("new reference accepted deleted configuration: %v", err)
		}
		var count int64
		if err := db.Model(&model.Device{}).Where("id=?", id).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("root orphan count=%d err=%v", count, err)
		}
	})

}
