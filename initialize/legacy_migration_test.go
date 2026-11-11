package initialize

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	componentsql "project/sql"
)

type legacyMigrationFixture struct {
	db, admin   *gorm.DB
	dsn, schema string
}

func legacyMigrationDSN(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err == nil {
			params := parsed.Query()
			params.Set("search_path", schema)
			params.Set("application_name", schema)
			parsed.RawQuery = params.Encode()
			return parsed.String()
		}
	}
	return dsn + " search_path=" + schema + " application_name=" + schema
}

func newLegacyMigrationFixture(t *testing.T) legacyMigrationFixture {
	t.Helper()
	dsn := os.Getenv("TEST_COMPONENT_MIGRATION_DSN")
	if dsn == "" {
		t.Skip("TEST_COMPONENT_MIGRATION_DSN required")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated migration database")
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminPool.Close() })
	var database string
	if err := admin.Raw("SELECT current_database()").Scan(&database).Error; err != nil || database != "qa_component_migration_test" {
		t.Fatal("refusing non-fixture migration database")
	}
	schema := "legacy_runner_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; err != nil {
			t.Error(err)
		}
	})
	selected := legacyMigrationDSN(dsn, schema)
	db, err := gorm.Open(postgres.Open(selected), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open private migration schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = pool.Close() })
	return legacyMigrationFixture{db: db, admin: admin, dsn: selected, schema: schema}
}

func legacyMigrationFile(number int) ([]byte, error) {
	switch number {
	case 1:
		return []byte("CREATE TABLE migration_probe(step int PRIMARY KEY); INSERT INTO migration_probe(step) VALUES (1)"), nil
	case 2:
		return []byte("INSERT INTO migration_probe(step) VALUES (2)"), nil
	case 3:
		return []byte("INSERT INTO migration_probe(step) VALUES (3)"), nil
	default:
		return nil, fmt.Errorf("unexpected fixture migration %d", number)
	}
}

func requireLegacyVersion(t *testing.T, db *gorm.DB, version, effects int) {
	t.Helper()
	var rows []struct{ VersionNumber int }
	if err := db.Table("sys_version").Select("version_number").Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].VersionNumber != version {
		t.Fatalf("legacy ledger=%+v, want one version %d", rows, version)
	}
	var count int64
	if err := db.Table("migration_probe").Count(&count).Error; err != nil || count != int64(effects) {
		t.Fatalf("migration effects=%d, want %d, err=%v", count, effects, err)
	}
}

type migrationReplyLossPool struct {
	*sql.DB
	rollbackLoss bool
	commits      atomic.Int32
	rollbacks    atomic.Int32
}

func (p *migrationReplyLossPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &migrationReplyLossTx{Tx: tx, pool: p}, nil
}

type migrationReplyLossTx struct {
	*sql.Tx
	pool *migrationReplyLossPool
}

func (tx *migrationReplyLossTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	tx.pool.commits.Add(1)
	return errors.New("injected migration commit reply loss")
}
func (tx *migrationReplyLossTx) Rollback() error {
	if err := tx.Tx.Rollback(); err != nil {
		return err
	}
	tx.pool.rollbacks.Add(1)
	if tx.pool.rollbackLoss {
		return errors.New("injected migration rollback reply loss")
	}
	return nil
}

func migrationReplyLossDB(t *testing.T, db *gorm.DB, rollback bool) (*gorm.DB, *migrationReplyLossPool) {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	loss := &migrationReplyLossPool{DB: pool, rollbackLoss: rollback}
	wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return wrapped, loss
}

