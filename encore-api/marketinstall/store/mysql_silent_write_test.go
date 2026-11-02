package store_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"encore.app/marketinstall/app"
)

// Preserve a red run before the read-back fix. With CLIENT_FOUND_ROWS a
// matching UPDATE reports one even when a real trigger restores OLD values.
// A row count alone must not acknowledge an unpersisted target state.
func TestMySQLSilentWritesCannotAcknowledgeUnpersistedState(t *testing.T) {
	for _, which := range []string{"create_metadata", "claim", "confirm_final", "confirm_nonfinal", "hold_paused", "hold_rejected", "resume"} {
		t.Run(which, func(t *testing.T) {
			f := mysqlStore(t, true)
			steps := 1
			if which == "confirm_nonfinal" {
				steps = 2
			}
			op := testOperation(t, steps)
			var work app.Work
			if which != "create_metadata" {
				if which == "claim" {
					if _, err := f.store.Create(f.ctx, op); err != nil {
						t.Fatal(err)
					}
				} else {
					work = f.dispatched(t, op)
				}
			}
			if which == "resume" {
				if err := f.store.Hold(f.ctx, work, app.Paused, "owner_unknown"); err != nil {
					t.Fatal(err)
				}
			}
			trigger := fmt.Sprintf(`CREATE TRIGGER tp_a08_silent BEFORE UPDATE ON market_install_operations FOR EACH ROW BEGIN IF NEW.id='%s' THEN SET NEW.bundle_key=OLD.bundle_key; SET NEW.bundle_version=OLD.bundle_version; SET NEW.state=OLD.state; SET NEW.generation=OLD.generation; SET NEW.lease_until=OLD.lease_until; SET NEW.error_code=OLD.error_code; SET NEW.updated_at=OLD.updated_at; END IF; END`, op.ID)
			if _, err := f.db.ExecContext(f.ctx, trigger); err != nil {
				t.Fatal(err)
			}
			if which == "create_metadata" {
				if _, err := f.store.Create(f.ctx, op); err == nil {
					t.Error("silent metadata suppression reported creation success")
				}
				f.assertNoRows(t)
				return
			}
			if which == "claim" {
				if work, err := f.store.Claim(f.ctx, 60); err == nil || work != nil {
					t.Errorf("silent lease suppression returned unpersisted work: %#v %v", work, err)
				}
				if current := f.current(t, op); current.State != app.Queued || current.Generation != 0 {
					t.Fatal("failed claim changed original operation")
				}
				return
			}
			if which == "resume" {
				if err := f.store.Resume(f.ctx, op.TenantID, op.SubjectID, op.ID); err == nil {
					t.Error("silent resume suppression acknowledged queued state")
				}
				if current := f.current(t, op); current.State != app.Paused {
					t.Fatal("failed resume changed paused operation")
				}
				return
			}
			receipt := app.Receipt{Owner: work.Step.Owner, ResourceID: "existing-owner-resource", PayloadHash: work.PayloadHash, Data: json.RawMessage(`{"existing":true}`)}
			var err error
			if which == "hold_paused" {
				err = f.store.Hold(f.ctx, work, app.Paused, "owner_unknown")
			} else if which == "hold_rejected" {
				err = f.store.Hold(f.ctx, work, app.Rejected, "owner_rejected")
			} else {
				err = f.store.Confirm(f.ctx, work, receipt)
			}
			if err == nil {
				t.Error("silent final operation update reported success")
			}
			if current := f.current(t, op); current.State != app.Running {
				t.Fatal("failed target state changed operation")
			}
			var state string
			var raw []byte
			if err := f.db.QueryRowContext(f.ctx, `SELECT state,receipt FROM market_install_steps WHERE operation_id=? AND ordinal=0`, op.ID).Scan(&state, &raw); err != nil {
				t.Fatal(err)
			}
			if state != "dispatched" || raw != nil {
				t.Fatalf("unpersisted target committed an intermediate step: state=%s receipt=%s", state, raw)
			}
		})
	}
}

func TestMySQLPartlyChangedTargetAndRewrittenReceiptStillRollback(t *testing.T) {
	for _, which := range []string{"operation_state_only", "receipt_changed"} {
		t.Run(which, func(t *testing.T) {
			f := mysqlStore(t, true)
			op := testOperation(t, 1)
			work := f.dispatched(t, op)
			var trigger string
			if which == "operation_state_only" {
				// Timestamp and lease still change, so even changed-row counting is
				// positive. Only actual target-state verification catches this.
				trigger = fmt.Sprintf(`CREATE TRIGGER tp_a08_partial BEFORE UPDATE ON market_install_operations FOR EACH ROW BEGIN IF NEW.id='%s' THEN SET NEW.state=OLD.state; END IF; END`, op.ID)
			} else {
				trigger = fmt.Sprintf(`CREATE TRIGGER tp_a08_receipt BEFORE UPDATE ON market_install_steps FOR EACH ROW BEGIN IF NEW.operation_id='%s' AND NEW.state='confirmed' THEN SET NEW.receipt=JSON_SET(NEW.receipt,'$.resourceId','different-resource'); END IF; END`, op.ID)
			}
			if _, err := f.db.ExecContext(f.ctx, trigger); err != nil {
				t.Fatal(err)
			}
			receipt := app.Receipt{Owner: work.Step.Owner, ResourceID: "original-owner-resource", PayloadHash: work.PayloadHash, Data: json.RawMessage(`{"existing":true}`)}
			if err := f.store.Confirm(f.ctx, work, receipt); err == nil {
				t.Fatal("different persisted target/receipt was acknowledged")
			}
			if current := f.current(t, op); current.State != app.Running {
				t.Fatal("failed target did not roll back operation")
			}
			var state string
			var raw []byte
			if err := f.db.QueryRowContext(f.ctx, `SELECT state,receipt FROM market_install_steps WHERE operation_id=? AND ordinal=0`, op.ID).Scan(&state, &raw); err != nil {
				t.Fatal(err)
			}
			if state != "dispatched" || raw != nil {
				t.Fatalf("failed target/receipt left an intermediate commit: %s %s", state, raw)
			}
		})
	}
}
