package devicemetadata

import (
	"context"
	"errors"
	"strings"
	"testing"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func TestValidateRuntimeReceipt(t *testing.T) {
	good := runtimeReceipt{EventID: "e", TenantID: "t", DeviceID: "d", Generation: 1,
		ContentDigest: strings.Repeat("a", 64), State: runtimeReceiptApplied}
	if err := validateRuntimeReceipt(good); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}
	for name, mutate := range map[string]func(*runtimeReceipt){
		"zero generation":          func(r *runtimeReceipt) { r.Generation = 0 },
		"short digest":             func(r *runtimeReceipt) { r.ContentDigest = "abc" },
		"uppercase digest":         func(r *runtimeReceipt) { r.ContentDigest = strings.Repeat("A", 64) },
		"unknown state":            func(r *runtimeReceipt) { r.State = "done" },
		"failed without code":      func(r *runtimeReceipt) { r.State = runtimeReceiptFailed },
		"applied carrying failure": func(r *runtimeReceipt) { r.FailureCode = "x" },
		"missing tenant":           func(r *runtimeReceipt) { r.TenantID = "" },
	} {
		bad := good
		mutate(&bad)
		if err := validateRuntimeReceipt(bad); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestRuntimeReceiptCompletesOnlyOnApplied(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	deviceID := uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "RCPT-"+deviceID[:8], "receipt device", "{}", true); err != nil {
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

	desired := map[string]any{"administrativeState": "enabled", "z": 1, "a": []string{"x"}}
	type event struct{ id, operationID string }
	newEvent := func(generation int64) (event, string) {
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		operationID, err := createDeviceOperationTx(ctx, tx, tenantID, "tester", "administrative_state", deviceID,
			"test", "req", strings.Repeat("b", 64), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := enqueueDeviceRuntimeTx(ctx, tx, tenantID, deviceID, operationID, "administrative_state", generation, desired); err != nil {
			t.Fatal(err)
		}
		var id, raw string
		if err := tx.QueryRow(ctx, `SELECT id::text,desired::text FROM device_runtime_outbox WHERE tenant_id=$1 AND operation_id=$2`,
			tenantID, operationID).Scan(&id, &raw); err != nil {
			t.Fatal(err)
		}
		digest, err := runtimeDesiredDigest([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return event{id, operationID}, digest
	}
	record := func(r runtimeReceipt) (runtimeReceiptOutcome, error) {
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out, err := recordRuntimeReceiptTx(ctx, tx, r)
		if err != nil {
			tx.Rollback()
			return out, err
		}
		return out, tx.Commit()
	}
	receipt := func(e event, digest, state string) runtimeReceipt {
		return runtimeReceipt{EventID: e.id, TenantID: tenantID, DeviceID: deviceID, Generation: 3,
			ContentDigest: digest, State: state}
	}
	operationState := func(e event) string {
		var s string
		if err := db.QueryRow(ctx, `SELECT state FROM metadata_operations WHERE tenant_id=$1 AND id=$2`, tenantID, e.operationID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	code := func(err error) errs.ErrCode {
		var e *errs.Error
		if errors.As(err, &e) {
			return e.Code
		}
		return errs.Unknown
	}

	// received and stale never complete the operation.
	e, digest := newEvent(3)
	for _, state := range []string{runtimeReceiptReceived, runtimeReceiptStale} {
		if _, err := record(receipt(e, digest, state)); err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		if got := operationState(e); got != "pending" {
			t.Fatalf("%s receipt moved operation to %s", state, got)
		}
	}
	// A digest or generation mismatch, or another tenant, is refused.
	if _, err := record(receipt(e, strings.Repeat("0", 64), runtimeReceiptApplied)); code(err) != errs.FailedPrecondition {
		t.Fatalf("digest mismatch: %v", err)
	}
	wrongGen := receipt(e, digest, runtimeReceiptApplied)
	wrongGen.Generation = 4
	if _, err := record(wrongGen); code(err) != errs.FailedPrecondition {
		t.Fatalf("generation mismatch: %v", err)
	}
	foreign := receipt(e, digest, runtimeReceiptApplied)
	foreign.TenantID = uuid.NewString()
	if _, err := record(foreign); code(err) != errs.NotFound {
		t.Fatalf("foreign tenant: %v", err)
	}
	if got := operationState(e); got != "pending" {
		t.Fatalf("refused receipts changed operation to %s", got)
	}
	// Retryable failure keeps the operation pending; applied then completes it, and replays are no-ops.
	retry := receipt(e, digest, runtimeReceiptFailed)
	retry.FailureCode, retry.Retryable = "RUNTIME_UNAVAILABLE", true
	if out, err := record(retry); err != nil || out.OutboxState != deviceRuntimeOutboxFailed || operationState(e) != "pending" {
		t.Fatalf("retryable failure: %+v %v state=%s", out, err, operationState(e))
	}
	if out, err := record(receipt(e, digest, runtimeReceiptApplied)); err != nil || out.OperationState != "succeeded" {
		t.Fatalf("applied: %+v %v", out, err)
	}
	if out, err := record(receipt(e, digest, runtimeReceiptApplied)); err != nil || !out.Replayed {
		t.Fatalf("replay: %+v %v", out, err)
	}
	if _, err := record(retry); code(err) != errs.FailedPrecondition {
		t.Fatalf("failure after applied should conflict: %v", err)
	}

	// A non-retryable failure ends the operation with a problem document.
	e2, digest2 := newEvent(3)
	fatal := receipt(e2, digest2, runtimeReceiptFailed)
	fatal.FailureCode = "DEVICE_REJECTED"
	if out, err := record(fatal); err != nil || out.OperationState != "failed" {
		t.Fatalf("non-retryable failure: %+v %v", out, err)
	}
	if _, err := record(receipt(e2, digest2, runtimeReceiptApplied)); code(err) != errs.FailedPrecondition {
		t.Fatalf("applied after final failure: %v", err)
	}
}

func TestApplyingADeletionReceiptRemovesTheDeviceButOtherKindsDoNot(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
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
	apply := func(kind string) (deviceID string) {
		deviceID = uuid.NewString()
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "DEL-"+deviceID[:8], "to delete", "{}", true); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		operationID, err := createDeviceOperationTx(ctx, tx, tenantID, "tester", kind, deviceID, "test", "req", strings.Repeat("c", 64), nil)
		if err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		desired := map[string]any{"deviceId": deviceID}
		if err := enqueueDeviceRuntimeTx(ctx, tx, tenantID, deviceID, operationID, kind, 1, desired); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		var eventID, raw string
		if err := tx.QueryRow(ctx, `SELECT id::text,desired::text FROM device_runtime_outbox WHERE tenant_id=$1 AND operation_id=$2`, tenantID, operationID).Scan(&eventID, &raw); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		digest, _ := runtimeDesiredDigest([]byte(raw))
		if _, err := recordRuntimeReceiptTx(ctx, tx, runtimeReceipt{EventID: eventID, TenantID: tenantID, DeviceID: deviceID,
			Generation: 1, ContentDigest: digest, State: runtimeReceiptApplied}); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return deviceID
	}
	alive := func(deviceID string) bool {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, deviceID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	if alive(apply(operationKindDeviceDeletion)) {
		t.Fatal("device still alive after the runtime confirmed its deletion")
	}
	if !alive(apply("administrative_state")) {
		t.Fatal("an unrelated applied receipt removed the device")
	}
}
