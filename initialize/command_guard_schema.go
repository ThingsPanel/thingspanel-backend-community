package initialize

import (
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
	files := make([]commandguardsql.NativeBindingMigration, 0, len(migrations))
	for _, migration := range migrations {
		files = append(files, commandguardsql.NativeBindingMigration{FileName: migration.FileName, Contents: migration.Contents})
	}
	return applyComponentMigrations(db, componentMigration{
		module: commandGuardMigrationModule, description: "command guard", lockKey: commandGuardLockKey,
		files: files, verify: verifyCommandGuardSchema,
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
