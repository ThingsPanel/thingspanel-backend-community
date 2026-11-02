package initialize

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestDeviceIdentitySchemaPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_COMPONENT_MIGRATION_DSN")
	if dsn == "" {
		t.Skip("TEST_COMPONENT_MIGRATION_DSN required")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated component database")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil || name != "qa_component_migration_test" {
		t.Fatal("refusing non-fixture database")
	}
	reset := func() {
		t.Helper()
		if err := db.Exec(`DROP TABLE IF EXISTS public.devices, public.device_id_reservations CASCADE;
		DROP FUNCTION IF EXISTS public.enforce_device_identity_fence();
		CREATE TABLE public.devices(id text PRIMARY KEY,tenant_id text NOT NULL,created_at timestamptz);
		INSERT INTO devices(id,tenant_id) VALUES('historical-identity-longer-than-thirty-six-characters','tenant-fixture')`).Error; err != nil {
			t.Fatal(err)
		}
		if db.Migrator().HasTable(componentMigrationLedgerTable) {
			if err := db.Exec("DELETE FROM "+componentMigrationLedgerTable+" WHERE module_name=?", deviceIdentityMigrationModule).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Run("upgrade backfills identities and repeat does not resurrect tombstones", func(t *testing.T) {
		reset()
		if err := ApplyDeviceIdentitySchema(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("UPDATE device_id_reservations SET state='tombstoned',deleted_at=NOW()").Error; err != nil {
				return err
			}
			return tx.Exec("DELETE FROM devices").Error
		}); err != nil {
			t.Fatal(err)
		}
		if err := ApplyDeviceIdentitySchema(db); err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Table("device_id_reservations").Where("state='tombstoned'").Count(&count).Error; err != nil || count != 1 {
			t.Fatal("lost tombstone", count, err)
		}
	})
	t.Run("concurrent startup has one migration", func(t *testing.T) {
		reset()
		var wg sync.WaitGroup
		errors := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); errors <- ApplyDeviceIdentitySchema(db) }()
		}
		wg.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		var count int64
		if err := db.Table(componentMigrationLedgerTable).Where("module_name=?", deviceIdentityMigrationModule).Count(&count).Error; err != nil || count != 1 {
			t.Fatal(count, err)
		}
	})
	t.Run("ledger and active writer fence drift stop startup", func(t *testing.T) {
		reset()
		if err := ApplyDeviceIdentitySchema(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("ALTER TABLE devices DISABLE TRIGGER devices_identity_fence").Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyDeviceIdentitySchema(db); err == nil {
			t.Fatal("disabled writer fence accepted")
		}
		if err := db.Exec("ALTER TABLE devices ENABLE TRIGGER devices_identity_fence").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE "+componentMigrationLedgerTable+" SET checksum=repeat('0',64) WHERE module_name=?", deviceIdentityMigrationModule).Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyDeviceIdentitySchema(db); err == nil || !strings.Contains(err.Error(), "checksum drift") {
			t.Fatal("changed migration accepted", err)
		}
	})
	t.Run("wrong identity data prevents committing the migration", func(t *testing.T) {
		reset()
		if err := db.Exec(`CREATE TABLE device_id_reservations(id text PRIMARY KEY,tenant_id text NOT NULL,state varchar(16) NOT NULL,created_at timestamptz NOT NULL,deleted_at timestamptz);
			INSERT INTO device_id_reservations VALUES('historical-identity-longer-than-thirty-six-characters','different-tenant','active',NOW(),NULL)`).Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyDeviceIdentitySchema(db); err == nil {
			t.Fatal("conflicting historical identity accepted")
		}
		var count int64
		if db.Migrator().HasTable(componentMigrationLedgerTable) {
			if err := db.Table(componentMigrationLedgerTable).Where("module_name=?", deviceIdentityMigrationModule).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
		}
		if count != 0 {
			t.Fatal("failed backfill left a successful ledger")
		}
	})
}
