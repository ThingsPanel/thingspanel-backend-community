package fileassets

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRecoverCommittedAssetIntentDoesNotDeleteAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	tenantID, assetID := uuid.NewString(), uuid.NewString()
	objectKey := "recover/" + tenantID + "/" + assetID
	intent := UploadIntent{ID: assetID, TenantID: tenantID, ObjectKey: objectKey}
	store := newWorkflowFakeObjectStore()
	service, _ := workflowService(t, store)
	require.NoError(t, CreateUploadIntent(ctx, tenantID, assetID, objectKey))
	require.NoError(t, store.Put(ctx, objectKey, []byte("fixture"), "image/png"))

	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	_, err = InsertTx(ctx, tx, tenantID, assetID, objectKey, "image/png", 7, testChecksum("a"), "template-image", "subject:recovery-test")
	require.NoError(t, err)
	require.NoError(t, MarkUploadIntentTx(ctx, tx, tenantID, assetID, "committed"))
	require.NoError(t, tx.Commit())

	store.mu.Lock()
	store.deleteErr = errors.New("delete must never be attempted for a valid asset")
	store.mu.Unlock()
	require.NoError(t, service.recoverUploadIntent(ctx, intent))
	require.NoError(t, service.recoverUploadIntent(ctx, intent))
	_, _, deletes, objects := store.counts()
	require.Zero(t, deletes)
	require.Equal(t, 1, objects)

	checkTx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer checkTx.Rollback()
	require.Equal(t, "committed", workflowIntentState(t, ctx, checkTx, tenantID, assetID))
	asset, err := LoadTx(ctx, checkTx, tenantID, assetID)
	require.NoError(t, err)
	require.Equal(t, objectKey, asset.ObjectKey)
}

func TestRecoverOrphanIntentDeletesObjectAndRepeatedRecoveryIsSafe(t *testing.T) {
	ctx := context.Background()
	tenantID, assetID := uuid.NewString(), uuid.NewString()
	objectKey := "orphan/" + tenantID + "/" + assetID
	intent := UploadIntent{ID: assetID, TenantID: tenantID, ObjectKey: objectKey}
	store := newWorkflowFakeObjectStore()
	service, _ := workflowService(t, store)
	require.NoError(t, CreateUploadIntent(ctx, tenantID, assetID, objectKey))
	require.NoError(t, store.Put(ctx, objectKey, []byte("orphan fixture"), "image/png"))

	require.NoError(t, service.recoverUploadIntent(ctx, intent))
	_, _, deletes, objects := store.counts()
	require.Equal(t, 1, deletes)
	require.Zero(t, objects)
	assertIntentState(t, ctx, tenantID, assetID, "cleaned")

	// The fake delete is idempotent for an already removed key, matching the
	// recovery worker's behavior if it is invoked again after a lost response.
	require.NoError(t, service.recoverUploadIntent(ctx, intent))
	_, _, deletes, objects = store.counts()
	require.Equal(t, 2, deletes)
	require.Zero(t, objects)
	assertIntentState(t, ctx, tenantID, assetID, "cleaned")
}

func TestRecoverOrphanDeleteFailureRemainsRetryable(t *testing.T) {
	ctx := context.Background()
	tenantID, assetID := uuid.NewString(), uuid.NewString()
	objectKey := "retry/" + tenantID + "/" + assetID
	intent := UploadIntent{ID: assetID, TenantID: tenantID, ObjectKey: objectKey}
	store := newWorkflowFakeObjectStore()
	service, _ := workflowService(t, store)
	require.NoError(t, CreateUploadIntent(ctx, tenantID, assetID, objectKey))
	require.NoError(t, store.Put(ctx, objectKey, []byte("retry fixture"), "image/png"))
	store.mu.Lock()
	store.deleteErr = errors.New("temporary synthetic delete failure")
	store.mu.Unlock()

	err := service.recoverUploadIntent(ctx, intent)
	require.Equal(t, errs.Unavailable, errs.Code(err))
	_, _, deletes, objects := store.counts()
	require.Equal(t, 1, deletes)
	require.Equal(t, 1, objects)
	assertIntentState(t, ctx, tenantID, assetID, "pending")

	store.mu.Lock()
	store.deleteErr = nil
	store.mu.Unlock()
	require.NoError(t, service.recoverUploadIntent(ctx, intent))
	_, _, deletes, objects = store.counts()
	require.Equal(t, 2, deletes)
	require.Zero(t, objects)
	assertIntentState(t, ctx, tenantID, assetID, "cleaned")
}

