package initialize

import (
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
	return applyComponentMigrations(db, componentMigration{
		module: authMigrationModule, description: "authentication", lockKey: authMigrationLockKey,
		files: migrations, verify: verifyAuthSchema,
	})
}

func verifyAuthSchema(tx *gorm.DB) error {
	// Check the security-critical shape even when a migration is recorded.
	var valid bool
	if err := tx.Raw(`SELECT
          (SELECT count(*)=2 FROM information_schema.columns WHERE table_schema=current_schema() AND
           (table_name='users' OR table_name='app_push_devices') AND column_name='auth_version' AND data_type='bigint' AND is_nullable='NO')
          AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='auth_external_bindings'::regclass AND contype='u' AND pg_get_constraintdef(oid)='UNIQUE (issuer, subject)')
          AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='auth_external_bindings' AND column_name='self_unlinked' AND data_type='boolean' AND is_nullable='NO')
          AND to_regclass('auth_audit_events') IS NOT NULL
          AND to_regclass('auth_provider_settings') IS NOT NULL`).Scan(&valid).Error; err != nil || !valid {
		return fmt.Errorf("authentication schema verification failed: %v", err)
	}
	return nil
}
