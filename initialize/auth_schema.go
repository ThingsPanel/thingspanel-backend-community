package initialize

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	nativebindingsql "project/sql"

	"gorm.io/gorm"
)

const (
	authMigrationModule      = "auth"
	authMigrationLockKey     = int64(0x415554484d494752)
	authMigrationLedgerTable = "public.native_component_migration_ledger"
)

// ApplyAuthSchema applies the authentication component's SQL assets
// atomically and records their exact source checksums independently of
// sys_version. The schema is checked on every startup, including ledger hits.
func ApplyAuthSchema(db *gorm.DB) error {
	migrations, err := nativebindingsql.AuthMigrations()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", authMigrationLockKey).Error; err != nil {
			return fmt.Errorf("acquire authentication migration lock: %w", err)
		}
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS ` + authMigrationLedgerTable + ` (
			module_name text NOT NULL,
			file_name text NOT NULL,
			checksum char(64) NOT NULL,
			applied_at timestamp with time zone NOT NULL DEFAULT NOW(),
			PRIMARY KEY (module_name, file_name)
		)`).Error; err != nil {
			return fmt.Errorf("create component migration ledger: %w", err)
		}

		for _, migration := range migrations {
			digest := sha256.Sum256(migration.Contents)
			checksum := hex.EncodeToString(digest[:])
			var recorded string
			result := tx.Table(authMigrationLedgerTable).
				Select("checksum").
				Where("module_name = ? AND file_name = ?", authMigrationModule, migration.FileName).
				Take(&recorded)
			if result.Error == nil {
				if recorded != checksum {
					return fmt.Errorf("authentication migration checksum drift for %s: recorded %s, source %s", migration.FileName, recorded, checksum)
				}
				continue
			}
			if result.Error != gorm.ErrRecordNotFound {
				return fmt.Errorf("read authentication migration ledger for %s: %w", migration.FileName, result.Error)
			}

			if err := tx.Exec(string(migration.Contents)).Error; err != nil {
				return fmt.Errorf("execute authentication migration %s: %w", migration.FileName, err)
			}
			if err := tx.Exec("INSERT INTO "+authMigrationLedgerTable+" (module_name, file_name, checksum) VALUES (?, ?, ?)", authMigrationModule, migration.FileName, checksum).Error; err != nil {
				return fmt.Errorf("record authentication migration %s: %w", migration.FileName, err)
			}
		}
		// Check the security-critical shape even when a migration is recorded.
		var valid bool
		if err := tx.Raw(`SELECT
          (SELECT count(*)=2 FROM information_schema.columns WHERE table_schema=current_schema() AND
           (table_name='users' OR table_name='app_push_devices') AND column_name='auth_version' AND data_type='bigint' AND is_nullable='NO')
          AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='auth_external_bindings'::regclass AND contype='u' AND pg_get_constraintdef(oid)='UNIQUE (issuer, subject)')
          AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='auth_external_bindings' AND column_name='self_unlinked' AND data_type='boolean' AND is_nullable='NO')
          AND to_regclass('auth_audit_events') IS NOT NULL`).Scan(&valid).Error; err != nil || !valid {
			return fmt.Errorf("authentication schema verification failed: %v", err)
		}
		return nil
	})
}
