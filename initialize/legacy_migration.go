package initialize

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const legacySchemaMigrationLock = int64(0x4c45474143595351)

// applyLegacySchemaMigrations preserves the original sys_version owner. It
// never copies its versions into the component ledger or edits old SQL files.
func applyLegacySchemaMigrations(db *gorm.DB, target int, version string, load func(int) ([]byte, error)) error {
	if target < 0 || load == nil {
		return fmt.Errorf("invalid legacy migration target")
	}
	return runPostgresOwnerMigration(db, "legacy-business", legacySchemaMigrationLock, func(tx *gorm.DB) error {
		var schemaName string
		if err := tx.Raw("SELECT current_schema()").Scan(&schemaName).Error; err != nil {
			return err
		}
		if schemaName == "" {
			return fmt.Errorf("legacy migration requires a current schema")
		}
		// Quote the database-returned identifier as one schema name, including
		// embedded dots or quotes. No caller-provided SQL fragment is accepted.
		ledger := `"` + strings.ReplaceAll(schemaName, `"`, `""`) + `"."sys_version"`
		if err := tx.Exec("CREATE TABLE IF NOT EXISTS " + ledger + " (version_number INT NOT NULL DEFAULT 0, version varchar(255) NOT NULL, PRIMARY KEY (version_number))").Error; err != nil {
			return err
		}
		var versions []struct {
			VersionNumber int `gorm:"column:version_number"`
		}
		if err := tx.Raw("SELECT version_number FROM " + ledger + " FOR UPDATE").Scan(&versions).Error; err != nil {
			return err
		}
		if len(versions) > 1 {
			return fmt.Errorf("legacy migration ledger must contain exactly one current version")
		}
		current := 0
		if len(versions) == 0 {
			inserted := tx.Exec("INSERT INTO "+ledger+" (version_number,version) VALUES (?,?)", 0, "0.0.0")
			if inserted.Error != nil {
				return inserted.Error
			}
			if inserted.RowsAffected != 1 {
				return fmt.Errorf("legacy migration initial version was not recorded exactly once")
			}
		} else {
			current = versions[0].VersionNumber
		}
		if current < 0 {
			return fmt.Errorf("legacy migration ledger contains an invalid version")
		}
		if current > target {
			return fmt.Errorf("database version %d is newer than program version %d", current, target)
		}
		if current == target {
			return nil
		}
		for number := current + 1; number <= target; number++ {
			contents, err := load(number)
			if err != nil {
				return fmt.Errorf("read legacy migration %d: %w", number, err)
			}
			if len(strings.TrimSpace(string(contents))) == 0 {
				return fmt.Errorf("legacy migration %d is empty", number)
			}
			if err := tx.Exec(string(contents)).Error; err != nil {
				return fmt.Errorf("execute legacy migration %d: %w", number, err)
			}
		}
		updated := tx.Exec("UPDATE "+ledger+" SET version_number=?,version=? WHERE version_number=?", target, version, current)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return fmt.Errorf("legacy migration final version was not recorded exactly once")
		}
		return nil
	})
}
