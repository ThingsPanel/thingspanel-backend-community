package fileassets

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

// UploadIntent records a committed intent before an object upload begins. It
// is a recovery marker, not a distributed transaction or an asset record.
type UploadIntent struct {
	ID        string
	TenantID  string
	ObjectKey string
}

// CreateUploadIntent commits an intent in its own short transaction before the
// caller uploads the object. The intent ID is the future asset ID.
func CreateUploadIntent(ctx context.Context, tenantID, id, objectKey string) error {
	normalizedTenantID, err := canonicalUUID(tenantID)
	if err != nil || normalizedTenantID != tenantID {
		return invalidAssetInput("tenantId", "tenant ID must be a canonical non-nil UUID")
	}
	normalizedID, err := canonicalUUID(id)
	if err != nil || normalizedID != id {
		return invalidAssetInput("id", "asset ID must be a canonical non-nil UUID")
	}
	if !utf8.ValidString(objectKey) || strings.TrimSpace(objectKey) == "" || utf8.RuneCountInString(objectKey) > 1024 {
		return invalidAssetInput("objectKey", "object key must be non-empty and at most 1024 characters")
	}
	tx, err := BeginTenantScopedTx(ctx, normalizedTenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `
		INSERT INTO file_asset_upload_intents (id, tenant_id, object_key)
		VALUES ($1::uuid, $2::uuid, $3)`, normalizedID, normalizedTenantID, objectKey); err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset upload intent could not be stored")
	}
	if err := tx.Commit(); err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset upload intent could not be committed")
	}
	return nil
}

// LockUploadIntentTx locks an intent within the trusted tenant. Upload/recovery
// callers must hold this lock before changing an intent state.
func LockUploadIntentTx(ctx context.Context, tx *sqldb.Tx, tenantID, id string) error {
	normalizedTenantID, normalizedID, err := validateTenantAssetTxInput(tx, tenantID, id)
	if err != nil {
		return err
	}
	var lockedID string
	err = tx.QueryRow(ctx, `
		SELECT id::text FROM file_asset_upload_intents
		WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, normalizedTenantID, normalizedID).Scan(&lockedID)
	if errors.Is(err, sqldb.ErrNoRows) {
		return &errs.Error{Code: errs.NotFound, Message: "file asset upload intent not found"}
	}
	if err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset upload intent could not be locked")
	}
	return nil
}

// MarkUploadIntentTx changes a locked intent to a terminal or recovery state.
// Database constraints enforce the allowed forward-only transition graph.
func MarkUploadIntentTx(ctx context.Context, tx *sqldb.Tx, tenantID, id, state string) error {
	normalizedTenantID, normalizedID, err := validateTenantAssetTxInput(tx, tenantID, id)
	if err != nil {
		return err
	}
	if state != "committed" && state != "cleaned" && state != "cleanup_pending" {
		return invalidAssetInput("state", "upload intent state is invalid")
	}
	result, err := tx.Exec(ctx, `
		UPDATE file_asset_upload_intents SET state=$1, updated_at=NOW()
		WHERE tenant_id=$2::uuid AND id=$3::uuid`, state, normalizedTenantID, normalizedID)
	if err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset upload intent state could not be updated")
	}
	if result.RowsAffected() == 0 {
		return &errs.Error{Code: errs.NotFound, Message: "file asset upload intent not found"}
	}
	return nil
}

// ListPendingUploadIntentsTx returns old recoverable intents for a trusted
// internal cleanup worker. FOR UPDATE SKIP LOCKED avoids duplicate workers.
func ListPendingUploadIntentsTx(ctx context.Context, tx *sqldb.Tx, olderThan time.Time, limit int) ([]UploadIntent, error) {
	if tx == nil {
		return nil, invalidStoreTransaction()
	}
	if olderThan.IsZero() || limit < 1 || limit > 100 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "upload intent recovery query is invalid"}
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, tenant_id::text, object_key
		FROM file_asset_upload_intents
		WHERE state IN ('pending', 'cleanup_pending') AND created_at < $1
		ORDER BY created_at, id
		LIMIT $2
		FOR UPDATE SKIP LOCKED`, olderThan.UTC(), limit)
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "file asset upload intents could not be listed")
	}
	defer rows.Close()
	intents := make([]UploadIntent, 0)
	for rows.Next() {
		var intent UploadIntent
		if err := rows.Scan(&intent.ID, &intent.TenantID, &intent.ObjectKey); err != nil {
			return nil, errs.WrapCode(err, errs.Internal, "file asset upload intents could not be read")
		}
		intents = append(intents, intent)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "file asset upload intents could not be read")
	}
	return intents, nil
}
