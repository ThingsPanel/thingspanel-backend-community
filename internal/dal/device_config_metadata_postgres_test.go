package dal

import (
	"errors"
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"net/url"
	"os"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
	"sync"
	"testing"
	"time"
)

func metadataPG(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("Q02_METADATA_TEST_DSN")
	if dsn == "" {
		t.Skip("Q02_METADATA_TEST_DSN isolated PostgreSQL fixture required")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Host != "127.0.0.1:25543" || u.Path != "/notification_test" {
		t.Fatal("refusing non-local isolated fixture")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("q02_metadata_%d", time.Now().UnixNano())
	if e = admin.Exec("CREATE SCHEMA " + schema).Error; e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.DeviceConfig{}, &model.Device{}); e != nil {
		t.Fatal(e)
	}
	previous := global.DB
	global.DB = db
	query.SetDefault(db)
	t.Cleanup(func() {
		global.DB = previous
		if previous != nil {
			query.SetDefault(previous)
		}
		sql, _ := db.DB()
		sql.Close()
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		a, _ := admin.DB()
		a.Close()
	})
	return db
}
func TestDeviceConfigMetadataPostgres(t *testing.T) {
	db := metadataPG(t)
	protocol, other, secret, extra := "HTTP", `{"heartbeat":17,"extension":true}`, "fixture-secret", `[{"name":"preserved"}]`
	c := model.DeviceConfig{ID: "q02-a", TenantID: "tenant-a", Name: "original", DeviceType: "1", ProtocolType: &protocol, OtherConfig: &other, TemplateSecret: &secret, AdditionalInfo: &extra, AutoRegister: 1, NativeBindingRevision: 7, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if e := db.Create(&c).Error; e != nil {
		t.Fatal(e)
	}
	db.First(&c, "id = ?", c.ID)
	tag, _ := model.DeviceConfigETag(&c)
	cond, _ := model.ParseDeviceConfigCondition(tag)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, name := range []string{"writer-a", "writer-b"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			<-start
			_, e := UpdateDeviceConfigConditional(c.ID, c.TenantID, map[string]interface{}{"name": name}, nil, cond)
			results <- e
		}(name)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, model.ErrDeviceConfigVersionConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	var current model.DeviceConfig
	db.First(&current, "id = ?", c.ID)
	if *current.OtherConfig != other || *current.TemplateSecret != secret || *current.AdditionalInfo != extra || current.AutoRegister != 1 || current.NativeBindingRevision != 7 {
		t.Fatal("ordinary edit reset unrelated fields")
	}
	stamp := current.UpdatedAt
	if _, e := UpdateDeviceConfigConditional(c.ID, c.TenantID, map[string]interface{}{"protocol_type": "MQTT"}, nil, cond); !errors.Is(e, model.ErrDeviceConfigVersionConflict) {
		t.Fatal("stale protocol overwrite not rejected")
	}
	db.First(&current, "id = ?", c.ID)
	if *current.ProtocolType != "HTTP" || !stamp.Equal(current.UpdatedAt) {
		t.Fatal("conflict wrote state")
	}
	tag, _ = model.DeviceConfigETag(&current)
	fresh, _ := model.ParseDeviceConfigCondition(tag)
	if _, e := UpdateDeviceConfigConditional(c.ID, "tenant-b", map[string]interface{}{"name": "foreign"}, nil, fresh); e == nil {
		t.Fatal("cross-tenant write")
	}
	if _, e := UpdateDeviceConfigConditional("absent", c.TenantID, map[string]interface{}{"name": "missing"}, nil, fresh); e == nil {
		t.Fatal("missing row write")
	}
	r, e := UpdateDeviceConfigConditional(c.ID, c.TenantID, map[string]interface{}{"protocol_type": "MQTT"}, nil, fresh)
	if e != nil {
		t.Fatal(e)
	}
	if *r.Before.ProtocolType != "HTTP" || *r.After.ProtocolType != "MQTT" || r.After.VoucherType == nil || *r.After.VoucherType != "ACCESSTOKEN" {
		t.Fatal("derived credential not atomic")
	}
	db.First(&current, "id = ?", c.ID)
	got, _ := model.DeviceConfigETag(&current)
	returned, _ := model.DeviceConfigETag(r.After)
	if got != returned {
		t.Fatal("returned entity is stale")
	}
	cond, _ = model.ParseDeviceConfigCondition(got)
	db.Model(&model.DeviceConfig{}).Where("id = ?", c.ID).UpdateColumn("voucher_type", "BASIC")
	if _, e := UpdateDeviceConfigConditional(c.ID, c.TenantID, map[string]interface{}{"name": "stale-direct"}, nil, cond); !errors.Is(e, model.ErrDeviceConfigVersionConflict) {
		t.Fatal("direct writer bypassed content validator")
	}
	revision := 6
	if _, e := UpdateDeviceConfigConditional(c.ID, c.TenantID, map[string]interface{}{}, &revision, nil); !errors.Is(e, ErrNativeBindingRevisionConflict) {
		t.Fatal("binding CAS weakened")
	}
	revision = 7
	if _, e := UpdateDeviceConfigConditional(c.ID, c.TenantID, map[string]interface{}{}, &revision, cond); !errors.Is(e, model.ErrDeviceConfigVersionConflict) {
		t.Fatal("combined condition ignored")
	}
	if _, e := UpdateDeviceConfigConditional(c.ID, c.TenantID, map[string]interface{}{"name": "legacy-compatible"}, nil, nil); e != nil {
		t.Fatal(e)
	}
	if _, e := UpdateDeviceConfigConditional(c.ID, c.TenantID, map[string]interface{}{}, &revision, nil); e != nil {
		t.Fatal(e)
	}
	db.First(&current, "id = ?", c.ID)
	if current.NativeBindingRevision != 8 {
		t.Fatal("binding revision not incremented")
	}

	t.Run("binding mutual exclusion is checked after lock acquisition", func(t *testing.T) {
		binding := model.DeviceConfig{ID: "q02-binding", TenantID: c.TenantID, Name: "binding", DeviceType: "1"}
		if e := db.Create(&binding).Error; e != nil {
			t.Fatal(e)
		}
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		var locked model.DeviceConfig
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", binding.ID).Error; e != nil {
			t.Fatal(e)
		}
		if e := tx.Model(&model.DeviceConfig{}).Where("id = ?", binding.ID).Update("device_template_id", "legacy").Error; e != nil {
			t.Fatal(e)
		}
		finished := make(chan error, 1)
		zero := 0
		go func() {
			_, e := UpdateDeviceConfigConditional(binding.ID, binding.TenantID, map[string]interface{}{"native_thing_model_id": "native", "native_thing_model_snapshot_id": "snapshot", "native_thing_model_revision": 1}, &zero, nil)
			finished <- e
		}()
		select {
		case e := <-finished:
			t.Fatalf("did not wait for owner row lock: %v", e)
		case <-time.After(80 * time.Millisecond):
		}
		if e := tx.Commit().Error; e != nil {
			t.Fatal(e)
		}
		select {
		case e := <-finished:
			if !errors.Is(e, ErrDeviceConfigBindingConflict) {
				t.Fatalf("stale native binding did not see committed legacy: %v", e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("binding transaction did not finish")
		}
		switched, e := UpdateDeviceConfigConditional(binding.ID, binding.TenantID, map[string]interface{}{"native_thing_model_id": "native", "native_thing_model_snapshot_id": "snapshot", "native_thing_model_revision": 1, "device_template_id": nil}, &zero, nil)
		if e != nil || switched.After.DeviceTemplateID != nil || switched.After.NativeThingModelID == nil {
			t.Fatalf("explicit legacy clear should permit native switch: %v", e)
		}
		if _, e = UpdateDeviceConfigConditional(binding.ID, binding.TenantID, map[string]interface{}{"device_template_id": "legacy"}, nil, nil); !errors.Is(e, ErrDeviceConfigBindingConflict) {
			t.Fatal("reverse binding must also reject final double binding")
		}
		one := 1
		_, e = UpdateDeviceConfigConditional(binding.ID, binding.TenantID, map[string]interface{}{"device_template_id": "legacy", "native_thing_model_id": nil, "native_thing_model_snapshot_id": nil, "native_thing_model_revision": nil}, &one, nil)
		if e != nil {
			t.Fatal("explicit native clear should permit legacy switch", e)
		}
	})
}
