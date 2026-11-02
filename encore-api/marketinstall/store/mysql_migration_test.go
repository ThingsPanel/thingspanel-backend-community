package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.app/marketinstall/store"
	"encore.app/pkg/ownermigration"
)

func TestMySQLMigrationConcurrentExecutorsUseOneSchemaLock(t *testing.T) {
	f := mysqlStore(t, false)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- f.store.MigrateMySQL(f.ctx) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ownermigration.ErrBusy) {
			t.Fatalf("unexpected migration error: %v", err)
		}
	}
	if success < 1 {
		t.Fatal("no migration executor completed")
	}
	if err := f.store.VerifyMySQLSchema(f.ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM market_install_schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("migration receipt duplication: %d", count)
	}
}

func TestMySQLMigrationConnectionDeathDoesNotSwitchToUnlockedSession(t *testing.T) {
	f := mysqlStore(t, true)
	id := make(chan uint64, 1)
	continueAfterKill := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(continueAfterKill) }) }
	t.Cleanup(release)
	result := make(chan error, 1)
	go func() {
		result <- ownermigration.WithMySQLSchemaLock(f.ctx, f.db, func(conn *sql.Conn, database string) error {
			if database != f.database {
				return errors.New("unexpected fixture database")
			}
			var connection uint64
			if err := conn.QueryRowContext(f.ctx, `SELECT CONNECTION_ID()`).Scan(&connection); err != nil {
				return err
			}
			id <- connection
			select {
			case <-continueAfterKill:
			case <-f.ctx.Done():
				return f.ctx.Err()
			}
			_, err := conn.ExecContext(f.ctx, `SELECT 1`)
			return err
		})
	}()
	var connection uint64
	select {
	case connection = <-id:
	case err := <-result:
		t.Fatalf("fixture failed to acquire its schema lock: %v", err)
	case <-f.ctx.Done():
		t.Fatal("fixture timed out before acquiring its schema lock")
	}
	var held sql.NullInt64
	if err := f.db.QueryRowContext(f.ctx, `SELECT IS_USED_LOCK(?)`, ownermigration.MySQLSchemaLockName(f.database)).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if !held.Valid || uint64(held.Int64) != connection {
		t.Fatal("schema lock not held by the dedicated connection")
	}
	if err := f.store.MigrateMySQL(f.ctx); !errors.Is(err, ownermigration.ErrBusy) {
		t.Fatalf("second owner bypassed schema lock: %v", err)
	}
	// This ID came from the connection holding this generated fixture's exact
	// lock; no other database or shared session is terminated.
	if _, err := f.db.ExecContext(f.ctx, fmt.Sprintf("KILL CONNECTION %d", connection)); err != nil {
		t.Fatal(err)
	}
	release()
	if err := <-result; !errors.Is(err, ownermigration.ErrOutcomeUnknown) {
		t.Fatalf("dead migration connection silently recovered: %v", err)
	}
	if err := f.store.MigrateMySQL(f.ctx); err != nil {
		t.Fatalf("new executor could not reconcile after old connection died: %v", err)
	}
}

func TestMySQLMigrationRejectsWrongChecksumVersionAndSchemaDrift(t *testing.T) {
	for _, which := range []string{"checksum", "future_version", "disabled_check", "missing_unique", "changed_literal", "unowned_tables"} {
		t.Run(which, func(t *testing.T) {
			f := mysqlStore(t, true)
			op := testOperation(t, 1)
			if _, err := f.store.Create(f.ctx, op); err != nil {
				t.Fatal(err)
			}
			var statement string
			var args []any
			switch which {
			case "checksum":
				statement = `UPDATE market_install_schema_migrations SET checksum=? WHERE version='0001_operations.sql'`
				args = []any{strings.Repeat("e", 64)}
			case "future_version":
				statement = `INSERT INTO market_install_schema_migrations(version,checksum,state) VALUES('9999_unknown.sql',?,'applied')`
				args = []any{strings.Repeat("e", 64)}
			case "disabled_check":
				statement = `ALTER TABLE market_install_steps ALTER CHECK market_install_receipt_json NOT ENFORCED`
			case "missing_unique":
				statement = `ALTER TABLE market_install_operations DROP INDEX market_install_tenant_key`
			case "changed_literal":
				statement = `ALTER TABLE market_install_operations DROP CHECK market_install_operation_state, ADD CONSTRAINT market_install_operation_state CHECK(state IN ('queued','running','paused','completed','rejected','QUEUED'))`
			case "unowned_tables":
				statement = `DROP TABLE market_install_schema_migrations`
			}
			if _, err := f.db.ExecContext(f.ctx, statement, args...); err != nil {
				t.Fatal(err)
			}
			if err := f.store.MigrateMySQL(f.ctx); err == nil {
				t.Fatal("migration adopted changed source, weakened schema or unowned tables")
			}
			if err := f.store.VerifyMySQLSchema(f.ctx); err == nil {
				t.Fatal("runtime accepted unverified owner schema")
			}
			if got := f.current(t, op); got.ID != op.ID {
				t.Fatal("migration refusal changed existing data")
			}
		})
	}
}

