package initialize

import (
	"os"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	componentsql "project/sql"
)

// The fixture is a separate, explicitly named database. These tests never use
// application configuration, default DSNs or a shared customer schema.
func TestMarketInstallSchemaPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_COMPONENT_MIGRATION_DSN")
	if dsn == "" {
		t.Skip("TEST_COMPONENT_MIGRATION_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated component migration database")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err := db.Raw("SELECT current_database()").Scan(&database).Error; err != nil || database != "qa_component_migration_test" {
		t.Fatal("refusing non-fixture component migration database")
	}
	baseline, err := os.ReadFile("../sql/19.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Use the real historical installation DDL, excluding unrelated menu data.
	parts := strings.SplitN(string(baseline), "-- Menu changes verified", 2)
	if len(parts) != 2 {
		t.Fatal("historical fixture boundary changed")
	}
	reset := func(t *testing.T) {
		t.Helper()
		if err := db.Exec(`DROP TABLE IF EXISTS public.market_bundle_binding_status, public.market_resource_mappings,
			public.market_installation_audit, public.market_bundle_installations,
			public.market_install_authorizations, public.market_install_config_receipts, public.native_component_migration_ledger CASCADE`).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(parts[0]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO market_bundle_installations(idempotency_key,bundle_key,bundle_version,tenant_id)
			VALUES ('old-key','old-bundle','1','tenant')`).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Run("upgrade preserves old identity and replay does not rewrite ledger", func(t *testing.T) {
		reset(t)
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal(err)
		}
		var digest string
		if err := db.Raw("SELECT request_digest FROM market_bundle_installations WHERE idempotency_key='old-key'").Scan(&digest).Error; err != nil || digest != "" {
			t.Fatal("legacy identity was fabricated", err)
		}
		var before string
		if err := db.Raw("SELECT string_agg(checksum || applied_at::text, ',' ORDER BY file_name) FROM native_component_migration_ledger WHERE module_name=?", marketInstallMigrationModule).Scan(&before).Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal(err)
		}
		var after string
		if err := db.Raw("SELECT string_agg(checksum || applied_at::text, ',' ORDER BY file_name) FROM native_component_migration_ledger WHERE module_name=?", marketInstallMigrationModule).Scan(&after).Error; err != nil || before != after {
			t.Fatal("replay rewrote migration evidence", err)
		}
	})
	t.Run("concurrent startup has one migration owner", func(t *testing.T) {
		reset(t)
		var wait sync.WaitGroup
		errors := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wait.Add(1)
			go func() { defer wait.Done(); errors <- ApplyMarketInstallSchema(db) }()
		}
		wait.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		var count int64
		if err := db.Table(componentMigrationLedgerTable).Where("module_name=?", marketInstallMigrationModule).Count(&count).Error; err != nil || count != 3 {
			t.Fatal("duplicate migration application", count, err)
		}
	})
	t.Run("existing owner upgrades without rehashing or adopting legacy plans", func(t *testing.T) {
		reset(t)
		files, err := componentsql.MarketInstallMigrations()
		if err != nil || len(files) != 3 || files[0].FileName != "30.sql" || files[1].FileName != "32.sql" || files[2].FileName != "market_install/003_exact_plan.sql" {
			t.Fatal("owner migration order changed", err)
		}
		// Apply the actual two historical owner migrations with the same runner
		// and checksums, then upgrade that existing receipt set.
		old := componentMigration{module: marketInstallMigrationModule, description: "fixture prior market installation", lockKey: marketInstallMigrationLock, files: files[:2], verify: func(*gorm.DB) error { return nil }}
		if err := applyComponentMigrations(db, old); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO market_install_authorizations(id,tenant_id,subject_id,authority,auth_version,purpose,request_key,request_digest,plan_digest,plan,session_hash,expires_at)
			VALUES ('old-grant','old-tenant','old-subject','TENANT_ADMIN',3,'a08.install.v1','old-original-key','old-request-digest',repeat('a',64),'{"steps":[{"value":1e+20}]}','old-session-marker',NOW()+INTERVAL '1 hour')`).Error; err != nil {
			t.Fatal(err)
		}
		var before, receiptsBefore string
		if err := db.Raw("SELECT row_to_json(a)::text FROM market_install_authorizations a WHERE id='old-grant'").Scan(&before).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Raw("SELECT string_agg(file_name || ':' || checksum || ':' || applied_at::text, ',' ORDER BY file_name) FROM native_component_migration_ledger WHERE module_name=?", marketInstallMigrationModule).Scan(&receiptsBefore).Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal(err)
		}
		var unchanged bool
		if err := db.Raw("SELECT (to_jsonb(a)-'plan_exact'-'plan_encoding')=CAST(? AS jsonb) AND plan_exact IS NULL AND plan_encoding IS NULL FROM market_install_authorizations a WHERE id='old-grant'", before).Scan(&unchanged).Error; err != nil || !unchanged {
			t.Fatal("schema upgrade changed an old plan/digest, grant identity or lifetime", err)
		}
		var receiptsAfter string
		if err := db.Raw("SELECT string_agg(file_name || ':' || checksum || ':' || applied_at::text, ',' ORDER BY file_name) FROM native_component_migration_ledger WHERE module_name=? AND file_name IN ('30.sql','32.sql')", marketInstallMigrationModule).Scan(&receiptsAfter).Error; err != nil || receiptsBefore != receiptsAfter {
			t.Fatal("upgrade rewrote historical owner receipts", err)
		}
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal("restart did not reuse verified owner receipts", err)
		}
	})
	t.Run("checksum and schema drift stop startup", func(t *testing.T) {
		reset(t)
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE native_component_migration_ledger SET checksum=repeat('0',64) WHERE module_name=?", marketInstallMigrationModule).Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyMarketInstallSchema(db); err == nil || !strings.Contains(err.Error(), "checksum drift") {
			t.Fatal("checksum drift was accepted", err)
		}
		reset(t)
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("ALTER TABLE market_bundle_installations ALTER COLUMN request_digest DROP NOT NULL").Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyMarketInstallSchema(db); err == nil {
			t.Fatal("schema drift hidden by ledger hit")
		}
		reset(t)
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("ALTER TABLE market_install_authorizations DROP CONSTRAINT market_install_authorizations_exact_plan_pair_check").Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyMarketInstallSchema(db); err == nil {
			t.Fatal("exact-plan pair constraint drift hidden by ledger hit")
		}
		reset(t)
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("ALTER TABLE market_install_authorizations ALTER COLUMN plan_exact TYPE varchar(10000)").Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyMarketInstallSchema(db); err == nil {
			t.Fatal("exact-plan storage type drift hidden by ledger hit")
		}
	})
	t.Run("failed verification rolls back new ddl and ledger", func(t *testing.T) {
		reset(t)
		if err := db.Exec("ALTER TABLE market_bundle_installations DROP CONSTRAINT uq_installation_idempotency_tenant").Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyMarketInstallSchema(db); err == nil {
			t.Fatal("missing idempotency constraint accepted")
		}
		if db.Migrator().HasColumn("market_bundle_installations", "request_digest") {
			t.Fatal("failed migration left partial DDL")
		}
		if db.Migrator().HasTable(componentMigrationLedgerTable) {
			t.Fatal("failed migration left ledger")
		}
	})
	t.Run("same file name in another owner is preserved", func(t *testing.T) {
		reset(t)
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal(err)
		}
		foreign := componentMigration{module: "fixture-other-owner", description: "fixture", lockKey: 37,
			files: []componentsql.NativeBindingMigration{{FileName: "30.sql", Contents: []byte("SELECT 1")}}, verify: func(*gorm.DB) error { return nil }}
		if err := applyComponentMigrations(db, foreign); err != nil {
			t.Fatal(err)
		}
		if err := ApplyMarketInstallSchema(db); err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Table(componentMigrationLedgerTable).Where("file_name=?", "30.sql").Count(&count).Error; err != nil || count != 2 {
			t.Fatal("owner identities were merged", count, err)
		}
	})
}
