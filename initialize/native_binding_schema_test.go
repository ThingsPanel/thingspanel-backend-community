package initialize

import (
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// This integration test is intentionally destructive only inside the explicitly
// named qa_native_binding_test database. It is serialized and never runs in the
// ordinary unit-test suite unless TEST_NATIVE_BINDING_DSN is set.
func TestNativeBindingSchemaPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_NATIVE_BINDING_DSN"))
	if dsn == "" {
		t.Skip("TEST_NATIVE_BINDING_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL: %v", err)
	}
	defer func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}()

	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if databaseName != "qa_native_binding_test" {
		t.Fatalf("refusing destructive native binding test against database %q", databaseName)
	}

	t.Run("first and repeated startup", func(t *testing.T) {
		resetNativeBindingTestSchema(t, db)
		if err := ApplyNativeBindingSchema(db); err != nil {
			t.Fatalf("first startup: %v", err)
		}
		assertNativeBindingLedgerCount(t, db, 2)
		if err := ApplyNativeBindingSchema(db); err != nil {
			t.Fatalf("repeated startup: %v", err)
		}
		assertNativeBindingLedgerCount(t, db, 2)
	})

	t.Run("checksum drift stops startup", func(t *testing.T) {
		resetNativeBindingTestSchema(t, db)
		if err := ApplyNativeBindingSchema(db); err != nil {
			t.Fatalf("initial startup: %v", err)
		}
		if err := db.Exec(`UPDATE public.native_component_migration_ledger
			SET checksum = repeat('0', 64)
			WHERE module_name = ? AND file_name = ?`, nativeBindingMigrationModule, "27.sql").Error; err != nil {
			t.Fatalf("inject checksum drift: %v", err)
		}
		err := ApplyNativeBindingSchema(db)
		if err == nil || !strings.Contains(err.Error(), "checksum drift") {
			t.Fatalf("expected checksum drift error, got %v", err)
		}
	})

	t.Run("wrongly grouped check constraint stops startup", func(t *testing.T) {
		resetNativeBindingTestSchema(t, db)
		if err := ApplyNativeBindingSchema(db); err != nil {
			t.Fatalf("initial startup: %v", err)
		}
		if err := db.Exec(`ALTER TABLE public.device_configs
			DROP CONSTRAINT device_configs_native_binding_fields_check`).Error; err != nil {
			t.Fatalf("drop expected check: %v", err)
		}
		if err := db.Exec(`ALTER TABLE public.device_configs
			ADD CONSTRAINT device_configs_native_binding_fields_check CHECK (
				((native_thing_model_id IS NULL) AND (native_thing_model_snapshot_id IS NULL) AND (native_thing_model_revision IS NULL)
					OR (native_thing_model_id IS NOT NULL))
				AND ((native_thing_model_snapshot_id IS NOT NULL) AND (native_thing_model_revision IS NOT NULL) AND (native_thing_model_revision > 0))
			)`).Error; err != nil {
			t.Fatalf("add wrong grouped check: %v", err)
		}
		err := ApplyNativeBindingSchema(db)
		if err == nil || !strings.Contains(err.Error(), "device_configs_native_binding_fields_check") {
			t.Fatalf("expected native binding contract error, got %v", err)
		}
	})

	t.Run("same-name wrong column length stops startup", func(t *testing.T) {
		resetNativeBindingTestSchema(t, db)
		if err := db.Exec(`ALTER TABLE public.device_configs
			ADD COLUMN native_thing_model_id varchar(100)`).Error; err != nil {
			t.Fatalf("add wrong-length binding column: %v", err)
		}
		err := ApplyNativeBindingSchema(db)
		if err == nil || !strings.Contains(err.Error(), "native_thing_model_id has character length") {
			t.Fatalf("expected wrong binding column length error, got %v", err)
		}
	})

	t.Run("final migration failure rolls back earlier DDL and ledger", func(t *testing.T) {
		resetNativeBindingTestSchema(t, db)
		if err := db.Exec("CREATE TABLE public.market_native_model_imports (wrong text)").Error; err != nil {
			t.Fatalf("create final migration conflict: %v", err)
		}
		err := ApplyNativeBindingSchema(db)
		if err == nil || !strings.Contains(err.Error(), "execute native binding migration 27.sql") {
			t.Fatalf("expected failure executing final migration 27.sql, got %v", err)
		}
		var columnCount int64
		if err := db.Raw(`SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'device_configs'
			AND column_name IN ('native_thing_model_id', 'native_thing_model_snapshot_id', 'native_thing_model_revision', 'native_binding_revision')`).Scan(&columnCount).Error; err != nil {
			t.Fatalf("inspect rolled-back binding columns: %v", err)
		}
		if columnCount != 0 {
			t.Fatalf("failed initialization left %d native binding columns behind", columnCount)
		}
		var ledgerExists bool
		if err := db.Raw(`SELECT to_regclass('public.native_component_migration_ledger') IS NOT NULL`).Scan(&ledgerExists).Error; err != nil {
			t.Fatalf("inspect rolled-back migration ledger: %v", err)
		}
		if ledgerExists {
			t.Fatal("failed initialization left the migration ledger behind")
		}
	})
}

func resetNativeBindingTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("DROP TABLE IF EXISTS public.native_component_migration_ledger").Error; err != nil {
		t.Fatalf("drop test migration ledger: %v", err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS public.market_native_model_imports").Error; err != nil {
		t.Fatalf("drop test native imports table: %v", err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS public.device_configs").Error; err != nil {
		t.Fatalf("drop test device_configs table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE public.device_configs (
		id varchar(36) PRIMARY KEY,
		device_template_id varchar(36)
	)`).Error; err != nil {
		t.Fatalf("create minimal device_configs table: %v", err)
	}
}

func assertNativeBindingLedgerCount(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	var count int64
	if err := db.Raw(`SELECT count(*) FROM public.native_component_migration_ledger
		WHERE module_name = ?`, nativeBindingMigrationModule).Scan(&count).Error; err != nil {
		t.Fatalf("read native migration ledger: %v", err)
	}
	if count != expected {
		t.Fatalf("native migration ledger has %d rows, expected %d", count, expected)
	}
}
