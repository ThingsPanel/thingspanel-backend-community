package store_test

import (
	"encoding/json"
	"testing"

	"encore.app/marketinstall/app"
	"encore.app/marketinstall/store"
)

// Uses the same explicit owner DSN/private-schema fixture as the zero-row tests.
// All operation creation, reads and progress use the actual PostgreSQL store.
func TestPostgresLookupOriginalKeyAfterSubmitReplyIsLost(t *testing.T) {
	f := postgresStore(t)
	operation := testOperation(t, 1)
	operation.RequestKey = "opaque/key + ? # original"
	if _, err := f.store.Create(f.ctx, operation); err != nil {
		t.Fatal(err)
	}
	// Discard the return value and reconstruct the store as another process does.
	restarted := store.New(f.db)
	identity := app.Identity{TenantID: operation.TenantID, SubjectID: operation.SubjectID}
	for attempt := 0; attempt < 3; attempt++ {
		observed, err := app.LookupByKey(f.ctx, restarted, identity, operation.RequestKey)
		if err != nil || observed.State != app.Confirmed || observed.Operation == nil || observed.Operation.ID != operation.ID || observed.Operation.State != app.Queued || observed.Operation.Generation != 0 {
			t.Fatalf("original-key recovery changed progress: %#v %v", observed, err)
		}
	}
	for _, foreign := range []app.Identity{
		{TenantID: operation.TenantID, SubjectID: "another-subject"},
		{TenantID: "another-tenant", SubjectID: operation.SubjectID},
	} {
		observed, err := app.LookupByKey(f.ctx, restarted, foreign, operation.RequestKey)
		if err != nil || observed.State != app.NotObserved || observed.Operation != nil {
			t.Fatalf("foreign identity read original record: %#v %v", observed, err)
		}
	}
	missing, err := app.LookupByKey(f.ctx, restarted, identity, "unseen-original-key")
	if err != nil || missing.State != app.NotObserved || missing.Operation != nil {
		t.Fatalf("missing original key: %#v %v", missing, err)
	}
	var operations, steps int
	if err := f.db.QueryRowContext(f.ctx, `SELECT (SELECT COUNT(*) FROM market_install_operations),(SELECT COUNT(*) FROM market_install_steps)`).Scan(&operations, &steps); err != nil || operations != 1 || steps != 1 {
		t.Fatalf("read created extra operations or steps: operations=%d steps=%d error=%v", operations, steps, err)
	}
}

func TestPostgresLookupReturnsOnlyDurableProgress(t *testing.T) {
	f := postgresStore(t)
	operation := testOperation(t, 1)
	identity := app.Identity{TenantID: operation.TenantID, SubjectID: operation.SubjectID}
	work := f.claimed(t, operation)
	observed, err := app.LookupByKey(f.ctx, f.store, identity, operation.RequestKey)
	if err != nil || observed.Operation == nil || observed.Operation.State != app.Running {
		t.Fatalf("running original operation: %#v %v", observed, err)
	}
	receipt := app.Receipt{Owner: work.Step.Owner, ResourceID: "fixture-resource", PayloadHash: work.PayloadHash, Data: json.RawMessage(`{}`)}
	if err := f.store.Confirm(f.ctx, work, receipt); err != nil {
		t.Fatal(err)
	}
	observed, err = app.LookupByKey(f.ctx, store.New(f.db), identity, operation.RequestKey)
	if err != nil || observed.Operation == nil || observed.Operation.ID != operation.ID || observed.Operation.State != app.Completed {
		t.Fatalf("completed original operation: %#v %v", observed, err)
	}
}

func TestPostgresLookupDatabaseFailureIsNotAbsence(t *testing.T) {
	f := postgresStore(t)
	operation := testOperation(t, 1)
	if _, err := f.store.Create(f.ctx, operation); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	observed, err := app.LookupByKey(f.ctx, f.store, app.Identity{TenantID: operation.TenantID, SubjectID: operation.SubjectID}, operation.RequestKey)
	if err == nil || observed.State != "" || observed.Operation != nil {
		t.Fatalf("database failure became safe-to-retry absence: %#v %v", observed, err)
	}
}
