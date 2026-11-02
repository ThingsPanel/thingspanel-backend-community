package store_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"encore.app/marketinstall/app"
	"encore.app/marketinstall/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// These tests use the real store and PostgreSQL triggers, not an app.Store
// wrapper that merely returns an error. The explicitly supplied owner channel
// is confined to one generated schema in a dedicated fixture database. The
// production runtime role does not need CREATE FUNCTION/TRIGGER privileges.
type postgresFixture struct {
	db    *sql.DB
	store *store.Store
	ctx   context.Context
}

func fixtureID(t *testing.T) string {
	t.Helper()
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(value[:])
}

func postgresStore(t *testing.T) postgresFixture {
	t.Helper()
	dsn := os.Getenv("TP_A08_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("TP_A08_STORE_TEST_DSN is required for real PostgreSQL zero-row fault injection")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid fixture PostgreSQL configuration")
	}
	owner := stdlib.OpenDB(*config)
	owner.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	var database, version string
	if err := owner.QueryRowContext(ctx, `SELECT current_database(),current_setting('server_version')`).Scan(&database, &version); err != nil {
		owner.Close()
		t.Fatal("fixture PostgreSQL is unavailable")
	}
	if !strings.HasPrefix(database, "tp_takeover_") && !strings.HasPrefix(database, "tp_a08_") {
		owner.Close()
		t.Fatal("refusing trigger tests outside a dedicated tp_takeover_* or tp_a08_* database")
	}
	schema := "tp_a08_zero_" + fixtureID(t)
	if _, err := owner.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		owner.Close()
		t.Fatalf("fixture owner cannot create its isolated schema: %v", err)
	}
	var ledger *sql.DB
	t.Cleanup(func() {
		if ledger != nil {
			ledger.Close()
		}
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := owner.ExecContext(cleanup, `DROP SCHEMA "`+schema+`" CASCADE`); err != nil {
			t.Errorf("could not remove this test's generated schema: %v", err)
		}
		owner.Close()
	})
	selected, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid fixture PostgreSQL configuration")
	}
	if selected.RuntimeParams == nil {
		selected.RuntimeParams = make(map[string]string)
	}
	selected.RuntimeParams["search_path"] = schema
	ledger = stdlib.OpenDB(*selected)
	ledger.SetMaxOpenConns(4)
	for _, name := range []string{"0001_installations.up.sql", "0002_request_metadata.up.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ledger.ExecContext(ctx, string(migration)); err != nil {
			t.Fatalf("apply original migration %s in fixture schema: %v", name, err)
		}
	}
	t.Logf("real PostgreSQL %s; fixture database=%s schema=%s", version, database, schema)
	return postgresFixture{db: ledger, store: store.New(ledger), ctx: ctx}
}

func testOperation(t *testing.T, steps int) app.Operation {
	t.Helper()
	id := fixtureID(t)
	plan := app.Plan{ArtifactDigest: "sha256:" + strings.Repeat("a", 64)}
	for index := 0; index < steps; index++ {
		plan.Steps = append(plan.Steps, app.Step{
			ID: fmt.Sprintf("create:%d", index), Owner: "thingmodel", Action: "create",
			ResourceKey: fmt.Sprintf("resource:%d", index), Payload: json.RawMessage(`{"fixture":true}`),
		})
	}
	return app.Operation{
		ID: id, BundleKey: "fixture-bundle", Version: "fixture-version", TenantID: "tenant-" + id,
		SubjectID: "subject-" + id, AuthorizationRef: "fixture-reference", RequestKey: "request-" + id,
		RequestDigest: strings.Repeat("b", 64), State: app.Queued, ArtifactDigest: plan.ArtifactDigest, Plan: plan,
	}
}

