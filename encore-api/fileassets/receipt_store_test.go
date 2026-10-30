package fileassets

import (
	"context"
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReceiptStoreLocksAndReplaysEncryptedSuccessFields(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.NewString()
	scope := ReceiptScope{
		TenantID: tenantID, SubjectID: "subject:receipt-test", Operation: "createFileAsset",
		Path: "/api/v2/file-assets", Key: "receipt-key-0001",
	}
	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer tx.Rollback()

	require.NoError(t, LockReceiptTx(ctx, tx, scope))
	missing, err := LoadReceiptTx(ctx, tx, scope)
	require.NoError(t, err)
	require.Nil(t, missing)

	box := newTestSecretBox(t, "k2", map[string][]byte{"k2": bytesOfKey('r')})
	responseBytes := []byte(`{"data":{"id":"opaque-asset-id"},"meta":{"requestId":"request-123"}}`)
	envelope, err := box.Seal(responseBytes, []byte("tenant="+tenantID+";scope="+scope.Path+";purpose=receipt-response"))
	require.NoError(t, err)
	expiresAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	want := Receipt{
		RequestHash: strings.Repeat("b", 64), Location: "/api/v2/file-assets/opaque-asset-id",
		RequestID: "request-123", Response: envelope, ExpiresAt: expiresAt,
	}
	require.NoError(t, InsertReceiptTx(ctx, tx, scope, want))
	loaded, err := LoadReceiptTx(ctx, tx, scope)
	require.NoError(t, err)
	require.Equal(t, &want, loaded)
	require.Equal(t, expiresAt, loaded.ExpiresAt)

	for _, differentScope := range []ReceiptScope{
		{TenantID: uuid.NewString(), SubjectID: scope.SubjectID, Operation: scope.Operation, Path: scope.Path, Key: scope.Key},
		{TenantID: scope.TenantID, SubjectID: "subject:other", Operation: scope.Operation, Path: scope.Path, Key: scope.Key},
		{TenantID: scope.TenantID, SubjectID: scope.SubjectID, Operation: "updateFileAsset", Path: scope.Path, Key: scope.Key},
		{TenantID: scope.TenantID, SubjectID: scope.SubjectID, Operation: scope.Operation, Path: "/api/v2/other", Key: scope.Key},
		{TenantID: scope.TenantID, SubjectID: scope.SubjectID, Operation: scope.Operation, Path: scope.Path, Key: "receipt-key-0002"},
	} {
		require.NoError(t, LockReceiptTx(ctx, tx, differentScope))
		other, err := LoadReceiptTx(ctx, tx, differentScope)
		require.NoError(t, err)
		require.Nil(t, other)
	}
	// Expired rows remain discoverable and reserve their key; replay policy is
	// deliberately decided by the caller after loading this stored record.
	require.NoError(t, tx.Rollback())

	checkTx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer checkTx.Rollback()
	afterRollback, err := LoadReceiptTx(ctx, checkTx, scope)
	require.NoError(t, err)
	require.Nil(t, afterRollback)
}

func TestAssetAuditStoreUsesTenantOwnedMetadata(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB, assetID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	tx, err := BeginTenantScopedTx(ctx, tenantA)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, insertLeaseTestAsset(ctx, tx, tenantA, assetID))

	require.Equal(t, errs.NotFound, errs.Code(InsertAssetAuditTx(ctx, tx, tenantB, assetID, "subject:test", "request-1")))
	require.NoError(t, InsertAssetAuditTx(ctx, tx, tenantA, assetID, "subject:test", "request-1"))
	var actor, operation, requestID, contentType, checksum string
	var sizeBytes int64
	err = tx.QueryRow(ctx, `
		SELECT actor, operation, request_id, content_type, size_bytes, checksum
		FROM file_asset_audit_events WHERE tenant_id=$1::uuid AND asset_id=$2::uuid`, tenantA, assetID,
	).Scan(&actor, &operation, &requestID, &contentType, &sizeBytes, &checksum)
	require.NoError(t, err)
	require.Equal(t, "subject:test", actor)
	require.Equal(t, "file_asset.created", operation)
	require.Equal(t, "request-1", requestID)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, int64(128), sizeBytes)
	require.Equal(t, strings.Repeat("a", 64), checksum)
}

func TestLeaseReceiptAndAuditHelpersRejectNilTransactions(t *testing.T) {
	ctx := context.Background()
	tenantID, assetID := uuid.NewString(), uuid.NewString()
	scope := ReceiptScope{TenantID: tenantID, SubjectID: "subject", Operation: "create", Path: "/api/v2/file-assets", Key: "key-12345678"}
	require.Equal(t, errs.InvalidArgument, errs.Code(LockAssetTx(ctx, nil, tenantID, assetID)))
	_, err := LoadURLLeaseTx(ctx, nil, tenantID, assetID)
	require.Equal(t, errs.InvalidArgument, errs.Code(err))
	require.Equal(t, errs.InvalidArgument, errs.Code(SaveURLLeaseTx(ctx, nil, tenantID, assetID, URLLease{})))
	require.Equal(t, errs.InvalidArgument, errs.Code(LockReceiptTx(ctx, nil, scope)))
	_, err = LoadReceiptTx(ctx, nil, scope)
	require.Equal(t, errs.InvalidArgument, errs.Code(err))
	require.Equal(t, errs.InvalidArgument, errs.Code(InsertReceiptTx(ctx, nil, scope, Receipt{})))
	require.Equal(t, errs.InvalidArgument, errs.Code(InsertAssetAuditTx(ctx, nil, tenantID, assetID, "actor", "request")))
}
