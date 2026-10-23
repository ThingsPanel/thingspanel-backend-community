package notification

import (
	"context"
	"database/sql"
	"encoding/hex"
	"strings"
)

type idempotencyRecord struct {
	Digest          string
	ResourceType    string
	ResourceID      string
	ResourceVersion int64
}

func validIdempotencyInput(tenantID, operation, key, digest string) bool {
	if tenantID == "" || operation == "" || len(key) < 8 || len(key) > 128 || len(digest) != 64 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil && strings.TrimSpace(key) == key
}

func findIdempotencyTx(ctx context.Context, tx *sql.Tx, tenantID, operation, key string) (*idempotencyRecord, error) {
	record := new(idempotencyRecord)
	err := tx.QueryRowContext(ctx, `
		SELECT request_digest, resource_type, resource_id, resource_version
		FROM notification_idempotency
		WHERE tenant_id = $1 AND operation = $2 AND idempotency_key = $3
	`, tenantID, operation, key).Scan(&record.Digest, &record.ResourceType, &record.ResourceID, &record.ResourceVersion)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, classifyStoreError(err)
	}
	return record, nil
}

func beginIdempotencyTx(ctx context.Context, tx *sql.Tx, tenantID, operation, key, digest, resourceType, resourceID string, version int64) (*idempotencyRecord, bool, error) {
	result, err := tx.ExecContext(ctx, `
		INSERT INTO notification_idempotency
			(tenant_id, operation, idempotency_key, request_digest, resource_type, resource_id, resource_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id, operation, idempotency_key) DO NOTHING
	`, tenantID, operation, key, digest, resourceType, resourceID, version)
	if err != nil {
		return nil, false, classifyStoreError(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, storeErr(StoreErrorUnavailable)
	}
	if rows == 1 {
		return nil, false, nil
	}
	record, err := findIdempotencyTx(ctx, tx, tenantID, operation, key)
	if err != nil {
		return nil, false, err
	}
	if record == nil {
		return nil, false, storeErr(StoreErrorUnavailable)
	}
	if record.Digest != digest {
		return nil, false, storeErr(StoreErrorConflict)
	}
	return record, true, nil
}