func (f postgresFixture) suppress(t *testing.T, table, event, id, suffix string) func() {
	t.Helper()
	column := "id"
	if table == "market_install_steps" {
		column = "operation_id"
	} else if table != "market_install_operations" {
		t.Fatal("invalid fixture table")
	}
	if event != "INSERT" && event != "UPDATE" {
		t.Fatal("invalid fixture trigger event")
	}
	// id is generated hex; all other SQL fragments below are test constants.
	function := fmt.Sprintf(`CREATE FUNCTION tp_a08_suppress_write() RETURNS trigger LANGUAGE plpgsql AS $body$
BEGIN
 IF NEW.%s = '%s'%s THEN RETURN NULL; END IF;
 RETURN NEW;
END
$body$`, column, id, suffix)
	if _, err := f.db.ExecContext(f.ctx, function); err != nil {
		t.Fatalf("fixture owner could not install real zero-row trigger function: %v", err)
	}
	if _, err := f.db.ExecContext(f.ctx, `CREATE TRIGGER tp_a08_zero BEFORE `+event+` ON `+table+` FOR EACH ROW EXECUTE FUNCTION tp_a08_suppress_write()`); err != nil {
		t.Fatalf("install real zero-row trigger: %v", err)
	}
	return func() {
		if _, err := f.db.ExecContext(f.ctx, `DROP TRIGGER tp_a08_zero ON `+table); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.ExecContext(f.ctx, `DROP FUNCTION tp_a08_suppress_write()`); err != nil {
			t.Fatal(err)
		}
	}
}

func (f postgresFixture) requireAbsent(t *testing.T, operation app.Operation) {
	t.Helper()
	var operations, steps int
	if err := f.db.QueryRowContext(f.ctx, `SELECT
 (SELECT COUNT(*) FROM market_install_operations WHERE id=$1),
 (SELECT COUNT(*) FROM market_install_steps WHERE operation_id=$1)`, operation.ID).Scan(&operations, &steps); err != nil {
		t.Fatal(err)
	}
	if operations != 0 || steps != 0 {
		t.Fatalf("failed Create committed partial state: operations=%d steps=%d", operations, steps)
	}
}

