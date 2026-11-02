package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.app/marketinstall/app"
	"encore.app/marketinstall/store"
	"github.com/go-sql-driver/mysql"
)

type relationalFixture struct {
	db       *sql.DB
	store    *store.Store
	ctx      context.Context
	mysql    bool
	dsn      string
	database string
}

func mysqlStore(t *testing.T, migrate bool) relationalFixture {
	t.Helper()
	dsn := os.Getenv("TP_A08_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("TP_A08_MYSQL_TEST_DSN required for actual MySQL owner tests")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid isolated MySQL test configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	owner, err := store.OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var selected, version string
	if err := owner.DB.QueryRowContext(ctx, `SELECT DATABASE(),VERSION()`).Scan(&selected, &version); err != nil {
		owner.DB.Close()
		t.Fatal(err)
	}
	if !strings.HasPrefix(selected, "tp_takeover_") && !strings.HasPrefix(selected, "tp_a08_") {
		owner.DB.Close()
		t.Fatal("refusing MySQL tests outside a dedicated tp_takeover_* or tp_a08_* database")
	}
	database := "tp_a08_mysql_" + fixtureID(t)
	if _, err := owner.DB.ExecContext(ctx, `CREATE DATABASE `+database+` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`); err != nil {
		owner.DB.Close()
		t.Fatal(err)
	}
	var ledger *store.Store
	t.Cleanup(func() {
		if ledger != nil {
			ledger.DB.Close()
		}
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		// Only the random database created by this invocation is removed. The
		// supplied base database, all existing tables and other owners stay intact.
		if _, err := owner.DB.ExecContext(cleanup, `DROP DATABASE `+database); err != nil {
			t.Errorf("remove this generated MySQL fixture: %v", err)
		}
		owner.DB.Close()
	})
	config.DBName = database
	dsn = config.FormatDSN()
	ledger, err = store.OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if migrate {
		if err := ledger.MigrateMySQL(ctx); err != nil {
			t.Fatal(err)
		}
		if err := ledger.VerifyMySQLSchema(ctx); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("real MySQL %s; generated owner database=%s", version, database)
	return relationalFixture{ledger.DB, ledger, ctx, true, dsn, database}
}

func forRelationalStores(t *testing.T, run func(*testing.T, relationalFixture)) {
	t.Helper()
	t.Run("PostgreSQL", func(t *testing.T) {
		f := postgresStore(t)
		run(t, relationalFixture{db: f.db, store: f.store, ctx: f.ctx})
	})
	t.Run("MySQL", func(t *testing.T) { run(t, mysqlStore(t, true)) })
}

func (f relationalFixture) expire(t *testing.T, id string) {
	t.Helper()
	query := `UPDATE market_install_operations SET lease_until=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE id=$1`
	if f.mysql {
		query = `UPDATE market_install_operations SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?`
	}
	if _, err := f.db.ExecContext(f.ctx, query, id); err != nil {
		t.Fatal(err)
	}
}

func (f relationalFixture) current(t *testing.T, op app.Operation) app.Operation {
	t.Helper()
	current, err := f.store.GetByID(f.ctx, op.TenantID, op.SubjectID, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func TestRelationalConcurrentCreateOriginalKeyAndConflict(t *testing.T) {
	forRelationalStores(t, func(t *testing.T, f relationalFixture) {
		op := testOperation(t, 2)
		var wg sync.WaitGroup
		results := make(chan app.Operation, 20)
		failures := make(chan error, 20)
		for i := 0; i < 20; i++ {
			candidate := op
			candidate.ID = fixtureID(t)
			wg.Add(1)
			go func() {
				defer wg.Done()
				created, err := f.store.Create(f.ctx, candidate)
				if err != nil {
					failures <- err
				} else {
					results <- created
				}
			}()
		}
		wg.Wait()
		close(failures)
		close(results)
		for err := range failures {
			t.Errorf("concurrent original-key create failed: %v", err)
		}
		var original string
		count := 0
		for result := range results {
			count++
			if original == "" {
				original = result.ID
			}
			if result.ID != original {
				t.Errorf("one tenant/key produced multiple operation IDs")
			}
		}
		if count != 20 {
			t.Fatalf("only %d of 20 durable original-key results", count)
		}
		var operations, steps int
		if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM market_install_operations`).Scan(&operations); err != nil {
			t.Fatal(err)
		}
		if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM market_install_steps`).Scan(&steps); err != nil {
			t.Fatal(err)
		}
		if operations != 1 || steps != 2 {
			t.Fatalf("duplicate writes: operations=%d steps=%d", operations, steps)
		}
		for _, mutate := range []func(*app.Operation){func(op *app.Operation) { op.RequestDigest = strings.Repeat("c", 64) }, func(op *app.Operation) { op.SubjectID += "-other" }} {
			conflict := op
			conflict.ID = fixtureID(t)
			mutate(&conflict)
			if _, err := f.store.Create(f.ctx, conflict); !errors.Is(err, app.ErrConflict) {
				t.Fatalf("changed original request was not a conflict: %v", err)
			}
		}
		read, err := f.store.Get(f.ctx, op.TenantID, op.SubjectID, op.RequestKey)
		if err != nil || read.ID != original || read.BundleKey != op.BundleKey || read.Version != op.Version {
			t.Fatalf("original-key lookup lost durable metadata: %#v %v", read, err)
		}
	})
}

func TestRelationalOpaqueIdentityAndSeparateInstances(t *testing.T) {
	forRelationalStores(t, func(t *testing.T, f relationalFixture) {
		base := testOperation(t, 1)
		base.RequestKey = "opaque/key + ?#Aa"
		for _, key := range []string{base.RequestKey, strings.ToLower(base.RequestKey), base.RequestKey + " "} {
			op := base
			op.ID = fixtureID(t)
			op.RequestKey = key
			created, err := f.store.Create(f.ctx, op)
			if err != nil || created.ID != op.ID {
				t.Fatalf("opaque key aliased another instance: %#v %v", created, err)
			}
			found, err := f.store.Get(f.ctx, op.TenantID, op.SubjectID, key)
			if err != nil || found.ID != op.ID {
				t.Fatalf("exact key query: %#v %v", found, err)
			}
		}
		for _, identity := range [][2]string{{strings.ToUpper(base.TenantID), base.SubjectID}, {base.TenantID, strings.ToUpper(base.SubjectID)}, {base.TenantID + " ", base.SubjectID}, {base.TenantID, base.SubjectID + " "}} {
			if _, err := f.store.Get(f.ctx, identity[0], identity[1], base.RequestKey); !errors.Is(err, app.ErrNotFound) {
				t.Fatalf("different opaque identity read operation: %v", err)
			}
		}
	})
}

func TestRelationalTwoWorkersClaimDifferentRowsAndFenceExpiredWork(t *testing.T) {
	forRelationalStores(t, func(t *testing.T, f relationalFixture) {
		operations := []app.Operation{testOperation(t, 1), testOperation(t, 1)}
		for _, op := range operations {
			if _, err := f.store.Create(f.ctx, op); err != nil {
				t.Fatal(err)
			}
		}
		works := make(chan *app.Work, 2)
		failures := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				work, err := f.store.Claim(f.ctx, 60)
				if err != nil {
					failures <- err
				} else {
					works <- work
				}
			}()
		}
		wg.Wait()
		close(failures)
		close(works)
		for err := range failures {
			t.Errorf("concurrent claim: %v", err)
		}
		claimed := map[string]*app.Work{}
		for work := range works {
			if work == nil {
				t.Fatal("worker failed to claim an available row")
			}
			if _, exists := claimed[work.Operation.ID]; exists {
				t.Fatal("two workers received one generation")
			}
			claimed[work.Operation.ID] = work
		}
		if len(claimed) != 2 {
			t.Fatalf("claimed %d of two rows", len(claimed))
		}
		if work, err := f.store.Claim(f.ctx, 60); err != nil || work != nil {
			t.Fatalf("live leases were reclaimed: %#v %v", work, err)
		}
		old := claimed[operations[0].ID]
		if err := f.store.Dispatch(f.ctx, *old); err != nil {
			t.Fatal(err)
		}
		// A repeated matched dispatch is valid even if the DB reports no changed
		// values. The MySQL owner connection explicitly requests matched rows.
		if err := f.store.Dispatch(f.ctx, *old); err != nil {
			t.Fatalf("idempotent dispatch rejected: %v", err)
		}
		f.expire(t, old.Operation.ID)
		fresh, err := f.store.Claim(f.ctx, 60)
		if err != nil || fresh == nil {
			t.Fatalf("reclaim: %#v %v", fresh, err)
		}
		if fresh.Operation.ID != old.Operation.ID || fresh.Operation.Generation != old.Operation.Generation+1 || fresh.IdempotencyKey != old.IdempotencyKey {
			t.Fatal("reclaim changed instance or original step key")
		}
		receipt := app.Receipt{Owner: old.Step.Owner, ResourceID: "real-store-fixture", PayloadHash: old.PayloadHash, Data: json.RawMessage(`{}`)}
		if err := f.store.Confirm(f.ctx, *old, receipt); !errors.Is(err, app.ErrLeaseLost) {
			t.Fatalf("old generation confirmed: %v", err)
		}
		foreign := *fresh
		foreign.Operation.TenantID += "-other"
		if err := f.store.Hold(f.ctx, foreign, app.Paused, "fixture"); !errors.Is(err, app.ErrLeaseLost) {
			t.Fatalf("foreign tenant advanced ledger: %v", err)
		}
		missing := *fresh
		missing.Index = 99
		if err := f.store.Dispatch(f.ctx, missing); !errors.Is(err, app.ErrLeaseLost) {
			t.Fatalf("zero matched step reported dispatch: %v", err)
		}
		if err := f.store.Confirm(f.ctx, *fresh, receipt); err != nil {
			t.Fatal(err)
		}
		if got := f.current(t, operations[0]); got.State != app.Completed {
			t.Fatalf("completed receipt missing: %#v", got)
		}
	})
}

func TestRelationalUnknownPauseResumePreservesOriginalStep(t *testing.T) {
	forRelationalStores(t, func(t *testing.T, f relationalFixture) {
		op := testOperation(t, 2)
		if _, err := f.store.Create(f.ctx, op); err != nil {
			t.Fatal(err)
		}
		work, err := f.store.Claim(f.ctx, 60)
		if err != nil || work == nil {
			t.Fatalf("claim: %#v %v", work, err)
		}
		if err := f.store.Dispatch(f.ctx, *work); err != nil {
			t.Fatal(err)
		}
		if err := f.store.Hold(f.ctx, *work, app.Paused, "owner_reply_unknown"); err != nil {
			t.Fatal(err)
		}
		if next, err := f.store.Claim(f.ctx, 60); err != nil || next != nil {
			t.Fatalf("unknown effect was automatically claimed: %#v %v", next, err)
		}
		if err := f.store.Resume(f.ctx, op.TenantID, op.SubjectID+"-other", op.ID); !errors.Is(err, app.ErrLeaseLost) {
			t.Fatalf("foreign resume: %v", err)
		}
		if err := f.store.Resume(f.ctx, op.TenantID, op.SubjectID, op.ID); err != nil {
			t.Fatal(err)
		}
		next, err := f.store.Claim(f.ctx, 60)
		if err != nil || next == nil {
			t.Fatalf("resume claim: %#v %v", next, err)
		}
		if next.Index != work.Index || next.IdempotencyKey != work.IdempotencyKey || next.PayloadHash != work.PayloadHash || next.Operation.Generation != 2 {
			t.Fatal("resume invented a new operation, payload or step key")
		}
		receipt := app.Receipt{Owner: next.Step.Owner, ResourceID: "existing-owner-resource", PayloadHash: next.PayloadHash, Data: json.RawMessage(`{"exact":9007199254740993}`)}
		if err := f.store.Confirm(f.ctx, *next, receipt); err != nil {
			t.Fatal(err)
		}
		last, err := f.store.Claim(f.ctx, 60)
		if err != nil || last == nil || last.Index != 1 || len(last.PriorResults) != 1 {
			t.Fatalf("next step lost receipt: %#v %v", last, err)
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(last.PriorResults[0].Receipt.Data, &value); err != nil || string(value["exact"]) != "9007199254740993" {
			t.Fatal("receipt lost exact integer value")
		}
		if err := f.store.Hold(f.ctx, *last, app.Rejected, "owner_rejected"); err != nil {
			t.Fatal(err)
		}
		if err := f.store.Resume(f.ctx, op.TenantID, op.SubjectID, op.ID); !errors.Is(err, app.ErrLeaseLost) {
			t.Fatalf("rejected effect was resumed: %v", err)
		}
	})
}

func TestRelationalLeaseCannotSurviveAWaitForItsRowLock(t *testing.T) {
	forRelationalStores(t, func(t *testing.T, f relationalFixture) {
		op := testOperation(t, 1)
		if _, err := f.store.Create(f.ctx, op); err != nil {
			t.Fatal(err)
		}
		work, err := f.store.Claim(f.ctx, 60)
		if err != nil || work == nil {
			t.Fatal(err)
		}
		// Give the old worker a short remaining lease, then keep its row locked
		// past expiry. No arbitrary long sleep is used by production code.
		query := `UPDATE market_install_operations SET lease_until=clock_timestamp()+INTERVAL '1 second' WHERE id=$1`
		lock := `SELECT id FROM market_install_operations WHERE id=$1 FOR UPDATE`
		if f.mysql {
			query = `UPDATE market_install_operations SET lease_until=UTC_TIMESTAMP(6)+INTERVAL 1 SECOND WHERE id=?`
			lock = `SELECT id FROM market_install_operations WHERE id=? FOR UPDATE`
		}
		if _, err := f.db.ExecContext(f.ctx, query, op.ID); err != nil {
			t.Fatal(err)
		}
		tx, err := f.db.BeginTx(f.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var id string
		if err := tx.QueryRowContext(f.ctx, lock, op.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() { result <- f.store.Dispatch(f.ctx, *work) }()
		select {
		case err := <-result:
			t.Fatalf("dispatch escaped a held row lock: %v", err)
		case <-time.After(1300 * time.Millisecond):
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, app.ErrLeaseLost) {
			t.Fatalf("expired lease survived its lock wait: %v", err)
		}
	})
}

func TestMySQLPlanPreservesNumberLexemesAndClosedDBIsNotAbsence(t *testing.T) {
	f := mysqlStore(t, true)
	op := testOperation(t, 1)
	op.Plan.Steps[0].Payload = json.RawMessage(`{"integer":9007199254740993,"exponent":1e+20,"fraction":1.2300}`)
	if _, err := f.store.Create(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	work, err := f.store.Claim(f.ctx, 60)
	if err != nil || work == nil {
		t.Fatalf("plan digest changed after MySQL roundtrip: %#v %v", work, err)
	}
	if string(work.Step.Payload) != string(op.Plan.Steps[0].Payload) {
		t.Fatalf("numeric lexemes changed: %s", work.Step.Payload)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Get(f.ctx, op.TenantID, op.SubjectID, op.RequestKey); err == nil || errors.Is(err, app.ErrNotFound) {
		t.Fatalf("database unavailable became absence: %v", err)
	}
}

// This is deliberately a real-PG regression, not a text/JSON fixture. The
// established digest preserves JSON number lexemes while jsonb may normalize
// exponent notation. Preserve any red result before deciding an additive owner
// migration; never rewrite already-executed PG SQL or silently change hashes.
func TestPostgresPlanExponentPreservesDigestAfterDatabaseRoundTrip(t *testing.T) {
	f := postgresStore(t)
	op := testOperation(t, 1)
	op.Plan.Steps[0].Payload = json.RawMessage(`{"integer":9007199254740993,"exponent":1e+20}`)
	expected, err := app.StepHash(op.Plan.Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Create(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	observed := f.current(t, op)
	stored, err := app.StepHash(observed.Plan.Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("original step hash=%s; stored plan hash=%s; stored fixture payload=%s", expected, stored, observed.Plan.Steps[0].Payload)
	work, err := f.store.Claim(f.ctx, 60)
	if err != nil || work == nil {
		t.Fatalf("valid plan cannot be claimed after PostgreSQL JSONB roundtrip: work=%#v error=%v", work, err)
	}
	if work.PayloadHash != expected {
		t.Fatal("claim replaced the original provider digest")
	}
}

func TestMySQLGenerationOverflowCannotWrap(t *testing.T) {
	f := mysqlStore(t, true)
	op := testOperation(t, 1)
	if _, err := f.store.Create(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `UPDATE market_install_operations SET generation=9223372036854775807 WHERE id=?`, op.ID); err != nil {
		t.Fatal(err)
	}
	if work, err := f.store.Claim(f.ctx, 60); err == nil || work != nil {
		t.Fatalf("exhausted generation produced work: %#v %v", work, err)
	}
	if got := f.current(t, op); got.Generation != 9223372036854775807 || got.State != app.Queued {
		t.Fatal("overflow changed durable generation")
	}
}

func TestMySQLSchemaFreshMigrationAndExistingData(t *testing.T) {
	f := mysqlStore(t, true)
	op := testOperation(t, 1)
	if _, err := f.store.Create(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := f.store.MigrateMySQL(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.current(t, op); got.ID != op.ID || got.BundleKey != op.BundleKey {
		t.Fatal("idempotent migration changed an existing operation")
	}
	var applied int
	if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM market_install_schema_migrations WHERE state='applied'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 4 {
		t.Fatal(fmt.Sprintf("unexpected owner version receipts: %d", applied))
	}
}