func TestMySQLMigrationReconcilesOnlyItsExactDirtyResult(t *testing.T) {
	f := mysqlStore(t, true)
	op := testOperation(t, 1)
	if _, err := f.store.Create(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `UPDATE market_install_schema_migrations SET state='applying',applied_at=NULL WHERE version='0001_operations.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := f.store.VerifyMySQLSchema(f.ctx); !errors.Is(err, ownermigration.ErrOutcomeUnknown) {
		t.Fatalf("runtime treated dirty migration as ready: %v", err)
	}
	if err := f.store.MigrateMySQL(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.VerifyMySQLSchema(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.current(t, op); got.ID != op.ID {
		t.Fatal("reconciliation replaced existing operation")
	}
	if _, err := f.db.ExecContext(f.ctx, `UPDATE market_install_schema_migrations SET state='applying',applied_at=NULL WHERE version='0001_operations.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `ALTER TABLE market_install_operations DROP INDEX market_install_tenant_key`); err != nil {
		t.Fatal(err)
	}
	if err := f.store.MigrateMySQL(f.ctx); !errors.Is(err, ownermigration.ErrOutcomeUnknown) {
		t.Fatalf("different dirty schema was adopted: %v", err)
	}
	var state string
	if err := f.db.QueryRowContext(f.ctx, `SELECT state FROM market_install_schema_migrations WHERE version='0001_operations.sql'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "applying" {
		t.Fatal("unverified migration was marked applied")
	}
}

// The helper runs the production owner runner in a separate OS process. The
// parent pauses actual SQL acknowledgement using a short trigger sleep, then
// sends SIGKILL. No production fault hooks or bypass flags are introduced.
func TestMySQLMigrationProcessHelper(t *testing.T) {
	dsn := os.Getenv("TP_A08_MYSQL_CHILD_DSN")
	if dsn == "" {
		t.Skip("process helper only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	ledger, err := store.OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.DB.Close()
	if err := ledger.MigrateMySQL(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLMigrationSIGKILLBeforeDDLAndAfterDDL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this boundary requires Unix SIGKILL; Windows process termination needs separate acceptance")
	}
	for _, phase := range []string{"before_ddl", "after_ddl_before_receipt"} {
		t.Run(phase, func(t *testing.T) {
			f := mysqlStore(t, true)
			// Reset only freshly generated fixture tables. Keep the owner ledger
			// created by the actual runner so a SQL trigger can pause its next run.
			for _, statement := range []string{`DROP TABLE market_install_steps`, `DROP TABLE market_install_operations`, `DELETE FROM market_install_schema_migrations`} {
				if _, err := f.db.ExecContext(f.ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			event := "INSERT"
			if phase == "after_ddl_before_receipt" {
				event = "UPDATE"
			}
			trigger := `CREATE TRIGGER tp_a08_pause BEFORE ` + event + ` ON market_install_schema_migrations FOR EACH ROW BEGIN IF NEW.version='0001_operations.sql' THEN DO SLEEP(2); END IF; END`
			if _, err := f.db.ExecContext(f.ctx, trigger); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestMySQLMigrationProcessHelper$", "-test.v")
			cmd.Env = append(os.Environ(), "TP_A08_MYSQL_CHILD_DSN="+f.dsn)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			})
			deadline := time.Now().Add(5 * time.Second)
			reached := false
			for time.Now().Before(deadline) {
				var sleeping int
				if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID=IS_USED_LOCK(?) AND STATE='User sleep' AND DB=?`, ownermigration.MySQLSchemaLockName(f.database), f.database).Scan(&sleeping); err != nil {
					t.Fatal(err)
				}
				if sleeping == 1 {
					reached = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !reached {
				t.Fatal("fault did not reach the actual migration acknowledgement boundary")
			}
			var present int
			if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME='market_install_operations'`, f.database).Scan(&present); err != nil {
				t.Fatal(err)
			}
			if phase == "before_ddl" && present != 0 || phase == "after_ddl_before_receipt" && present != 1 {
				t.Fatalf("wrong DDL boundary: phase=%s table_present=%d", phase, present)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("killed migration process exited successfully")
			}
			deadline = time.Now().Add(6 * time.Second)
			free := false
			for time.Now().Before(deadline) {
				var available sql.NullInt64
				if err := f.db.QueryRowContext(f.ctx, `SELECT IS_FREE_LOCK(?)`, ownermigration.MySQLSchemaLockName(f.database)).Scan(&available); err != nil {
					t.Fatal(err)
				}
				if available.Valid && available.Int64 == 1 {
					free = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !free {
				t.Fatal("dead process's schema lock is still held; recovery correctly cannot assume ownership")
			}
			if _, err := f.db.ExecContext(f.ctx, `DROP TRIGGER tp_a08_pause`); err != nil {
				t.Fatal(err)
			}
			if err := f.store.MigrateMySQL(f.ctx); err != nil {
				t.Fatalf("original migration could not recover after %s: %v", phase, err)
			}
			if err := f.store.VerifyMySQLSchema(f.ctx); err != nil {
				t.Fatal(err)
			}
			var applied int
			if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM market_install_schema_migrations WHERE state='applied'`).Scan(&applied); err != nil {
				t.Fatal(err)
			}
			if applied != 2 {
				t.Fatalf("wrong recovered owner receipts: %d", applied)
			}
			t.Logf("actual SIGKILL reached %s; same source/schema lock reconciled both owner versions", phase)
		})
	}
}