func TestLegacySchemaMigrationPostgres(t *testing.T) {
	t.Run("two_startup_connections_apply_once_and_replay_original_version", func(t *testing.T) {
		fixture := newLegacyMigrationFixture(t)
		other, err := gorm.Open(postgres.Open(fixture.dsn), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		otherPool, err := other.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = otherPool.Close() })
		var loads atomic.Int32
		load := func(number int) ([]byte, error) { loads.Add(1); return legacyMigrationFile(number) }
		ready := make(chan struct{})
		results := make(chan error, 2)
		for _, db := range []*gorm.DB{fixture.db, other} {
			go func(db *gorm.DB) { <-ready; results <- applyLegacySchemaMigrations(db, 2, "fixture-2", load) }(db)
		}
		close(ready)
		for i := 0; i < 2; i++ {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		if loads.Load() != 2 {
			t.Fatalf("two startup connections loaded migration files %d times, want 2 total", loads.Load())
		}
		requireLegacyVersion(t, fixture.db, 2, 2)
		if err := applyLegacySchemaMigrations(fixture.db, 2, "fixture-2", func(int) ([]byte, error) {
			t.Error("ledger replay loaded old SQL")
			return nil, errors.New("unexpected replay")
		}); err != nil {
			t.Fatal(err)
		}
		requireLegacyVersion(t, fixture.db, 2, 2)
	})
	t.Run("existing_zero_version_is_not_inserted_again", func(t *testing.T) {
		fixture := newLegacyMigrationFixture(t)
		for i := 0; i < 2; i++ {
			if err := applyLegacySchemaMigrations(fixture.db, 0, "fixture-0", legacyMigrationFile); err != nil {
				t.Fatal(err)
			}
		}
		if err := applyLegacySchemaMigrations(fixture.db, 1, "fixture-1", legacyMigrationFile); err != nil {
			t.Fatal(err)
		}
		requireLegacyVersion(t, fixture.db, 1, 1)
	})
	for _, failure := range []string{"load", "empty", "sql"} {
		t.Run("upgrade_"+failure+"_failure_preserves_prior_version", func(t *testing.T) {
			fixture := newLegacyMigrationFixture(t)
			if err := applyLegacySchemaMigrations(fixture.db, 1, "fixture-1", legacyMigrationFile); err != nil {
				t.Fatal(err)
			}
			load := func(number int) ([]byte, error) {
				if number != 3 {
					return legacyMigrationFile(number)
				}
				switch failure {
				case "load":
					return nil, errors.New("injected read failure")
				case "empty":
					return []byte(" \n"), nil
				default:
					return []byte("INSERT INTO missing_migration_table VALUES (3)"), nil
				}
			}
			if err := applyLegacySchemaMigrations(fixture.db, 3, "fixture-3", load); err == nil {
				t.Fatal("failed migration was reported complete")
			}
			requireLegacyVersion(t, fixture.db, 1, 1)
			if err := applyLegacySchemaMigrations(fixture.db, 3, "fixture-3", legacyMigrationFile); err != nil {
				t.Fatal(err)
			}
			requireLegacyVersion(t, fixture.db, 3, 3)
		})
	}
	for _, event := range []string{"INSERT", "UPDATE"} {
		t.Run("zero_row_version_"+strings.ToLower(event)+"_rolls_back", func(t *testing.T) {
			fixture := newLegacyMigrationFixture(t)
			if err := applyLegacySchemaMigrations(fixture.db, 0, "fixture-0", legacyMigrationFile); err != nil {
				t.Fatal(err)
			}
			if event == "INSERT" {
				if err := fixture.db.Exec("DELETE FROM sys_version").Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := fixture.db.Exec("CREATE FUNCTION suppress_legacy_version() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$; CREATE TRIGGER suppress_legacy_version BEFORE " + event + " ON sys_version FOR EACH ROW EXECUTE FUNCTION suppress_legacy_version()").Error; err != nil {
				t.Fatal(err)
			}
			if err := applyLegacySchemaMigrations(fixture.db, 1, "fixture-1", legacyMigrationFile); err == nil {
				t.Fatal("zero-row version receipt reported success")
			}
			if fixture.db.Migrator().HasTable("migration_probe") {
				t.Fatal("failed version receipt committed migration effects")
			}
			var count int64
			if err := fixture.db.Table("sys_version").Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if event == "INSERT" {
				want = 0
			}
			if count != want {
				t.Fatalf("prior ledger rows=%d, want %d", count, want)
			}
			if err := fixture.db.Exec("DROP TRIGGER suppress_legacy_version ON sys_version; DROP FUNCTION suppress_legacy_version()").Error; err != nil {
				t.Fatal(err)
			}
			if err := applyLegacySchemaMigrations(fixture.db, 1, "fixture-1", legacyMigrationFile); err != nil {
				t.Fatal(err)
			}
			requireLegacyVersion(t, fixture.db, 1, 1)
		})
	}
	t.Run("newer_and_multiple_versions_fail_closed", func(t *testing.T) {
		fixture := newLegacyMigrationFixture(t)
		if err := applyLegacySchemaMigrations(fixture.db, 1, "fixture-1", legacyMigrationFile); err != nil {
			t.Fatal(err)
		}
		if err := applyLegacySchemaMigrations(fixture.db, 0, "older", legacyMigrationFile); err == nil {
			t.Fatal("newer database version accepted by older program")
		}
		requireLegacyVersion(t, fixture.db, 1, 1)
		if err := fixture.db.Exec("INSERT INTO sys_version(version_number,version) VALUES(2,'fixture-corrupt')").Error; err != nil {
			t.Fatal(err)
		}
		if err := applyLegacySchemaMigrations(fixture.db, 3, "fixture-3", legacyMigrationFile); err == nil {
			t.Fatal("multiple current ledger versions were silently adopted")
		}
		var count int64
		if err := fixture.db.Table("sys_version").Count(&count).Error; err != nil || count != 2 {
			t.Fatal("corrupt owner ledger was rewritten", count, err)
		}
	})
	t.Run("ledger_read_error_releases_the_only_connection", func(t *testing.T) {
		fixture := newLegacyMigrationFixture(t)
		pool, err := fixture.db.DB()
		if err != nil {
			t.Fatal(err)
		}
		pool.SetMaxOpenConns(1)
		if err := fixture.db.Exec("CREATE TABLE sys_version(unexpected_column text)").Error; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := applyLegacySchemaMigrations(fixture.db.WithContext(ctx), 1, "fixture-1", legacyMigrationFile); err == nil {
			t.Fatal("unreadable version ledger accepted")
		}
		if err := fixture.db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
			t.Fatalf("failed migration leaked its connection: %v", err)
		}
	})
	t.Run("commit_reply_loss_queries_original_version_before_replay", func(t *testing.T) {
		fixture := newLegacyMigrationFixture(t)
		wrapped, loss := migrationReplyLossDB(t, fixture.db, false)
		if err := applyLegacySchemaMigrations(wrapped, 1, "fixture-1", legacyMigrationFile); !errors.Is(err, ErrMigrationOutcomeUnknown) {
			t.Fatalf("commit reply loss was not unknown: %v", err)
		}
		if loss.commits.Load() != 1 {
			t.Fatal("migration commit was automatically repeated")
		}
		requireLegacyVersion(t, fixture.db, 1, 1)
		if err := applyLegacySchemaMigrations(fixture.db, 1, "fixture-1", func(int) ([]byte, error) {
			t.Error("unknown commit replayed committed SQL")
			return nil, errors.New("unexpected replay")
		}); err != nil {
			t.Fatal(err)
		}
		requireLegacyVersion(t, fixture.db, 1, 1)
	})
	t.Run("rollback_reply_loss_remains_unknown_and_preserves_prior_version", func(t *testing.T) {
		fixture := newLegacyMigrationFixture(t)
		if err := applyLegacySchemaMigrations(fixture.db, 1, "fixture-1", legacyMigrationFile); err != nil {
			t.Fatal(err)
		}
		wrapped, loss := migrationReplyLossDB(t, fixture.db, true)
		load := func(int) ([]byte, error) {
			return []byte("INSERT INTO migration_probe(step) VALUES(2); INSERT INTO missing_migration_table VALUES(3)"), nil
		}
		if err := applyLegacySchemaMigrations(wrapped, 2, "fixture-2", load); !errors.Is(err, ErrMigrationOutcomeUnknown) {
			t.Fatalf("rollback reply loss was not unknown: %v", err)
		}
		if loss.rollbacks.Load() != 1 || loss.commits.Load() != 0 {
			t.Fatal("failed migration was not rolled back once")
		}
		requireLegacyVersion(t, fixture.db, 1, 1)
		if err := applyLegacySchemaMigrations(fixture.db, 2, "fixture-2", legacyMigrationFile); err != nil {
			t.Fatal(err)
		}
		requireLegacyVersion(t, fixture.db, 2, 2)
	})
	t.Run("terminated_database_transaction_recovers_from_prior_version", func(t *testing.T) {
		fixture := newLegacyMigrationFixture(t)
		if err := applyLegacySchemaMigrations(fixture.db, 1, "fixture-1", legacyMigrationFile); err != nil {
			t.Fatal(err)
		}
		entered := make(chan int, 1)
		release := make(chan struct{})
		var releaseOnce, once sync.Once
		unlock := func() { releaseOnce.Do(func() { close(release) }) }
		t.Cleanup(unlock)
		const callbackName = "test:terminate-legacy-migration"
		if err := fixture.db.Callback().Raw().After("gorm:raw").Register(callbackName, func(tx *gorm.DB) {
			if tx.Error != nil || !strings.Contains(tx.Statement.SQL.String(), "INSERT INTO migration_probe(step) VALUES (2)") {
				return
			}
			once.Do(func() {
				var pid int
				if err := tx.Statement.ConnPool.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
					tx.AddError(err)
					entered <- 0
					return
				}
				entered <- pid
				<-release
			})
		}); err != nil {
			t.Fatal(err)
		}
		remove := func() { _ = fixture.db.Callback().Raw().Remove(callbackName) }
		t.Cleanup(remove)
		finished := make(chan error, 1)
		go func() { finished <- applyLegacySchemaMigrations(fixture.db, 2, "fixture-2", legacyMigrationFile) }()
		var pid int
		select {
		case pid = <-entered:
		case err := <-finished:
			t.Fatalf("migration ended before interruption: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("migration did not reach transaction interruption point")
		}
		if pid <= 0 {
			t.Fatal("missing isolated transaction backend ID")
		}
		var terminated bool
		if err := fixture.admin.Raw("SELECT pg_terminate_backend(?)", pid).Scan(&terminated).Error; err != nil || !terminated {
			t.Fatalf("could not terminate this fixture transaction: terminated=%v error=%v", terminated, err)
		}
		unlock()
		select {
		case err := <-finished:
			if err == nil {
				t.Fatal("terminated migration reported completion")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("terminated migration did not return")
		}
		remove()
		requireLegacyVersion(t, fixture.db, 1, 1)
		if err := applyLegacySchemaMigrations(fixture.db, 2, "fixture-2", legacyMigrationFile); err != nil {
			t.Fatal(err)
		}
		requireLegacyVersion(t, fixture.db, 2, 2)
	})
}

func TestComponentMigrationZeroRowPostgres(t *testing.T) {
	fixture := newLegacyMigrationFixture(t)
	module := "fixture-" + uuid.NewString()
	component := componentMigration{module: module, description: "fixture component", lockKey: 0x54455354434f4d50, verify: func(*gorm.DB) error { return nil }}
	if err := applyComponentMigrations(fixture.db, component); err != nil {
		t.Fatal(err)
	}
	trigger := "component_zero_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	function := `"` + fixture.schema + `".suppress_component_receipt`
	if err := fixture.db.Exec("CREATE FUNCTION " + function + "() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.module_name='" + module + "' THEN RETURN NULL; END IF; RETURN NEW; END $$; CREATE TRIGGER " + trigger + " BEFORE INSERT ON " + componentMigrationLedgerTable + " FOR EACH ROW EXECUTE FUNCTION " + function + "()").Error; err != nil {
		t.Fatal(err)
	}
	remove := func() {
		if err := fixture.admin.Exec("DROP TRIGGER IF EXISTS " + trigger + " ON " + componentMigrationLedgerTable).Error; err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() {
		remove()
		_ = fixture.admin.Exec("DELETE FROM "+componentMigrationLedgerTable+" WHERE module_name=?", module).Error
	})
	component.files = []componentsql.NativeBindingMigration{{FileName: "001_fixture.sql", Contents: []byte("CREATE TABLE component_probe(id int PRIMARY KEY); INSERT INTO component_probe VALUES(1)")}}
	if err := applyComponentMigrations(fixture.db, component); err == nil {
		t.Fatal("zero-row component version receipt reported success")
	}
	if fixture.db.Migrator().HasTable("component_probe") {
		t.Fatal("missing component receipt committed schema effects")
	}
	var count int64
	if err := fixture.db.Table(componentMigrationLedgerTable).Where("module_name=?", module).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed component receipt was recorded", count, err)
	}
	remove()
	if err := applyComponentMigrations(fixture.db, component); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Table(componentMigrationLedgerTable).Where("module_name=?", module).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("successful original migration was not recorded once", count, err)
	}
}
