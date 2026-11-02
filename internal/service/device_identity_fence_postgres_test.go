package service

import (
	"errors"
	"gorm.io/gorm"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/utils"
	componentsql "project/sql"
	"strings"
	"testing"
	"time"
)

func TestDeviceIdentityDatabaseWriterFencePostgres(t *testing.T) {
	db := setupSceneDeleteBarrierDB(t)
	useSceneDeleteBarrierDB(t, db)
	// Real creation must reserve its identity through the production DAL, not
	// through the callback used by unrelated tests to seed legacy fixtures.
	if err := db.Callback().Create().Remove("fixture-device-reservation"); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Group{}, &model.RGroupDevice{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&dal.DeviceIDReservation{}); err != nil {
		t.Fatal(err)
	}
	files, err := componentsql.DeviceIdentityMigrations()
	if err != nil {
		t.Fatal(err)
	}
	apply := func() error {
		return db.Transaction(func(tx *gorm.DB) error {
			for _, file := range files {
				if err := tx.Exec(string(file.Contents)).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
	// An old writer already holds a device-table write lock when cutover starts.
	old := db.Begin()
	if old.Error != nil {
		t.Fatal(old.Error)
	}
	defer old.Rollback()
	if err := old.Exec(`INSERT INTO devices(id,tenant_id,voucher,device_number,is_enabled,activate_flag,is_online) VALUES('before-cutover','tenant-a','v','before-cutover','enabled','active',0)`).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- apply() }()
	deadline := time.Now().Add(5 * time.Second)
	waited := false
	for time.Now().Before(deadline) {
		var n int64
		if err := db.Raw(`SELECT count(*) FROM pg_locks WHERE relation='devices'::regclass AND NOT granted`).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			waited = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waited {
		t.Fatal("identity migration did not wait for old device writer")
	}
	if err := old.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("identity migration did not resume")
	}
	var reservation dal.DeviceIDReservation
	if err := db.Where("id='before-cutover'").Take(&reservation).Error; err != nil || reservation.State != "active" {
		t.Fatalf("cutover omitted committed in-flight device: %+v %v", reservation, err)
	}
	assertFence := func(statement, want string, args ...interface{}) {
		t.Helper()
		err := db.Exec(statement, args...).Error
		var state interface{ SQLState() string }
		if !errors.As(err, &state) || state.SQLState() != "23514" || !strings.Contains(err.Error(), want) {
			t.Fatalf("statement did not reach the expected identity fence: %s; error=%v, want SQLSTATE 23514 and %q", statement, err, want)
		}
	}
	for _, rejected := range []struct{ statement, reason string }{
		{`INSERT INTO devices(id,tenant_id,voucher,device_number,is_enabled,activate_flag,is_online) VALUES('old-unreserved','tenant-a','v','old-unreserved','enabled','active',0)`, "device INSERT requires an active tenant identity reservation"},
		{`DELETE FROM devices WHERE id='before-cutover'`, "device DELETE requires a tenant identity tombstone"},
		{`UPDATE devices SET id='renamed-old' WHERE id='before-cutover'`, "device identity and tenant are immutable"},
		{`UPDATE devices SET tenant_id='tenant-b' WHERE id='before-cutover'`, "device identity and tenant are immutable"},
	} {
		assertFence(rejected.statement, rejected.reason)
	}
	claims := &utils.UserClaims{ID: "owner", TenantID: "tenant-a"}
	id := "fenced-new-device"
	if _, err := (&Device{}).CreateDevice(model.CreateDeviceReq{ID: &id}, claims); err != nil {
		t.Fatalf("legal reserved create failed: %v", err)
	}
	if err := (&Device{}).DeleteDevice(id, claims); err != nil {
		t.Fatalf("legal tombstoned delete failed: %v", err)
	}
	assertFence(`INSERT INTO devices(id,tenant_id,voucher,device_number,is_enabled,activate_flag,is_online) VALUES(?,'tenant-a','v',?,'enabled','active',0)`, "device INSERT requires an active tenant identity reservation", id, id)
	if err := db.Exec(`INSERT INTO device_id_reservations(id,tenant_id,state) VALUES('foreign-reservation','tenant-b','active')`).Error; err != nil {
		t.Fatal(err)
	}
	assertFence(`INSERT INTO devices(id,tenant_id,voucher,device_number,is_enabled,activate_flag,is_online) VALUES('foreign-reservation','tenant-a','v','foreign-reservation','enabled','active',0)`, "device INSERT requires an active tenant identity reservation")
	if err := db.Exec(`UPDATE devices SET name='ordinary mutable name' WHERE id='before-cutover'`).Error; err != nil {
		t.Fatalf("fence blocked legal nonidentity update: %v", err)
	}
	if err := apply(); err != nil {
		t.Fatalf("same migration bytes are not replay-safe: %v", err)
	}
	var n int64
	if err := db.Model(&dal.DeviceIDReservation{}).Where("id=? AND state='tombstoned'", id).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("migration replay resurrected tombstone: %d %v", n, err)
	}
	if err := dal.CreateDevice(&model.Device{ID: id, TenantID: "tenant-a", Voucher: id, DeviceNumber: id, IsEnabled: "enabled", ActivateFlag: "active"}); !errors.Is(err, dal.ErrDeviceIDReserved) {
		t.Fatalf("DAL lost permanent conflict under real trigger: %v", err)
	}
}
