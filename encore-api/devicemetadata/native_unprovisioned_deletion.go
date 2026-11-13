package devicemetadata

import (
	"context"
	"database/sql"
	"errors"

	"encore.dev/storage/sqldb"
)

// A device that has never had credentials or a runtime registration has no
// external device to isolate. Prove that while holding the same device lock as
// credential creation/registration, and tombstone it in that transaction.
// A missing runtimeRef alone is never sufficient: a failed activation may have
// already reached the runtime owner before its confirmation was persisted.
func completeUnprovisionedDeviceDeletion(ctx context.Context, item runtimeOutboxItem) (bool, error) {
	if item.Kind != operationKindDeviceDeletion {
		return false, nil
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var operationID string
	err = tx.QueryRow(ctx, `SELECT operation_id::text FROM device_runtime_outbox
		WHERE tenant_id=$1 AND id=$2 AND device_id=$3 AND kind=$4 FOR UPDATE`,
		item.TenantID, item.EventID, item.DeviceID, operationKindDeviceDeletion).Scan(&operationID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var operationState string
	if err := tx.QueryRow(ctx, `SELECT state FROM metadata_operations WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
		item.TenantID, operationID).Scan(&operationState); err != nil {
		return false, err
	}
	if operationState != "pending" && operationState != "running" {
		return false, nil
	}
	var namespace, externalID sql.NullString
	var deletedAt sql.NullTime
	err = tx.QueryRow(ctx, `SELECT b.runtime_namespace,b.runtime_external_id,d.deleted_at
		FROM devices d JOIN device_access_bindings b ON b.tenant_id=d.tenant_id AND b.device_id=d.id
		WHERE d.tenant_id=$1 AND d.id=$2 FOR UPDATE OF d,b`, item.TenantID, item.DeviceID).
		Scan(&namespace, &externalID, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if namespace.Valid || externalID.Valid || deletedAt.Valid {
		return false, nil
	}
	var possibleRuntime bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM device_credentials WHERE tenant_id=$1 AND device_id=$2)
		OR EXISTS (SELECT 1 FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2 AND kind=$3)`,
		item.TenantID, item.DeviceID, operationKindCredentialChange).Scan(&possibleRuntime); err != nil {
		return false, err
	}
	if possibleRuntime {
		return false, nil
	}
	digest, err := runtimeDesiredDigest(item.Desired)
	if err != nil {
		return false, err
	}
	_, err = recordRuntimeReceiptTx(ctx, tx, runtimeReceipt{EventID: item.EventID, TenantID: item.TenantID,
		DeviceID: item.DeviceID, Generation: item.Generation, ContentDigest: digest,
		State: runtimeReceiptApplied, RequestID: "unprovisioned-device-cleanup"})
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
