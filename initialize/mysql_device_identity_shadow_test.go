package initialize

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"project/internal/model"
)

// A session-local table named like the reservation table must not satisfy the fence in
// PostgreSQL (pg_temp is searched last). MySQL has no such pinning; this test records what
// actually happens so the deployment requirement in sql/mysql/002_device_identity.sql
// (the application account must not hold CREATE TEMPORARY TABLES) is a measured fact.
func TestDeviceIdentityFenceAgainstShadowingTemporaryTable(t *testing.T) {
	pg, my := openIdentityDialects(t)
	attempt := func(db *gorm.DB, create string) error {
		conn := db.Session(&gorm.Session{NewDB: true})
		tx := conn.Begin()
		defer tx.Rollback()
		if err := tx.Exec(create).Error; err != nil {
			t.Fatalf("temporary table: %v", err)
		}
		id := "shadow-" + uuid.NewString()[:8]
		if err := tx.Exec("INSERT INTO device_id_reservations (id, tenant_id, state) VALUES (?, 'tenant-s', 'active')", id).Error; err != nil {
			t.Fatalf("insert into the temporary table: %v", err)
		}
		return tx.Create(&model.Device{ID: id, Voucher: "v-" + id, TenantID: "tenant-s", DeviceNumber: "n-" + id}).Error
	}
	pgErr := attempt(pg, "CREATE TEMPORARY TABLE device_id_reservations (id text, tenant_id text, state varchar(16), created_at timestamptz, deleted_at timestamptz) ON COMMIT DROP")
	if pgErr == nil {
		t.Fatal("PostgreSQL must reject a device whose only reservation sits in a shadowing temporary table")
	}
	myErr := attempt(my, "CREATE TEMPORARY TABLE device_id_reservations (id VARCHAR(36), tenant_id VARCHAR(36), state VARCHAR(16), created_at DATETIME(6) NULL, deleted_at DATETIME(6) NULL)")
	t.Logf("MySQL with CREATE TEMPORARY TABLES privilege: device insert error=%v (nil means the fence was bypassed)", myErr)
	if myErr == nil {
		t.Log("RESULT: shadowing bypasses the MySQL fence; the privilege must be withheld from the application account")
	} else {
		t.Log("RESULT: the MySQL fence holds even against a shadowing temporary table")
	}
}
