package initialize

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	commandguardsql "project/sql"

	"gorm.io/gorm"
)

const (
	commandGuardMigrationModule = "automation-command-guard"
	commandGuardLockKey         = int64(0x434d444755415244) // "CMDGUARD"
	commandGuardLedgerTable     = "public.native_component_migration_ledger"
)

// ApplyCommandGuardSchema applies the automation command guard's SQL assets with
// the same checksum ledger the other component schemas use. Reusing the ledger
// table keeps one place that answers "which component applied what, from which
// bytes".
func ApplyCommandGuardSchema(db *gorm.DB) error {
	migrations, err := commandguardsql.CommandGuardMigrations()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", commandGuardLockKey).Error; err != nil {
			return fmt.Errorf("acquire command guard migration lock: %w", err)
		}
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS ` + commandGuardLedgerTable + ` (
			module_name text NOT NULL,
			file_name text NOT NULL,
			checksum char(64) NOT NULL,
			applied_at timestamp with time zone NOT NULL DEFAULT NOW(),
			PRIMARY KEY (module_name, file_name)
		)`).Error; err != nil {
			return fmt.Errorf("create command guard migration ledger: %w", err)
		}

		for _, migration := range migrations {
			digest := sha256.Sum256(migration.Contents)
			checksum := hex.EncodeToString(digest[:])
			var recorded string
			result := tx.Table(commandGuardLedgerTable).
				Select("checksum").
				Where("module_name = ? AND file_name = ?", commandGuardMigrationModule, migration.FileName).
				Take(&recorded)
			if result.Error == nil {
				if recorded != checksum {
					return fmt.Errorf("command guard migration checksum drift for %s: recorded %s, source %s",
						migration.FileName, recorded, checksum)
				}
				continue
			}
			if result.Error != gorm.ErrRecordNotFound {
				return fmt.Errorf("read command guard migration ledger for %s: %w", migration.FileName, result.Error)
			}
			if err := tx.Exec(string(migration.Contents)).Error; err != nil {
				return fmt.Errorf("execute command guard migration %s: %w", migration.FileName, err)
			}
			if err := tx.Exec("INSERT INTO "+commandGuardLedgerTable+" (module_name, file_name, checksum) VALUES (?, ?, ?)",
				commandGuardMigrationModule, migration.FileName, checksum).Error; err != nil {
				return fmt.Errorf("record command guard migration %s: %w", migration.FileName, err)
			}
		}
		return verifyCommandGuardSchema(tx)
	})
}

// verifyCommandGuardSchema checks the columns the guard's SQL statements depend
// on. A missing column would otherwise surface as a runtime error on the first
// automation-issued command, i.e. exactly when it must not fail.
func verifyCommandGuardSchema(tx *gorm.DB) error {
	required := map[string]map[string]expectedNativeColumn{
		"automation_scope_generation": {
			"scope_key":  {dataType: "text", nullable: "NO"},
			"generation": {dataType: "bigint", nullable: "NO"},
		},
		"automation_command_guard": {
			"idempotency_key": {dataType: "text", nullable: "NO"},
			"scope_key":       {dataType: "text", nullable: "NO"},
			"generation":      {dataType: "bigint", nullable: "NO"},
			"payload_hash":    {dataType: "text", nullable: "NO"},
			"state":           {dataType: "text", nullable: "NO"},
			"http_status":     {dataType: "integer", nullable: "NO"},
			"response_body":   {dataType: "text", nullable: "NO"},
			"accepted_ref":    {dataType: "text", nullable: "NO"},
		},
	}
	for table, columns := range required {
		for name, expected := range columns {
			if err := verifyNativeColumn(tx, table, name, expected); err != nil {
				return fmt.Errorf("command guard schema: %w", err)
			}
		}
	}
	return nil
}
