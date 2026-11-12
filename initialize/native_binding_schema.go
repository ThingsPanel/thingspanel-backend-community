package initialize

import (
	"fmt"
	"regexp"
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
	return applyComponentMigrations(db, componentMigration{
		module: nativeBindingMigrationModule, description: "native binding", lockKey: nativeBindingLockKey,
		files: migrations, verify: verifyNativeBindingSchema,
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

// normalizeSchemaExpression produces the form used to compare a catalog-rendered
// expression with the contract text: dump/restore-stable, lower-cased, with
// whitespace collapsed.
func normalizeSchemaExpression(expression string) string {
	return strings.ToLower(strings.Join(strings.Fields(canonicalizeRestoredArrayAny(expression)), " "))
}

var (
	// `= ANY (ARRAY[('a'::character varying)::text, ('b'::character varying)::text])`
	// is how pg_get_constraintdef renders a varchar IN-list after pg_dump/pg_restore
	// (the dump text is re-parsed, so the per-element casts are no longer folded).
	restoredVarcharAny = regexp.MustCompile(`(?i)ANY \(ARRAY\[((?:\('(?:[^']|'')*'::character varying\)::text(?:, )?)+)\]\)`)
	restoredVarcharElt = regexp.MustCompile(`(?i)\(('(?:[^']|'')*'::character varying)\)::text`)
)

// canonicalizeRestoredArrayAny rewrites the post-restore rendering of a varchar
// `col IN ('a','b')` check into the fresh-install rendering
// `ANY ((ARRAY['a'::character varying, 'b'::character varying])::text[])`.
// Only the exact restored shape (every element a quoted literal cast to
// character varying and then text) is rewritten; the literals themselves are
// kept, so a different value list, a different column or a different operator
// still compares unequal. Any other text is returned unchanged.
func canonicalizeRestoredArrayAny(expression string) string {
	return restoredVarcharAny.ReplaceAllStringFunc(expression, func(match string) string {
		inner := restoredVarcharAny.FindStringSubmatch(match)[1]
		return "ANY ((ARRAY[" + restoredVarcharElt.ReplaceAllString(inner, "$1") + "])::text[])"
	})
}
