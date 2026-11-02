package store_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"encore.app/marketinstall/app"
)

// MySQL does not implement PostgreSQL's RETURN NULL trigger suppression. These
// tests use actual SIGNAL errors in equivalent transaction boundaries; stale
// generation and missing-step zero-row CAS are tested by the shared contract.
func (f relationalFixture) signal(t *testing.T, table, event, id, suffix string) func() {
	t.Helper()
	column := "id"
	if table == "market_install_steps" {
		column = "operation_id"
	} else if table != "market_install_operations" {
		t.Fatal("invalid fault table")
	}
	if event != "INSERT" && event != "UPDATE" {
		t.Fatal("invalid fault event")
	}
	statement := fmt.Sprintf(`CREATE TRIGGER tp_a08_fault BEFORE %s ON %s FOR EACH ROW BEGIN IF NEW.%s='%s'%s THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='A08 isolated actual SQL failure'; END IF; END`, event, table, column, id, suffix)
	if _, err := f.db.ExecContext(f.ctx, statement); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := f.db.ExecContext(f.ctx, `DROP TRIGGER tp_a08_fault`); err != nil {
			t.Fatal(err)
		}
	}
}

func (f relationalFixture) assertNoRows(t *testing.T) {
	t.Helper()
	for _, table := range []string{"market_install_operations", "market_install_steps"} {
		var count int
		if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("failed transaction left %d rows in %s", count, table)
		}
	}
}

func (f relationalFixture) dispatched(t *testing.T, op app.Operation) app.Work {
	t.Helper()
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
	return *work
}

func TestMySQLWriteErrorsRollbackWholeTransaction(t *testing.T) {
	for _, which := range []string{"create_metadata", "create_second_step", "claim", "confirm_nonfinal", "confirm_final", "hold_paused", "hold_rejected"} {
		t.Run(which, func(t *testing.T) {
			f := mysqlStore(t, true)
			steps := 1
			if which == "create_second_step" || which == "confirm_nonfinal" {
				steps = 2
			}
			op := testOperation(t, steps)
			if which == "create_metadata" || which == "create_second_step" {
				table, event, suffix := "market_install_operations", "UPDATE", ""
				if which == "create_second_step" {
					table, event, suffix = "market_install_steps", "INSERT", " AND NEW.ordinal=1"
				}
				drop := f.signal(t, table, event, op.ID, suffix)
				if _, err := f.store.Create(f.ctx, op); err == nil {
					t.Fatal("failed Create reported success")
				}
				f.assertNoRows(t)
				drop()
				if _, err := f.store.Create(f.ctx, op); err != nil {
					t.Fatal(err)
				}
				return
			}
			if which == "claim" {
				if _, err := f.store.Create(f.ctx, op); err != nil {
					t.Fatal(err)
				}
				drop := f.signal(t, "market_install_operations", "UPDATE", op.ID, " AND NEW.state='running'")
				if work, err := f.store.Claim(f.ctx, 60); err == nil || work != nil {
					t.Fatalf("unpersisted claim returned work: %#v %v", work, err)
				}
				if current := f.current(t, op); current.State != app.Queued || current.Generation != 0 {
					t.Fatal("claim error advanced durable lease")
				}
				drop()
				return
			}
			work := f.dispatched(t, op)
			drop := f.signal(t, "market_install_operations", "UPDATE", op.ID, "")
			receipt := app.Receipt{Owner: work.Step.Owner, ResourceID: "owner-resource", PayloadHash: work.PayloadHash, Data: json.RawMessage(`{"durable":true}`)}
			var err error
			if which == "hold_paused" {
				err = f.store.Hold(f.ctx, work, app.Paused, "unknown")
			} else if which == "hold_rejected" {
				err = f.store.Hold(f.ctx, work, app.Rejected, "rejected")
			} else {
				err = f.store.Confirm(f.ctx, work, receipt)
			}
			if err == nil {
				t.Fatal("unpersisted final operation state reported success")
			}
			if current := f.current(t, op); current.State != app.Running || current.Generation != work.Operation.Generation {
				t.Fatal("operation error committed a state transition")
			}
			var state string
			var raw []byte
			if err := f.db.QueryRowContext(f.ctx, `SELECT state,receipt FROM market_install_steps WHERE operation_id=? AND ordinal=0`, op.ID).Scan(&state, &raw); err != nil {
				t.Fatal(err)
			}
			if state != "dispatched" || raw != nil {
				t.Fatalf("operation error committed intermediate step/receipt: %s %s", state, raw)
			}
			drop()
			if which == "hold_paused" {
				err = f.store.Hold(f.ctx, work, app.Paused, "unknown")
			} else if which == "hold_rejected" {
				err = f.store.Hold(f.ctx, work, app.Rejected, "rejected")
			} else {
				err = f.store.Confirm(f.ctx, work, receipt)
			}
			if err != nil {
				t.Fatalf("original work could not resume after removing fault: %v", err)
			}
		})
	}
}

func TestPostgresActualDeferredCommitErrorDoesNotReportCompletion(t *testing.T) {
	f := postgresStore(t)
	op := testOperation(t, 1)
	work := f.claimed(t, op)
	function := fmt.Sprintf(`CREATE FUNCTION tp_a08_fail_commit() RETURNS trigger LANGUAGE plpgsql AS $body$ BEGIN IF NEW.id='%s' AND NEW.state='completed' THEN RAISE EXCEPTION 'A08 actual deferred commit failure'; END IF; RETURN NEW; END $body$`, op.ID)
	if _, err := f.db.ExecContext(f.ctx, function); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `CREATE CONSTRAINT TRIGGER tp_a08_commit AFTER UPDATE ON market_install_operations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION tp_a08_fail_commit()`); err != nil {
		t.Fatal(err)
	}
	receipt := app.Receipt{Owner: work.Step.Owner, ResourceID: "resource", PayloadHash: work.PayloadHash, Data: json.RawMessage(`{}`)}
	if err := f.store.Confirm(f.ctx, work, receipt); err == nil {
		t.Fatal("failed Commit reported completion")
	}
	if current := f.current(t, op); current.State != app.Running {
		t.Fatal("commit failure advanced operation")
	}
	var state string
	var raw []byte
	if err := f.db.QueryRowContext(f.ctx, `SELECT state,receipt FROM market_install_steps WHERE operation_id=$1 AND ordinal=0`, op.ID).Scan(&state, &raw); err != nil {
		t.Fatal(err)
	}
	if state != "dispatched" || raw != nil {
		t.Fatal("commit failure left confirmed step")
	}
	if _, err := f.db.ExecContext(f.ctx, `DROP TRIGGER tp_a08_commit ON market_install_operations`); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Confirm(f.ctx, work, receipt); err != nil {
		t.Fatal(err)
	}
}
