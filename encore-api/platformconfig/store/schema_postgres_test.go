package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"encore.app/platformconfig/app"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// The initialize package tests the actual owner runner. This separate real-PG
// fixture tests the native host's read-only consumption of its exact receipt,
// SQL verifier and existing public schema; it is not a second migration runner.
func TestDictionaryPostgresNativeReadinessConsumesExactOwnerReceipt(t *testing.T) {
	_, private := postgresFixture(t) // applies the dedicated DB/role/loopback guard
	if err := New(private).ValidateReady(context.Background()); !errors.Is(err, app.ErrUnavailable) {
		t.Fatal("accepted an unadopted private schema")
	}
	config, err := pgx.ParseConfig(os.Getenv("TP_PLATFORMCONFIG_TEST_DSN"))
	if err != nil {
		t.Fatal("invalid dedicated fixture DSN")
	}
	if config.RuntimeParams == nil {
		config.RuntimeParams = map[string]string{}
	}
	config.RuntimeParams["search_path"] = "public"
	pool := stdlib.OpenDB(*config)
	pool.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = pool.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	guard, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.ExecContext(ctx, `SELECT pg_advisory_lock(81963339485141)`); err != nil {
		_ = guard.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = guard.ExecContext(context.Background(), `SELECT pg_advisory_unlock(81963339485141)`)
		_ = guard.Close()
	})
	var occupied bool
	if err := pool.QueryRowContext(ctx, `SELECT to_regclass('public.sys_dict') IS NOT NULL OR to_regclass('public.sys_dict_language') IS NOT NULL OR to_regclass('public.native_component_migration_ledger') IS NOT NULL OR to_regprocedure('public.platformconfig_dictionary_schema_ready()') IS NOT NULL`).Scan(&occupied); err != nil || occupied {
		t.Fatal("refusing to overwrite existing dictionary public fixtures")
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(`DROP FUNCTION IF EXISTS public.platformconfig_dictionary_schema_ready(); DROP TABLE IF EXISTS public.sys_dict_language; DROP TABLE IF EXISTS public.sys_dict; DROP TABLE IF EXISTS public.native_component_migration_ledger`); err != nil {
			t.Error(err)
		}
	})
	store := New(pool)
	unavailable := func() {
		t.Helper()
		if err := store.ValidateReady(ctx); !errors.Is(err, app.ErrUnavailable) {
			t.Fatal("unverified schema/receipt reported ready")
		}
	}
	unavailable()
	legacy, err := os.ReadFile(filepath.Join("..", "..", "..", "sql", "1.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sys_dict", "sys_dict_language"} {
		start := strings.Index(string(legacy), "CREATE TABLE public."+table+" (")
		if start < 0 {
			t.Fatal("legacy DDL absent")
		}
		rest := string(legacy[start:])
		end := strings.Index(rest, "\n);")
		if end < 0 {
			t.Fatal("legacy DDL incomplete")
		}
		if _, err := pool.ExecContext(ctx, rest[:end+3]); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "sql", DictionaryMigrationFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, string(contents)); err != nil {
		t.Fatal(err)
	}
	unavailable() // tables/function without an owner receipt do not enable SQL
	if _, err := pool.ExecContext(ctx, `CREATE TABLE public.native_component_migration_ledger(module_name text NOT NULL,file_name text NOT NULL,checksum char(64) NOT NULL,applied_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(module_name,file_name))`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, `INSERT INTO public.native_component_migration_ledger(module_name,file_name,checksum) VALUES($1,$2,$3)`, DictionaryMigrationModule, DictionaryMigrationFile, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	unavailable()
	if _, err := pool.ExecContext(ctx, `UPDATE public.native_component_migration_ledger SET checksum=$1 WHERE module_name=$2`, DictionaryMigrationChecksum, DictionaryMigrationModule); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateReady(ctx); err != nil {
		t.Fatal("exact adopted schema did not become ready: ", err)
	}
	if _, err := pool.ExecContext(ctx, `ALTER TABLE public.sys_dict ALTER COLUMN remark TYPE varchar(254)`); err != nil {
		t.Fatal(err)
	}
	unavailable()
	if _, err := pool.ExecContext(ctx, `ALTER TABLE public.sys_dict ALTER COLUMN remark TYPE varchar(255)`); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateReady(ctx); err != nil {
		t.Fatal("exact original schema not restored: ", err)
	}
	if _, err := pool.ExecContext(ctx, `ALTER FUNCTION public.platformconfig_dictionary_schema_ready() SET search_path = public`); err != nil {
		t.Fatal(err)
	}
	unavailable()
	if _, err := pool.ExecContext(ctx, `ALTER FUNCTION public.platformconfig_dictionary_schema_ready() SET search_path = pg_catalog, public`); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateReady(ctx); err != nil {
		t.Fatal("exact original verifier configuration not restored: ", err)
	}
	if _, err := pool.ExecContext(ctx, `CREATE OR REPLACE FUNCTION public.platformconfig_dictionary_schema_ready() RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT true $$`); err != nil {
		t.Fatal(err)
	}
	unavailable()
}
