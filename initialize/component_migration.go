package initialize

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	componentsql "project/sql"
)

const componentMigrationLedgerTable = "public.native_component_migration_ledger"
const componentMigrationLedgerLock = int64(0x434f4d504c454447)

// componentMigration keeps the existing owner, lock and file identities. This
// runner only shares execution; it neither adopts another owner's migrations
// nor rewrites a checksum recorded by an earlier release.
type componentMigration struct {
	module      string
	description string
	lockKey     int64
	files       []componentsql.NativeBindingMigration
	verify      func(*gorm.DB) error
}

func applyComponentMigrations(db *gorm.DB, component componentMigration) error {
	if db == nil || component.module == "" || component.verify == nil {
		return fmt.Errorf("invalid component migration registration")
	}
	seen := make(map[string]bool, len(component.files))
	for _, file := range component.files {
		if file.FileName == "" || len(file.Contents) == 0 || seen[file.FileName] {
			return fmt.Errorf("invalid or duplicate %s migration %q", component.description, file.FileName)
		}
		seen[file.FileName] = true
	}
	return runPostgresOwnerMigration(db, component.module, component.lockKey, func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS ` + componentMigrationLedgerTable + ` (
			module_name text NOT NULL,
			file_name text NOT NULL,
			checksum char(64) NOT NULL,
			applied_at timestamp with time zone NOT NULL DEFAULT NOW(),
			PRIMARY KEY (module_name, file_name)
		)`).Error; err != nil {
			return fmt.Errorf("create component migration ledger: %w", err)
		}
		for _, file := range component.files {
			checksum := componentsql.MigrationChecksum(file.Contents)
			var recorded string
			result := tx.Table(componentMigrationLedgerTable).Select("checksum").
				Where("module_name = ? AND file_name = ?", component.module, file.FileName).Take(&recorded)
			if result.Error == nil {
				if !componentsql.MigrationChecksumMatches(recorded, file.Contents) {
					return fmt.Errorf("%s migration checksum drift for %s: recorded %s, source %s", component.description, file.FileName, recorded, checksum)
				}
				continue
			}
			if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read %s migration ledger for %s: %w", component.description, file.FileName, result.Error)
			}
			if err := tx.Exec(string(file.Contents)).Error; err != nil {
				return fmt.Errorf("execute %s migration %s: %w", component.description, file.FileName, err)
			}
			recordResult := tx.Exec("INSERT INTO "+componentMigrationLedgerTable+" (module_name, file_name, checksum) VALUES (?, ?, ?)", component.module, file.FileName, checksum)
			if recordResult.Error != nil {
				return fmt.Errorf("record %s migration %s: %w", component.description, file.FileName, recordResult.Error)
			}
			if recordResult.RowsAffected != 1 {
				return fmt.Errorf("record %s migration %s: version receipt was not inserted exactly once", component.description, file.FileName)
			}
		}
		return component.verify(tx)
	})
}
