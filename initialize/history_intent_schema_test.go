package initialize

import (
	"os"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestHistoryIntentSchemaPostgres(t *testing.T) {
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
	if err := db.Exec("DROP TABLE IF EXISTS public.telemetry_history_intents, public.telemetry_history_deletions").Error; err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable(componentMigrationLedgerTable) {
		if err := db.Exec("DELETE FROM "+componentMigrationLedgerTable+" WHERE module_name = ?", historyIntentMigrationModule).Error; err != nil {
			t.Fatal(err)
		}
	}
	failures := make(chan error, 6)
	var wg sync.WaitGroup
	for i := 0; i < cap(failures); i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- ApplyHistoryIntentSchema(db) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO telemetry_history_intents(tenant_id,device_id,"key",ts,generation) VALUES('t','d','k',1,0)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyHistoryIntentSchema(db); err != nil {
		t.Fatal("replaying the applied migration must succeed")
	}
	var rows int
	if err := db.Raw("SELECT count(*) FROM telemetry_history_intents").Scan(&rows).Error; err != nil || rows != 1 {
		t.Fatalf("replay must preserve existing rows, rows=%d err=%v", rows, err)
	}
	var applied int
	if err := db.Raw("SELECT count(*) FROM "+componentMigrationLedgerTable+" WHERE module_name = ?", historyIntentMigrationModule).Scan(&applied).Error; err != nil || applied != 2 {
		t.Fatalf("one ledger entry per migration file (2) expected, got %d err=%v", applied, err)
	}
	if err := db.Exec(`INSERT INTO telemetry_history_intents(tenant_id,device_id,"key",ts,generation) VALUES('t','d','k',1,0)`).Error; err == nil {
		t.Fatal("duplicate identity must be rejected")
	}
}
