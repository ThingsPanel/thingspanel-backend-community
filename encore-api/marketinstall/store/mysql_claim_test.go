package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"encore.app/marketinstall/app"
	"encore.app/marketinstall/store"
)

func TestMySQLClaimRangeDoesNotLockEveryReadyOperation(t *testing.T) {
	f := mysqlStore(t, true)
	operations := []app.Operation{testOperation(t, 1), testOperation(t, 1)}
	for i, op := range operations {
		if _, err := f.store.Create(f.ctx, op); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.ExecContext(f.ctx, `UPDATE market_install_operations SET created_at=TIMESTAMPADD(SECOND,?,'2000-01-01 00:00:00') WHERE id=?`, i, op.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Observe the actual production query, not a copied test approximation.
	var raw []byte
	if err := f.db.QueryRowContext(f.ctx, `EXPLAIN FORMAT=JSON `+store.MySQLClaimSQLForTest()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	indexed, filesort := false, false
	var inspect func(any)
	inspect = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if node["table_name"] == "market_install_operations" && node["key"] == "market_install_ready" {
				indexed = true
			}
			if node["using_filesort"] == true {
				filesort = true
			}
			for _, child := range node {
				inspect(child)
			}
		case []any:
			for _, child := range node {
				inspect(child)
			}
		}
	}
	inspect(plan)
	if !indexed || filesort {
		t.Fatalf("claim does not use its bounded ordered index: %s", raw)
	}
	t.Logf("actual MySQL claim EXPLAIN: %s", raw)
	// Pause the first production Claim after its locking SELECT, at the actual
	// UPDATE. The second worker must make progress before this SQL sleep ends.
	trigger := `CREATE TRIGGER tp_a08_claim_pause BEFORE UPDATE ON market_install_operations FOR EACH ROW BEGIN IF NEW.id='` + operations[0].ID + `' AND NEW.state='running' THEN DO SLEEP(2); END IF; END`
	if _, err := f.db.ExecContext(f.ctx, trigger); err != nil {
		t.Fatal(err)
	}
	type result struct {
		work *app.Work
		err  error
	}
	first := make(chan result, 1)
	observed := false
	t.Cleanup(func() {
		if !observed {
			select {
			case <-first:
			case <-time.After(5 * time.Second):
				t.Error("first claim did not finish after its bounded SQL pause")
			}
		}
	})
	go func() {
		work, err := f.store.Claim(f.ctx, 60)
		first <- result{work, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	held := false
	for time.Now().Before(deadline) {
		var count int
		if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE DB=? AND STATE='User sleep'`, f.database).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			held = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !held {
		t.Fatal("first production Claim did not reach the held-row boundary")
	}
	secondContext, stop := context.WithTimeout(f.ctx, time.Second)
	defer stop()
	second, err := f.store.Claim(secondContext, 60)
	if err != nil || second == nil || second.Operation.ID != operations[1].ID {
		t.Fatalf("second worker did not claim its unlocked ready row: %#v %v", second, err)
	}
	select {
	case got := <-first:
		observed = true
		if got.err != nil || got.work == nil || got.work.Operation.ID != operations[0].ID {
			t.Fatalf("first worker did not retain its original claim: %#v %v", got.work, got.err)
		}
	case <-f.ctx.Done():
		t.Fatal("first claim did not finish")
	}
}

func TestMySQLClaimReadinessIncludesExpiredLeasesAndExcludesFinalStates(t *testing.T) {
	f := mysqlStore(t, true)
	states := []string{app.Queued, app.Running, app.Running, app.Paused, app.Completed, app.Rejected}
	operations := make([]app.Operation, len(states))
	for i, state := range states {
		op := testOperation(t, 1)
		operations[i] = op
		if _, err := f.store.Create(f.ctx, op); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.ExecContext(f.ctx, `UPDATE market_install_operations SET state=?,created_at='2000-01-01 00:00:00',lease_until=TIMESTAMPADD(SECOND,?,CURRENT_TIMESTAMP(6)) WHERE id=?`, state, i-2, op.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Explicitly keep one running lease live; equality at the clock boundary
	// is expired, and must never accidentally make this third row eligible.
	if _, err := f.db.ExecContext(f.ctx, `UPDATE market_install_operations SET lease_until=TIMESTAMPADD(SECOND,120,CURRENT_TIMESTAMP(6)) WHERE id=?`, operations[2].ID); err != nil {
		t.Fatal(err)
	}
	for _, expected := range operations[:2] {
		work, err := f.store.Claim(f.ctx, 60)
		if err != nil || work == nil || work.Operation.ID != expected.ID {
			t.Fatalf("readiness chose the wrong operation: expected=%s work=%#v error=%v", expected.ID, work, err)
		}
	}
	if work, err := f.store.Claim(f.ctx, 60); err != nil || work != nil {
		t.Fatalf("a live lease or paused/final state was claimed: %#v %v", work, err)
	}
}
