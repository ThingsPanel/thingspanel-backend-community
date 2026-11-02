package initialize

import (
	"os"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestServiceAccessSchemaPostgres(t *testing.T) {
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
	t.Cleanup(func() { _ = pool.Close() })
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil || name != "qa_component_migration_test" {
		t.Fatal("refusing non-fixture database")
	}
	reset := func() {
		t.Helper()
		if err := db.Exec("DROP TABLE IF EXISTS public.service_access_create_receipts").Error; err != nil {
			t.Fatal(err)
		}
		if db.Migrator().HasTable(componentMigrationLedgerTable) {
			if err := db.Exec("DELETE FROM "+componentMigrationLedgerTable+" WHERE module_name = ?", serviceAccessMigrationModule).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Run("concurrent_startup_is_one_owner_and_replay_preserves_receipt", func(t *testing.T) {
		reset()
		failures := make(chan error, 6)
		var wg sync.WaitGroup
		for i := 0; i < cap(failures); i++ {
			wg.Add(1)
			go func() { defer wg.Done(); failures <- ApplyServiceAccessSchema(db) }()
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Exec("INSERT INTO service_access_create_receipts(tenant_id,operation_key,subject_id,request_hash,access_id,resource_hash,created_at) VALUES('tenant','original-key','subject',repeat('a',64),'00000000-0000-0000-0000-000000000001',repeat('b',64),NOW())").Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyServiceAccessSchema(db); err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Table(componentMigrationLedgerTable).Where("module_name = ?", serviceAccessMigrationModule).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("migration count=%d err=%v", count, err)
		}
		if err := db.Table("service_access_create_receipts").Where("operation_key='original-key' AND request_hash=repeat('a',64) AND resource_hash=repeat('b',64)").Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("replay changed original receipt count=%d err=%v", count, err)
		}
	})
	t.Run("ledger_drift_and_missing_constraint_fail_closed", func(t *testing.T) {
		reset()
		if err := ApplyServiceAccessSchema(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("ALTER TABLE service_access_create_receipts DROP CONSTRAINT service_access_create_receipts_request_hash_check").Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyServiceAccessSchema(db); err == nil {
			t.Fatal("schema drift accepted from ledger hit")
		}
		reset()
		if err := ApplyServiceAccessSchema(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE "+componentMigrationLedgerTable+" SET checksum=repeat('0',64) WHERE module_name=?", serviceAccessMigrationModule).Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyServiceAccessSchema(db); err == nil || !strings.Contains(err.Error(), "checksum drift") {
			t.Fatalf("checksum drift=%v", err)
		}
	})
	t.Run("failed_verification_does_not_record_a_migration", func(t *testing.T) {
		reset()
		if err := db.Exec("CREATE TABLE service_access_create_receipts(tenant_id text, operation_key text)").Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyServiceAccessSchema(db); err == nil {
			t.Fatal("incompatible preexisting table adopted")
		}
		var count int64
		if err := db.Table(componentMigrationLedgerTable).Where("module_name=?", serviceAccessMigrationModule).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("failed migration recorded count=%d err=%v", count, err)
		}
	})
	t.Cleanup(reset)
}
