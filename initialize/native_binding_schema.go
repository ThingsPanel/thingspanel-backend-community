package initialize

import (
	"fmt"
	"strings"

	nativebindingsql "project/sql"

	"gorm.io/gorm"
)

const (
	nativeBindingMigrationModule = "native-binding"
	nativeBindingLockKey         = int64(0x4e41544956454249)
	nativeBindingLedgerTable     = "public.native_component_migration_ledger"
)

// ApplyNativeBindingSchema applies the native binding component's SQL assets
// atomically and records their exact source checksums independently of
// sys_version. The schema is checked on every startup, including ledger hits.
func ApplyNativeBindingSchema(db *gorm.DB) error {
	migrations, err := nativebindingsql.NativeBindingMigrations()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", nativeBindingLockKey).Error; err != nil {
			return fmt.Errorf("acquire native binding migration lock: %w", err)
		}
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS ` + nativeBindingLedgerTable + ` (
			module_name text NOT NULL,
			file_name text NOT NULL,
			checksum char(64) NOT NULL,
			applied_at timestamp with time zone NOT NULL DEFAULT NOW(),
			PRIMARY KEY (module_name, file_name)
		)`).Error; err != nil {
			return fmt.Errorf("create native component migration ledger: %w", err)
		}

		for _, migration := range migrations {
			checksum := nativebindingsql.MigrationChecksum(migration.Contents)
			var recorded string
			result := tx.Table(nativeBindingLedgerTable).
				Select("checksum").
				Where("module_name = ? AND file_name = ?", nativeBindingMigrationModule, migration.FileName).
				Take(&recorded)
			if result.Error == nil {
				if !nativebindingsql.MigrationChecksumMatches(recorded, migration.Contents) {
					return fmt.Errorf("native binding migration checksum drift for %s: recorded %s, source %s", migration.FileName, recorded, checksum)
				}
				continue
			}
			if result.Error != gorm.ErrRecordNotFound {
				return fmt.Errorf("read native binding migration ledger for %s: %w", migration.FileName, result.Error)
			}

			if err := tx.Exec(string(migration.Contents)).Error; err != nil {
				return fmt.Errorf("execute native binding migration %s: %w", migration.FileName, err)
			}
			if err := tx.Exec("INSERT INTO "+nativeBindingLedgerTable+" (module_name, file_name, checksum) VALUES (?, ?, ?)", nativeBindingMigrationModule, migration.FileName, checksum).Error; err != nil {
				return fmt.Errorf("record native binding migration %s: %w", migration.FileName, err)
			}
		}
		return verifyNativeBindingSchema(tx)
	})
}

type nativeSchemaColumn struct {
	DataType        string  `gorm:"column:data_type"`
	CharacterLength *int    `gorm:"column:character_maximum_length"`
	Nullable        string  `gorm:"column:is_nullable"`
	ColumnDefault   *string `gorm:"column:column_default"`
}

type expectedNativeColumn struct {
	dataType    string
	length      int
	nullable    string
	defaultZero bool
}

func verifyNativeBindingSchema(tx *gorm.DB) error {
	deviceColumns := map[string]expectedNativeColumn{
		"native_thing_model_id":          {dataType: "character varying", length: 36, nullable: "YES"},
		"native_thing_model_snapshot_id": {dataType: "character varying", length: 36, nullable: "YES"},
		"native_thing_model_revision":    {dataType: "integer", nullable: "YES"},
		"native_binding_revision":        {dataType: "integer", nullable: "NO", defaultZero: true},
	}
	for name, expected := range deviceColumns {
		if err := verifyNativeColumn(tx, "device_configs", name, expected); err != nil {
			return err
		}
	}

	constraints := map[string]string{
		"device_configs_native_binding_fields_check":           `CHECK ((((native_thing_model_id IS NULL) AND (native_thing_model_snapshot_id IS NULL) AND (native_thing_model_revision IS NULL)) OR ((native_thing_model_id IS NOT NULL) AND (native_thing_model_snapshot_id IS NOT NULL) AND (native_thing_model_revision IS NOT NULL) AND (native_thing_model_revision > 0))))`,
		"device_configs_native_binding_revision_check":         `CHECK ((native_binding_revision >= 0))`,
		"device_configs_native_legacy_binding_exclusive_check": `CHECK (((native_thing_model_id IS NULL) OR (device_template_id IS NULL)))`,
	}
	for name, expected := range constraints {
		if err := verifyNativeConstraint(tx, "device_configs", name, expected, "c"); err != nil {
			return err
		}
	}

	marketColumns := map[string]expectedNativeColumn{
		"id":                     {dataType: "uuid", nullable: "NO"},
		"tenant_id":              {dataType: "character varying", length: 100, nullable: "NO"},
		"market_template_id":     {dataType: "character varying", length: 100, nullable: "NO"},
		"market_version":         {dataType: "character varying", length: 128, nullable: "NO"},
		"source_model_id":        {dataType: "character varying", length: 36, nullable: "NO"},
		"source_snapshot_id":     {dataType: "character varying", length: 36, nullable: "NO"},
		"source_revision":        {dataType: "integer", nullable: "NO"},
		"dependency_checksum":    {dataType: "character", length: 64, nullable: "NO"},
		"created_by":             {dataType: "character varying", length: 100, nullable: "NO"},
		"status":                 {dataType: "character varying", length: 20, nullable: "NO"},
		"local_model_id":         {dataType: "character varying", length: 36, nullable: "YES"},
		"local_snapshot_id":      {dataType: "character varying", length: 36, nullable: "YES"},
		"local_revision":         {dataType: "integer", nullable: "YES"},
		"local_device_config_id": {dataType: "character varying", length: 36, nullable: "YES"},
		"created_at":             {dataType: "timestamp with time zone", nullable: "NO"},
		"updated_at":             {dataType: "timestamp with time zone", nullable: "NO"},
	}
	for name, expected := range marketColumns {
		if err := verifyNativeColumn(tx, "market_native_model_imports", name, expected); err != nil {
			return err
		}
	}
	return verifyNativeConstraint(tx, "market_native_model_imports", "uq_market_native_model_import_identity", `UNIQUE (tenant_id, market_template_id, market_version)`, "u")
}

func verifyNativeColumn(tx *gorm.DB, tableName, columnName string, expected expectedNativeColumn) error {
	var column nativeSchemaColumn
	result := tx.Raw(`SELECT data_type, character_maximum_length, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = ? AND column_name = ?`, tableName, columnName).Scan(&column)
	if result.Error != nil {
		return fmt.Errorf("inspect public.%s.%s: %w", tableName, columnName, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("required schema column public.%s.%s is missing", tableName, columnName)
	}
	if column.DataType != expected.dataType || column.Nullable != expected.nullable {
		return fmt.Errorf("schema column public.%s.%s has type/nullability %s/%s, expected %s/%s", tableName, columnName, column.DataType, column.Nullable, expected.dataType, expected.nullable)
	}
	if expected.length != 0 && (column.CharacterLength == nil || *column.CharacterLength != expected.length) {
		return fmt.Errorf("schema column public.%s.%s has character length %v, expected %d", tableName, columnName, column.CharacterLength, expected.length)
	}
	if expected.defaultZero {
		if column.ColumnDefault == nil || strings.Trim(strings.TrimSpace(*column.ColumnDefault), "()") != "0" {
			return fmt.Errorf("schema column public.%s.%s must default to 0", tableName, columnName)
		}
	}
	return nil
}

type nativeSchemaConstraint struct {
	ConstraintType string `gorm:"column:constraint_type"`
	Definition     string `gorm:"column:definition"`
	Validated      bool   `gorm:"column:validated"`
}

func verifyNativeConstraint(tx *gorm.DB, tableName, constraintName, expectedDefinition, expectedType string) error {
	var constraint nativeSchemaConstraint
	result := tx.Raw(`SELECT c.contype::text AS constraint_type, pg_get_constraintdef(c.oid) AS definition, c.convalidated AS validated
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = 'public' AND t.relname = ? AND c.conname = ?`, tableName, constraintName).Scan(&constraint)
	if result.Error != nil {
		return fmt.Errorf("inspect constraint public.%s.%s: %w", tableName, constraintName, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("required schema constraint public.%s.%s is missing", tableName, constraintName)
	}
	if constraint.ConstraintType != expectedType || !constraint.Validated || normalizeSchemaExpression(constraint.Definition) != normalizeSchemaExpression(expectedDefinition) {
		return fmt.Errorf("schema constraint public.%s.%s does not match the native binding contract", tableName, constraintName)
	}
	return nil
}

func normalizeSchemaExpression(expression string) string {
	return strings.ToLower(strings.Join(strings.Fields(expression), " "))
}
