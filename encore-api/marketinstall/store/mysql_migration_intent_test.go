package store_test

import (
	"errors"
	"testing"

	"encore.app/pkg/ownermigration"
)

func TestMySQLMigrationRequiresOriginalApplyingReceiptBeforeDDL(t *testing.T) {
	for _, which := range []string{"rewritten_checksum", "rewritten_version", "premature_applied"} {
		t.Run(which, func(t *testing.T) {
			f := mysqlStore(t, true)
			// Return only this generated fixture to the exact version-3 schema.
			// The original runner must persist and observe a new version-4 intent.
			for _, statement := range []string{
				`ALTER TABLE market_install_operations DROP INDEX market_install_ready, DROP COLUMN claim_ready_at`,
				`DELETE FROM market_install_schema_migrations WHERE version='0004_claim_ready.sql'`,
			} {
				if _, err := f.db.ExecContext(f.ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			change := `SET NEW.checksum=REPEAT('e',64);`
			if which == "rewritten_version" {
				change = `SET NEW.version='0099_foreign_intent.sql';`
			} else if which == "premature_applied" {
				change = `SET NEW.state='applied'; SET NEW.applied_at=CURRENT_TIMESTAMP(6);`
			}
			if _, err := f.db.ExecContext(f.ctx, `CREATE TRIGGER tp_a08_rewrite_intent BEFORE INSERT ON market_install_schema_migrations FOR EACH ROW BEGIN `+change+` END`); err != nil {
				t.Fatal(err)
			}
			if err := f.store.MigrateMySQL(f.ctx); !errors.Is(err, ownermigration.ErrOutcomeUnknown) {
				t.Fatalf("changed original intent was not an uncertain refusal: %v", err)
			}
			var targetColumn, receipts int
			if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=? AND TABLE_NAME='market_install_operations' AND COLUMN_NAME='claim_ready_at'`, f.database).Scan(&targetColumn); err != nil {
				t.Fatal(err)
			}
			if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM market_install_schema_migrations`).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if targetColumn != 0 || receipts != 4 {
				t.Fatalf("DDL occurred before the original intent was verified: column=%d receipts=%d", targetColumn, receipts)
			}
			if err := f.store.VerifyMySQLSchema(f.ctx); err == nil {
				t.Fatal("changed original intent was reported as ready")
			}
		})
	}
}
