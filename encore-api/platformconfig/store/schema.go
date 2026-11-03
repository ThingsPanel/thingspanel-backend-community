package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"

	"encore.app/pkg/sqldialect"
	"encore.app/platformconfig/app"
)

// The migration bytes remain in the existing Community migration module.
// This host consumes its exact receipt; it never starts another migrator.
const DictionaryMigrationModule = "platformconfig-dictionary"
const DictionaryMigrationFile = "platformconfig/001_adopt_dictionary.sql"
const DictionaryMigrationChecksum = "f1ae3b1a2a8752526587638b9b8791f7648f520911c7641aac28b064e6f1ef54"
const DictionarySchemaFunctionChecksum = "33aed8960796c46024ba26927e8c7bfa95b2952f242e1b09ebd70a9622ab654e"

// ValidateReady checks the already adopted public schema. No SQL DDL or
// automatic database selection is performed here. All failure details stay in
// the owner, rather than becoming SQL/credential-bearing HTTP responses.
func (s *Store) ValidateReady(ctx context.Context) error {
	if s == nil || s.db == nil || ctx == nil {
		return app.ErrUnavailable
	}
	if s.dialect == sqldialect.MySQL {
		return s.validateReadyMySQL(ctx)
	}
	var schema, receipt string
	if err := s.db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil || schema != "public" {
		return app.ErrUnavailable
	}
	if err := s.db.QueryRowContext(ctx, `SELECT checksum FROM public.native_component_migration_ledger WHERE module_name=$1 AND file_name=$2`, DictionaryMigrationModule, DictionaryMigrationFile).Scan(&receipt); err != nil || receipt != DictionaryMigrationChecksum {
		return app.ErrUnavailable
	}
	var source string
	var stable, invoker, sqlFunction, configMatches bool
	// Function SET options are not part of prosrc. An altered search_path must
	// not inherit the original source checksum and report the schema as ready.
	if err := s.db.QueryRowContext(ctx, `SELECT p.prosrc,p.provolatile='s',NOT p.prosecdef,l.lanname='sql',COALESCE(p.proconfig=ARRAY['search_path=pg_catalog, public']::text[],false) FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_language l ON l.oid=p.prolang WHERE p.oid=pg_catalog.to_regprocedure('public.platformconfig_dictionary_schema_ready()')`).Scan(&source, &stable, &invoker, &sqlFunction, &configMatches); err != nil || !stable || !invoker || !sqlFunction || !configMatches {
		return app.ErrUnavailable
	}
	sum := sha256.Sum256([]byte(source))
	if hex.EncodeToString(sum[:]) != DictionarySchemaFunctionChecksum {
		return app.ErrUnavailable
	}
	var ready bool
	if err := s.db.QueryRowContext(ctx, `SELECT public.platformconfig_dictionary_schema_ready()`).Scan(&ready); err != nil || !ready {
		return app.ErrUnavailable
	}
	return nil
}

// The MySQL form of the same contract, written to the owner's MySQL migration (sql/mysql/009).
const DictionaryMySQLMigrationFile = "009_platformconfig_dictionary.sql"
const DictionaryMySQLMigrationChecksum = "2bbd7e99c5e3cef362ed3bfc4c1d061f4646a17201b65932642128c74a4bdba6"

type mysqlColumn struct {
	table, name, dataType string
	length                int
	nullable              string
}

// validateReadyMySQL checks the exact migration receipt and then the live definition of both tables,
// including columns nobody expects: any difference is "unavailable". No DDL runs here.
func (s *Store) validateReadyMySQL(ctx context.Context) error {
	var database sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT DATABASE()`).Scan(&database); err != nil || !database.Valid {
		return app.ErrUnavailable
	}
	var receipt string
	if err := s.db.QueryRowContext(ctx, `SELECT checksum FROM native_component_migration_ledger WHERE module_name=? AND file_name=?`, DictionaryMigrationModule, DictionaryMySQLMigrationFile).Scan(&receipt); err != nil || receipt != DictionaryMySQLMigrationChecksum {
		return app.ErrUnavailable
	}
	expected := map[string]mysqlColumn{}
	for _, c := range []mysqlColumn{
		{"sys_dict", "id", "varchar", 36, "NO"}, {"sys_dict", "dict_code", "varchar", 36, "NO"}, {"sys_dict", "dict_value", "varchar", 255, "NO"},
		{"sys_dict", "created_at", "datetime", 0, "NO"}, {"sys_dict", "remark", "varchar", 255, "YES"},
		{"sys_dict_language", "id", "varchar", 36, "NO"}, {"sys_dict_language", "dict_id", "varchar", 36, "NO"},
		{"sys_dict_language", "language_code", "varchar", 36, "NO"}, {"sys_dict_language", "translation", "varchar", 255, "NO"},
	} {
		expected[c.table+"."+c.name] = c
	}
	rows, err := s.db.QueryContext(ctx, `SELECT table_name,column_name,data_type,COALESCE(character_maximum_length,0),is_nullable,COALESCE(column_default,''),extra,COALESCE(datetime_precision,0)
		FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name IN ('sys_dict','sys_dict_language')`)
	if err != nil {
		return app.ErrUnavailable
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var table, name, dataType, nullable, def, extra string
		var length, precision int
		if err := rows.Scan(&table, &name, &dataType, &length, &nullable, &def, &extra, &precision); err != nil {
			return app.ErrUnavailable
		}
		want, ok := expected[table+"."+name]
		if !ok || dataType != want.dataType || length != want.length || nullable != want.nullable || def != "" || extra != "" {
			return app.ErrUnavailable
		}
		if name == "created_at" && precision != 6 {
			return app.ErrUnavailable
		}
		seen++
	}
	if err := rows.Err(); err != nil || seen != len(expected) {
		return app.ErrUnavailable
	}
	var constraints int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.table_constraints
		WHERE table_schema=DATABASE() AND ((table_name='sys_dict' AND constraint_name IN ('PRIMARY','sys_dict_dict_code_dict_value_key'))
		OR (table_name='sys_dict_language' AND constraint_name IN ('PRIMARY','sys_dict_language_dict_id_language_code_key','sys_dict_language_dict_id_fkey')))`).Scan(&constraints); err != nil || constraints != 5 {
		return app.ErrUnavailable
	}
	var cascade int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.referential_constraints
		WHERE constraint_schema=DATABASE() AND constraint_name='sys_dict_language_dict_id_fkey' AND referenced_table_name='sys_dict' AND delete_rule='CASCADE'`).Scan(&cascade); err != nil || cascade != 1 {
		return app.ErrUnavailable
	}
	var uniqueColumns int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.key_column_usage
		WHERE table_schema=DATABASE() AND ((constraint_name='sys_dict_dict_code_dict_value_key' AND table_name='sys_dict' AND column_name IN ('dict_code','dict_value'))
		OR (constraint_name='sys_dict_language_dict_id_language_code_key' AND table_name='sys_dict_language' AND column_name IN ('dict_id','language_code')))`).Scan(&uniqueColumns); err != nil || uniqueColumns != 4 {
		return app.ErrUnavailable
	}
	return nil
}
