package initialize

import (
	"context"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	componentsql "project/sql"
)

// This test may adopt only the dedicated platformconfig database. All public
// objects must be absent beforehand; cleanup never drops pre-existing tables.
func platformConfigMigrationFixture(t *testing.T) (*gorm.DB, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("TP_PLATFORMCONFIG_TEST_DSN")
	if dsn == "" {
		t.Skip("TP_PLATFORMCONFIG_TEST_DSN is required for real dictionary schema adoption")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid dictionary fixture DSN")
	}
	loopback := func(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
	if !loopback(config.Host) || config.Database != "tp_takeover_platformconfig" || config.User != "platformconfig_fixture" {
		t.Fatal("refusing non-dedicated dictionary migration fixture")
	}
	for _, fallback := range config.Fallbacks {
		if !loopback(fallback.Host) {
			t.Fatal("refusing non-loopback fixture fallback")
		}
	}
	if config.RuntimeParams == nil {
		config.RuntimeParams = map[string]string{}
	}
	config.RuntimeParams["search_path"] = "public"
	pool := stdlib.OpenDB(*config)
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = pool.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var database, user string
	if err := pool.QueryRowContext(ctx, `SELECT current_database(),current_user`).Scan(&database, &user); err != nil || database != "tp_takeover_platformconfig" || user != "platformconfig_fixture" {
		t.Fatal("actual dictionary fixture identity mismatch")
	}
	// A fixture-only session lock prevents two invocations from sharing this
	// database's required public schema. The production runner has its own lock.
	guard, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = guard.ExecContext(ctx, `SELECT pg_advisory_lock(81963339485141)`); err != nil {
		_ = guard.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = guard.ExecContext(context.Background(), `SELECT pg_advisory_unlock(81963339485141)`)
		_ = guard.Close()
	})
	var occupied bool
	if err := pool.QueryRowContext(ctx, `SELECT to_regclass('public.sys_dict') IS NOT NULL OR to_regclass('public.sys_dict_language') IS NOT NULL OR to_regclass('public.native_component_migration_ledger') IS NOT NULL OR to_regprocedure('public.platformconfig_dictionary_schema_ready()') IS NOT NULL OR to_regprocedure('public.tp_dictionary_reject_receipt()') IS NOT NULL`).Scan(&occupied); err != nil || occupied {
		t.Fatal("dictionary public fixture objects already exist; refusing destructive setup")
	}
	// Everything named here was absent and belongs to this guarded fixture.
	t.Cleanup(func() {
		_, err := pool.Exec(`DROP FUNCTION IF EXISTS public.platformconfig_dictionary_schema_ready(); DROP TABLE IF EXISTS public.sys_dict_language; DROP TABLE IF EXISTS public.sys_dict; DROP TABLE IF EXISTS public.native_component_migration_ledger; DROP FUNCTION IF EXISTS public.tp_dictionary_reject_receipt()`)
		if err != nil {
			t.Error(err)
		}
	})
	legacy, err := os.ReadFile(filepath.Join("..", "sql", "1.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sys_dict", "sys_dict_language"} {
		start := strings.Index(string(legacy), "CREATE TABLE public."+table+" (")
		if start < 0 {
			t.Fatal("missing historical table DDL")
		}
		remainder := string(legacy[start:])
		end := strings.Index(remainder, "\n);")
		if end < 0 {
			t.Fatal("incomplete historical table DDL")
		}
		if _, err := pool.ExecContext(ctx, remainder[:end+3]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.ExecContext(ctx, `INSERT INTO public.sys_dict(id,dict_code,dict_value,created_at,remark) VALUES('old-opaque-id','legacy','original','2024-01-18T15:39:38.469Z','retained'); INSERT INTO public.sys_dict_language VALUES('old-opaque-language','old-opaque-id','en_US','old translation')`); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return db, pool
}

func TestPlatformConfigSchemaPostgresAdoptionAndRestart(t *testing.T) {
	db, pool := platformConfigMigrationFixture(t)
	var start sync.WaitGroup
	start.Add(1)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { start.Wait(); done <- ApplyPlatformConfigSchema(db) }()
	}
	start.Done()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal("concurrent owner adoption: ", err)
		}
	}
	if err := ApplyPlatformConfigSchema(db); err != nil {
		t.Fatal("restart ledger replay: ", err)
	}
	var count int
	if err := pool.QueryRow(`SELECT count(*) FROM public.native_component_migration_ledger WHERE module_name='platformconfig-dictionary'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("owner receipts=%d error=%v", count, err)
	}
	var value, translation, remark string
	if err := pool.QueryRow(`SELECT d.dict_value,l.translation,d.remark FROM public.sys_dict d JOIN public.sys_dict_language l ON l.dict_id=d.id WHERE d.id='old-opaque-id'`).Scan(&value, &translation, &remark); err != nil || value != "original" || translation != "old translation" || remark != "retained" {
		t.Fatal("adoption changed legacy data")
	}
	files, err := componentsql.PlatformConfigMigrations()
	if err != nil {
		t.Fatal(err)
	}
	checksum := componentsql.MigrationChecksum(files[0].Contents)
	if err := db.Exec(`UPDATE public.native_component_migration_ledger SET checksum=? WHERE module_name='platformconfig-dictionary'`, strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlatformConfigSchema(db); err == nil {
		t.Fatal("accepted migration checksum drift")
	}
	if err := db.Exec(`UPDATE public.native_component_migration_ledger SET checksum=? WHERE module_name='platformconfig-dictionary'`, checksum).Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlatformConfigSchema(db); err != nil {
		t.Fatal("restored exact receipt: ", err)
	}
}

func TestPlatformConfigSchemaPostgresRejectsSchemaAndVerifierDrift(t *testing.T) {
	for _, test := range []struct{ name, change, restore string }{
		{"column-type", `ALTER TABLE public.sys_dict ALTER COLUMN remark TYPE varchar(254)`, `ALTER TABLE public.sys_dict ALTER COLUMN remark TYPE varchar(255)`},
		{"default", `ALTER TABLE public.sys_dict ALTER COLUMN created_at SET DEFAULT now()`, `ALTER TABLE public.sys_dict ALTER COLUMN created_at DROP DEFAULT`},
		{"parent-unique", `ALTER TABLE public.sys_dict DROP CONSTRAINT sys_dict_dict_code_dict_value_key`, `ALTER TABLE public.sys_dict ADD CONSTRAINT sys_dict_dict_code_dict_value_key UNIQUE(dict_code,dict_value)`},
		{"child-fk", `ALTER TABLE public.sys_dict_language DROP CONSTRAINT sys_dict_language_dict_id_fkey; ALTER TABLE public.sys_dict_language ADD CONSTRAINT sys_dict_language_dict_id_fkey FOREIGN KEY(dict_id) REFERENCES public.sys_dict(id) ON DELETE CASCADE NOT VALID`, `ALTER TABLE public.sys_dict_language VALIDATE CONSTRAINT sys_dict_language_dict_id_fkey`},
		{"row-security", `ALTER TABLE public.sys_dict ENABLE ROW LEVEL SECURITY`, `ALTER TABLE public.sys_dict DISABLE ROW LEVEL SECURITY`},
		{"function-search-path", `ALTER FUNCTION public.platformconfig_dictionary_schema_ready() SET search_path = public`, `ALTER FUNCTION public.platformconfig_dictionary_schema_ready() SET search_path = pg_catalog, public`},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, _ := platformConfigMigrationFixture(t)
			if err := ApplyPlatformConfigSchema(db); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(test.change).Error; err != nil {
				t.Fatal(err)
			}
			if err := ApplyPlatformConfigSchema(db); err == nil {
				t.Fatal("accepted schema drift after an existing migration receipt")
			}
			if err := db.Exec(test.restore).Error; err != nil {
				t.Fatal(err)
			}
			if err := ApplyPlatformConfigSchema(db); err != nil {
				t.Fatal("restored original schema: ", err)
			}
		})
	}
	t.Run("verifier-body", func(t *testing.T) {
		db, _ := platformConfigMigrationFixture(t)
		if err := ApplyPlatformConfigSchema(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`CREATE OR REPLACE FUNCTION public.platformconfig_dictionary_schema_ready() RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT true $$`).Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyPlatformConfigSchema(db); err == nil {
			t.Fatal("accepted replaced schema verifier under old checksum")
		}
	})
}

func TestPlatformConfigSchemaPostgresReceiptSuppressionAndInterruption(t *testing.T) {
	t.Run("zero-row receipt rolls back adoption", func(t *testing.T) {
		db, pool := platformConfigMigrationFixture(t)
		if err := db.Exec(`CREATE TABLE public.native_component_migration_ledger(module_name text NOT NULL,file_name text NOT NULL,checksum char(64) NOT NULL,applied_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(module_name,file_name)); CREATE FUNCTION public.tp_dictionary_reject_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$; CREATE TRIGGER tp_dictionary_reject_receipt BEFORE INSERT ON public.native_component_migration_ledger FOR EACH ROW EXECUTE FUNCTION public.tp_dictionary_reject_receipt()`).Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyPlatformConfigSchema(db); err == nil {
			t.Fatal("suppressed adoption receipt was acknowledged")
		}
		var absent bool
		if err := pool.QueryRow(`SELECT to_regprocedure('public.platformconfig_dictionary_schema_ready()') IS NULL`).Scan(&absent); err != nil || !absent {
			t.Fatal("failed receipt did not roll back migration")
		}
		if err := db.Exec(`DROP TRIGGER tp_dictionary_reject_receipt ON public.native_component_migration_ledger`).Error; err != nil {
			t.Fatal(err)
		}
		if err := ApplyPlatformConfigSchema(db); err != nil {
			t.Fatal("retry after confirmed rollback: ", err)
		}
	})
	t.Run("cancelled lock wait preserves original version", func(t *testing.T) {
		db, pool := platformConfigMigrationFixture(t)
		holder, err := pool.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer holder.Rollback()
		if _, err := holder.Exec(`SELECT pg_advisory_xact_lock($1)`, componentMigrationLedgerLock); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		err = ApplyPlatformConfigSchema(db.WithContext(ctx))
		cancel()
		if err == nil {
			t.Fatal("interrupted adoption returned success")
		}
		if err := holder.Rollback(); err != nil {
			t.Fatal(err)
		}
		var absent bool
		if err := pool.QueryRow(`SELECT to_regclass('public.native_component_migration_ledger') IS NULL AND to_regprocedure('public.platformconfig_dictionary_schema_ready()') IS NULL`).Scan(&absent); err != nil || !absent {
			t.Fatal("cancelled waiter changed original version")
		}
		if err := ApplyPlatformConfigSchema(db); err != nil {
			t.Fatal("startup could not adopt after interrupted waiter: ", err)
		}
	})
}
