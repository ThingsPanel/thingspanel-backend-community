package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

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