func TestListPendingUploadIntentsSkipsLockedRowsWithoutBlocking(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.NewString()
	lockedID, availableID := uuid.NewString(), uuid.NewString()
	lockedKey, availableKey := "lock/"+lockedID, "lock/"+availableID
	require.NoError(t, CreateUploadIntent(ctx, tenantID, lockedID, lockedKey))
	require.NoError(t, CreateUploadIntent(ctx, tenantID, availableID, availableKey))

	lockTx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	require.NoError(t, LockUploadIntentTx(ctx, lockTx, tenantID, lockedID))

	listCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	listTx, err := BeginTenantScopedTx(listCtx, tenantID)
	require.NoError(t, err)
	intents, err := ListPendingUploadIntentsTx(listCtx, listTx, time.Now().UTC().Add(time.Hour), 100)
	require.NoError(t, err)
	listed := make(map[string]bool, len(intents))
	for _, intent := range intents {
		listed[intent.ID] = true
	}
	require.False(t, listed[lockedID])
	require.True(t, listed[availableID])
	require.NoError(t, listTx.Rollback())
	require.NoError(t, lockTx.Rollback())

	markRecoveryFixtureCleaned(t, ctx, tenantID, lockedID)
	markRecoveryFixtureCleaned(t, ctx, tenantID, availableID)
}

func TestRecoveryBatchItemForOtherTenantCommitsValidAssetWithoutDeleting(t *testing.T) {
	ctx := context.Background()
	assetTenant, currentTenant, assetID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	objectKey := "batch/" + assetTenant + "/" + assetID
	intent := UploadIntent{ID: assetID, TenantID: assetTenant, ObjectKey: objectKey}
	store := newWorkflowFakeObjectStore()
	service, _ := workflowService(t, store)
	require.NoError(t, CreateUploadIntent(ctx, assetTenant, assetID, objectKey))
	require.NoError(t, store.Put(ctx, objectKey, []byte("valid asset"), "image/png"))

	assetTx, err := BeginTenantScopedTx(ctx, assetTenant)
	require.NoError(t, err)
	_, err = InsertTx(ctx, assetTx, assetTenant, assetID, objectKey, "image/png", 11, testChecksum("b"), "template-image", "subject:batch-test")
	require.NoError(t, err)
	require.NoError(t, assetTx.Commit())

	// This is the same per-intent operation used by the global cron after it
	// switches the transaction-local tenant to the intent's trusted tenant.
	workerTx, err := db.Begin(ctx)
	require.NoError(t, err)
	defer workerTx.Rollback()
	_, err = workerTx.Exec(ctx, `SELECT set_config('app.current_tenant_id',$1,true)`, currentTenant)
	require.NoError(t, err)
	var beforeSwitch string
	require.NoError(t, workerTx.QueryRow(ctx, `SELECT current_setting('app.current_tenant_id')`).Scan(&beforeSwitch))
	require.Equal(t, currentTenant, beforeSwitch)
	_, err = workerTx.Exec(ctx, `SELECT set_config('app.current_tenant_id',$1,true)`, assetTenant)
	require.NoError(t, err)
	var currentSetting string
	require.NoError(t, workerTx.QueryRow(ctx, `SELECT current_setting('app.current_tenant_id')`).Scan(&currentSetting))
	require.Equal(t, assetTenant, currentSetting)
	require.NoError(t, LockUploadIntentTx(ctx, workerTx, assetTenant, assetID))
	require.NoError(t, service.recoverUploadIntentTx(ctx, workerTx, intent))
	require.NoError(t, workerTx.Commit())

	_, _, deletes, objects := store.counts()
	require.Zero(t, deletes)
	require.Equal(t, 1, objects)
	assertIntentState(t, ctx, assetTenant, assetID, "committed")
}

func assertIntentState(t *testing.T, ctx context.Context, tenantID, id, want string) {
	t.Helper()
	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer tx.Rollback()
	require.Equal(t, want, workflowIntentState(t, ctx, tx, tenantID, id))
}

func markRecoveryFixtureCleaned(t *testing.T, ctx context.Context, tenantID, id string) {
	t.Helper()
	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, LockUploadIntentTx(ctx, tx, tenantID, id))
	require.NoError(t, MarkUploadIntentTx(ctx, tx, tenantID, id, "cleaned"))
	require.NoError(t, tx.Commit())
}

func testChecksum(fill string) string {
	return strings.Repeat(fill[:1], 64)
}
