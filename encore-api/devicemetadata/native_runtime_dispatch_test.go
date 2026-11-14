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
	if summary.Applied != 1 || summary.Failed != 2 || summary.Skipped != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	for id, want := range map[string]string{applied: "succeeded", rejected: "failed", unavailable: "pending", skipped: "pending"} {
		if got := opState(id); got != want {
			t.Errorf("operation %s state=%s want %s", id, got, want)
		}
	}

	// Only the retryable failure is picked up again; unsupported, finished and
	// permanently failed operations do not consume the batch.
	applier.seen = nil
	applier.byKind[operationKindGatewayBinding] = runtimeApplyOutcome{Applied: true}
	if _, err := dispatchRuntimeOutbox(ctx, applier, 50); err != nil {
		t.Fatal(err)
	}
	if len(applier.seen) != 1 {
		t.Fatalf("second pass saw %v", applier.seen)
	}
	if got := opState(unavailable); got != "succeeded" {
		t.Fatalf("retried operation state=%s", got)
	}
	if got := opState(skipped); got != "pending" {
		t.Fatalf("skipped operation state=%s", got)
	}
}

// Real PostgreSQL regression: a saturated prefix must not indefinitely hide a
// newer actionable event, whether the prefix is unsupported or retrying.
func TestDispatchRuntimeOutboxSaturatedPrefix(t *testing.T) {
	requireDeviceGroupPostgres(t)
	for _, unsupported := range []bool{true, false} {
		name := "retryable_failures"
		if unsupported {
			name = "unsupported"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			tenantID, deviceID := testTenantID(), uuid.NewString()
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
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "SAT-"+deviceID[:8], "saturated dispatch", "{}", true); err != nil {
				t.Fatal(err)
			}
			var target string
			for i := 0; i < 51; i++ {
				kind := operationKindGatewayBinding
				if unsupported {
					kind = operationKindAdministrativeState
				}
				if i == 50 {
					kind = operationKindDeviceRenumber
				}
				oid, err := createDeviceOperationTx(ctx, tx, tenantID, "tester", kind, deviceID, "test", "req", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := enqueueDeviceRuntimeTx(ctx, tx, tenantID, deviceID, oid, kind, 2, map[string]any{"deviceId": deviceID}); err != nil {
					t.Fatal(err)
				}
				// A deterministic older prefix; updated_at's existing trigger advances
				// each failure to now when the dispatcher records its retry receipt.
				if _, err := tx.Exec(ctx, `UPDATE device_runtime_outbox SET created_at=NOW()-($3::int * INTERVAL '1 second') WHERE tenant_id=$1 AND operation_id=$2`, tenantID, oid, 100-i); err != nil {
					t.Fatal(err)
				}
				if i == 50 {
					target = oid
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			applier := &scriptedApplier{byKind: map[string]runtimeApplyOutcome{
				operationKindAdministrativeState: {Skip: true},
				operationKindGatewayBinding:      {FailureCode: "RUNTIME_UNAVAILABLE", Retryable: true},
				operationKindDeviceRenumber:      {Applied: true},
			}}
			first, err := dispatchRuntimeOutbox(ctx, applier, 50)
			if err != nil {
				t.Fatal(err)
			}
			if unsupported {
				if first.Applied != 1 || first.Skipped != 0 || len(applier.seen) != 1 {
					t.Fatalf("unsupported prefix consumed batch: %+v seen=%v", first, applier.seen)
				}
				var pending int
				if err := db.QueryRow(ctx, `SELECT count(*) FROM device_runtime_outbox WHERE tenant_id=$1 AND kind=$2 AND state='pending' AND attempts=0`, tenantID, operationKindAdministrativeState).Scan(&pending); err != nil {
					t.Fatal(err)
				}
				if pending != 50 {
					t.Fatalf("unsupported intents mutated: %d", pending)
				}
			} else {
				if first.Failed != 50 || first.Applied != 0 {
					t.Fatalf("first batch=%+v", first)
				}
				applier.seen = nil
				second, err := dispatchRuntimeOutbox(ctx, applier, 50)
				if err != nil {
					t.Fatal(err)
				}
				if second.Applied != 1 || len(applier.seen) != 50 || applier.seen[0] != operationKindDeviceRenumber {
					t.Fatalf("retry prefix starved new work: %+v seen=%v", second, applier.seen)
				}
				// Retry work remains eligible; it is not discarded to make room.
				applier.byKind[operationKindGatewayBinding] = runtimeApplyOutcome{Applied: true}
				third, err := dispatchRuntimeOutbox(ctx, applier, 50)
				if err != nil || third.Applied != 50 {
					t.Fatalf("retries lost: %+v err=%v", third, err)
				}
			}
			var state string
			if err := db.QueryRow(ctx, `SELECT state FROM metadata_operations WHERE tenant_id=$1 AND id=$2`, tenantID, target).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != "succeeded" {
				t.Fatalf("target state=%s", state)
			}
		})
	}
}
