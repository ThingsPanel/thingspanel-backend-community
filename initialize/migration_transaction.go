package initialize

import (
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ErrMigrationOutcomeUnknown never authorizes replay based on an assumed
// rollback. A subsequent startup acquires the same lock and reads the existing
// owner's ledger before deciding which migration remains outstanding.
var ErrMigrationOutcomeUnknown = errors.New("database migration outcome is unknown; inspect the existing owner ledger")

// runPostgresOwnerMigration shares transaction and exclusion mechanics only.
// Every caller retains its own historical ledger, migration bytes and lock.
// The existing database-scoped outer lock also excludes other component owners
// while a legacy schema is changing; it does not combine their version ledgers.
func runPostgresOwnerMigration(db *gorm.DB, owner string, ownerLock int64, apply func(*gorm.DB) error) (result error) {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" || owner == "" || ownerLock == 0 || apply == nil {
		return fmt.Errorf("invalid or unsupported PostgreSQL migration owner")
	}
	// A waiter must obtain a fresh snapshot after the preceding owner releases
	// the advisory lock, even if the connection's default isolation is stronger.
	tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if tx.Error != nil {
		return tx.Error
	}
	settled := false
	defer func() {
		if settled {
			return
		}
		if err := tx.Rollback().Error; err != nil {
			result = errors.Join(result, fmt.Errorf("%w: %s rollback acknowledgement failed: %v", ErrMigrationOutcomeUnknown, owner, err))
		}
	}()
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", componentMigrationLedgerLock).Error; err != nil {
		return fmt.Errorf("acquire schema migration exclusion: %w", err)
	}
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", ownerLock).Error; err != nil {
		return fmt.Errorf("acquire %s migration lock: %w", owner, err)
	}
	if err := apply(tx); err != nil {
		return err
	}
	err := tx.Commit().Error
	settled = true
	if err != nil {
		return fmt.Errorf("%w: %s commit acknowledgement failed: %v", ErrMigrationOutcomeUnknown, owner, err)
	}
	return nil
}