func (f postgresFixture) current(t *testing.T, operation app.Operation) app.Operation {
	t.Helper()
	current, err := f.store.GetByID(f.ctx, operation.TenantID, operation.SubjectID, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func (f postgresFixture) claimed(t *testing.T, operation app.Operation) app.Work {
	t.Helper()
	if _, err := f.store.Create(f.ctx, operation); err != nil {
		t.Fatal(err)
	}
	work, err := f.store.Claim(f.ctx, 60)
	if err != nil || work == nil {
		t.Fatalf("claim fixture work: %v", err)
	}
	if work.Operation.ID != operation.ID {
		t.Fatal("claimed an unrelated operation")
	}
	if err := f.store.Dispatch(f.ctx, *work); err != nil {
		t.Fatal(err)
	}
	return *work
}

func (f postgresFixture) requireUnchangedWork(t *testing.T, work app.Work) {
	t.Helper()
	current := f.current(t, work.Operation)
	if current.State != app.Running || current.Generation != work.Operation.Generation || current.ErrorCode != "" {
		t.Fatalf("failed final transition changed operation: state=%s generation=%d code=%s", current.State, current.Generation, current.ErrorCode)
	}
	var stepState string
	var receipt []byte
	var lease sql.NullTime
	if err := f.db.QueryRowContext(f.ctx, `SELECT state,receipt FROM market_install_steps WHERE operation_id=$1 AND ordinal=$2`, work.Operation.ID, work.Index).Scan(&stepState, &receipt); err != nil {
		t.Fatal(err)
	}
	if stepState != "dispatched" || receipt != nil {
		t.Fatalf("failed final transition committed its step: state=%s receiptPresent=%v", stepState, receipt != nil)
	}
	if err := f.db.QueryRowContext(f.ctx, `SELECT lease_until FROM market_install_operations WHERE id=$1`, work.Operation.ID).Scan(&lease); err != nil || !lease.Valid {
		t.Fatalf("failed transition lost its existing lease: %v", err)
	}
}

func TestPostgresZeroRowsCreateRollsBackWholeOperation(t *testing.T) {
	for _, boundary := range []string{"metadata-update", "second-step-insert"} {
		t.Run(boundary, func(t *testing.T) {
			f := postgresStore(t)
			operation := testOperation(t, 2)
			table, event, suffix := "market_install_operations", "UPDATE", ""
			if boundary == "second-step-insert" {
				table, event, suffix = "market_install_steps", "INSERT", " AND NEW.ordinal=1"
			}
			restore := f.suppress(t, table, event, operation.ID, suffix)
			if _, err := f.store.Create(f.ctx, operation); !errors.Is(err, app.ErrLeaseLost) {
				t.Fatalf("zero-row %s reported successful Create: %v", boundary, err)
			}
			f.requireAbsent(t, operation)
			restore()
			if _, err := f.store.Create(f.ctx, operation); err != nil {
				t.Fatalf("same original operation cannot be retried after rollback: %v", err)
			}
			current := f.current(t, operation)
			if current.BundleKey != operation.BundleKey || current.Version != operation.Version {
				t.Fatal("request metadata did not persist on successful retry")
			}
			var steps int
			if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM market_install_steps WHERE operation_id=$1`, operation.ID).Scan(&steps); err != nil || steps != 2 {
				t.Fatalf("successful retry did not retain the full original plan: count=%d error=%v", steps, err)
			}
		})
	}
}

func TestPostgresZeroRowsClaimDoesNotReturnUnpersistedLease(t *testing.T) {
	f := postgresStore(t)
	operation := testOperation(t, 1)
	if _, err := f.store.Create(f.ctx, operation); err != nil {
		t.Fatal(err)
	}
	restore := f.suppress(t, "market_install_operations", "UPDATE", operation.ID, "")
	if work, err := f.store.Claim(f.ctx, 60); work != nil || !errors.Is(err, app.ErrLeaseLost) {
		t.Fatalf("claim returned an unpersisted generation: workPresent=%v error=%v", work != nil, err)
	}
	current := f.current(t, operation)
	if current.State != app.Queued || current.Generation != 0 {
		t.Fatalf("failed claim changed state: %s generation=%d", current.State, current.Generation)
	}
	var lease sql.NullTime
	if err := f.db.QueryRowContext(f.ctx, `SELECT lease_until FROM market_install_operations WHERE id=$1`, operation.ID).Scan(&lease); err != nil || lease.Valid {
		t.Fatalf("failed claim persisted a lease: %v", err)
	}
	restore()
	if work, err := f.store.Claim(f.ctx, 60); err != nil || work == nil || work.Operation.Generation != 1 {
		t.Fatalf("claim after fault removal did not preserve its first generation: %v", err)
	}
}

func TestPostgresZeroRowsConfirmRollsBackReceiptAndStep(t *testing.T) {
	for _, steps := range []int{1, 2} {
		t.Run(fmt.Sprintf("plan-steps-%d", steps), func(t *testing.T) {
			f := postgresStore(t)
			operation := testOperation(t, steps)
			work := f.claimed(t, operation)
			restore := f.suppress(t, "market_install_operations", "UPDATE", operation.ID, "")
			receipt := app.Receipt{Owner: work.Step.Owner, ResourceID: "fixture-resource", PayloadHash: work.PayloadHash, Data: json.RawMessage(`{}`)}
			if err := f.store.Confirm(f.ctx, work, receipt); !errors.Is(err, app.ErrLeaseLost) {
				t.Fatalf("zero-row final operation update reported confirmation: %v", err)
			}
			f.requireUnchangedWork(t, work)
			restore()
			if err := f.store.Confirm(f.ctx, work, receipt); err != nil {
				t.Fatalf("original receipt could not be confirmed after rollback: %v", err)
			}
			wanted := app.Completed
			if steps > 1 {
				wanted = app.Queued
			}
			if current := f.current(t, operation); current.State != wanted {
				t.Fatalf("successful final transition: got=%s want=%s", current.State, wanted)
			}
		})
	}
}

func TestPostgresZeroRowsHoldRollsBackIntermediateState(t *testing.T) {
	for _, target := range []string{app.Paused, app.Rejected} {
		t.Run(target, func(t *testing.T) {
			f := postgresStore(t)
			operation := testOperation(t, 1)
			work := f.claimed(t, operation)
			restore := f.suppress(t, "market_install_operations", "UPDATE", operation.ID, "")
			if err := f.store.Hold(f.ctx, work, target, "FIXTURE_OWNER_RESULT"); !errors.Is(err, app.ErrLeaseLost) {
				t.Fatalf("zero-row final operation update reported hold success: %v", err)
			}
			f.requireUnchangedWork(t, work)
			restore()
			if err := f.store.Hold(f.ctx, work, target, "FIXTURE_OWNER_RESULT"); err != nil {
				t.Fatalf("hold after rollback: %v", err)
			}
			current := f.current(t, operation)
			if current.State != target || current.ErrorCode != "FIXTURE_OWNER_RESULT" {
				t.Fatalf("successful hold state=%s code=%s", current.State, current.ErrorCode)
			}
		})
	}
}
