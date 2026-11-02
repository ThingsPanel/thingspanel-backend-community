package devicemetadata

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type scriptedApplier struct {
	byKind map[string]runtimeApplyOutcome
	seen   []string
}

func (s *scriptedApplier) Apply(_ context.Context, item runtimeOutboxItem) runtimeApplyOutcome {
	s.seen = append(s.seen, item.Kind)
	return s.byKind[item.Kind]
}

func TestDispatchRuntimeOutboxRecordsOnlyWhatTheApplierReports(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	deviceID := uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "DISP-"+deviceID[:8], "dispatch device", "{}", true); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM device_runtime_outbox WHERE tenant_id=$1`,
			`DELETE FROM metadata_operations WHERE tenant_id=$1`,
			`DELETE FROM device_access_bindings WHERE tenant_id=$1`,
			`DELETE FROM devices WHERE tenant_id=$1`,
		} {
			db.Exec(ctx, q, tenantID)
		}
	})
	enqueue := func(kind string) string {
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		operationID, err := createDeviceOperationTx(ctx, tx, tenantID, "tester", kind, deviceID, "test", "req", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := enqueueDeviceRuntimeTx(ctx, tx, tenantID, deviceID, operationID, kind, 2, map[string]any{"deviceId": deviceID}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return operationID
	}
	opState := func(id string) string {
		var s string
		if err := db.QueryRow(ctx, `SELECT state FROM metadata_operations WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	applied := enqueue(operationKindDeviceRenumber)
	rejected := enqueue(operationKindDeviceDeletion)
	unavailable := enqueue(operationKindGatewayBinding)
	skipped := enqueue(operationKindAdministrativeState)
	applier := &scriptedApplier{byKind: map[string]runtimeApplyOutcome{
		operationKindDeviceRenumber:      {Applied: true},
		operationKindDeviceDeletion:      {FailureCode: "RUNTIME_REJECTED"},
		operationKindGatewayBinding:      {FailureCode: "RUNTIME_UNAVAILABLE", Retryable: true},
		operationKindAdministrativeState: {Skip: true},
	}}

	summary, err := dispatchRuntimeOutbox(ctx, applier, 50)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Applied != 1 || summary.Failed != 2 || summary.Skipped != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	for id, want := range map[string]string{applied: "succeeded", rejected: "failed", unavailable: "pending", skipped: "pending"} {
		if got := opState(id); got != want {
			t.Errorf("operation %s state=%s want %s", id, got, want)
		}
	}

	// Only the retryable failure and the skipped row are picked up again; the
	// finished and permanently failed operations are not.
	applier.seen = nil
	applier.byKind[operationKindGatewayBinding] = runtimeApplyOutcome{Applied: true}
	if _, err := dispatchRuntimeOutbox(ctx, applier, 50); err != nil {
		t.Fatal(err)
	}
	if len(applier.seen) != 2 {
		t.Fatalf("second pass saw %v", applier.seen)
	}
	if got := opState(unavailable); got != "succeeded" {
		t.Fatalf("retried operation state=%s", got)
	}
	if got := opState(skipped); got != "pending" {
		t.Fatalf("skipped operation state=%s", got)
	}
}
