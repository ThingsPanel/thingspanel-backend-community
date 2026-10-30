package fileassets

import (
	"context"
	"strings"
	"testing"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFileAssetStoreInsertLoadAndCallerRollback(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.NewString()
	assetID := uuid.NewString()
	objectKey := "tenant/" + tenantID + "/" + assetID

	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	var currentTenant string
	require.NoError(t, tx.QueryRow(ctx, `SELECT current_setting('app.current_tenant_id')`).Scan(&currentTenant))
	require.Equal(t, tenantID, currentTenant)

	created, err := InsertTx(ctx, tx, tenantID, assetID, objectKey, "image/png", 128, strings.Repeat("a", 64), "template-image", "subject:upload-test")
	require.NoError(t, err)
	loaded, err := LoadTx(ctx, tx, tenantID, assetID)
	require.NoError(t, err)
	require.Equal(t, created, loaded)
	require.Equal(t, assetID, loaded.ID)
	require.Equal(t, tenantID, loaded.TenantID)

	require.NoError(t, tx.Rollback())
	checkTx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer checkTx.Rollback()
	_, err = LoadTx(ctx, checkTx, tenantID, assetID)
	require.Equal(t, errs.NotFound, errs.Code(err))
}

func TestFileAssetStoreTenantScopedNotFoundAndCommittedRead(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	assetID := uuid.NewString()
	objectKey := "tenant/" + tenantA + "/" + assetID
	asset := insertCommittedTestAsset(t, ctx, tenantA, assetID, objectKey)
	t.Cleanup(func() { deleteTestAsset(t, ctx, tenantA, assetID) })

	readA, err := BeginTenantScopedTx(ctx, tenantA)
	require.NoError(t, err)
	loaded, err := LoadTx(ctx, readA, tenantA, assetID)
	require.NoError(t, err)
	require.Equal(t, asset, loaded)
	require.NoError(t, readA.Commit())

	readB, err := BeginTenantScopedTx(ctx, tenantB)
	require.NoError(t, err)
	defer readB.Rollback()
	_, err = LoadTx(ctx, readB, tenantB, assetID)
	require.Equal(t, errs.NotFound, errs.Code(err))
}

func TestFileAssetStoreRejectsInvalidRowsBeforeSQL(t *testing.T) {
	testCases := []struct {
		name      string
		tenantID  string
		id        string
		objectKey string
		content   string
		size      int64
		checksum  string
		purpose   string
		createdBy string
		wantField string
	}{
		{name: "nil tenant", tenantID: uuid.Nil.String(), id: uuid.NewString(), objectKey: "object", content: "image/png", size: 1, checksum: strings.Repeat("a", 64), purpose: "template-image", createdBy: "actor", wantField: "tenantId"},
		{name: "noncanonical tenant", tenantID: strings.ToUpper(uuid.NewString()), id: uuid.NewString(), objectKey: "object", content: "image/png", size: 1, checksum: strings.Repeat("a", 64), purpose: "template-image", createdBy: "actor", wantField: "tenantId"},
		{name: "nil id", tenantID: uuid.NewString(), id: uuid.Nil.String(), objectKey: "object", content: "image/png", size: 1, checksum: strings.Repeat("a", 64), purpose: "template-image", createdBy: "actor", wantField: "id"},
		{name: "empty key", tenantID: uuid.NewString(), id: uuid.NewString(), objectKey: " ", content: "image/png", size: 1, checksum: strings.Repeat("a", 64), purpose: "template-image", createdBy: "actor", wantField: "objectKey"},
		{name: "bad mime", tenantID: uuid.NewString(), id: uuid.NewString(), objectKey: "object", content: "image/svg+xml", size: 1, checksum: strings.Repeat("a", 64), purpose: "template-image", createdBy: "actor", wantField: "contentType"},
		{name: "zero size", tenantID: uuid.NewString(), id: uuid.NewString(), objectKey: "object", content: "image/png", size: 0, checksum: strings.Repeat("a", 64), purpose: "template-image", createdBy: "actor", wantField: "sizeBytes"},
		{name: "too large", tenantID: uuid.NewString(), id: uuid.NewString(), objectKey: "object", content: "image/png", size: MaxTemplateImageBytes + 1, checksum: strings.Repeat("a", 64), purpose: "template-image", createdBy: "actor", wantField: "sizeBytes"},
		{name: "uppercase checksum", tenantID: uuid.NewString(), id: uuid.NewString(), objectKey: "object", content: "image/png", size: 1, checksum: strings.Repeat("A", 64), purpose: "template-image", createdBy: "actor", wantField: "checksum"},
		{name: "wrong checksum length", tenantID: uuid.NewString(), id: uuid.NewString(), objectKey: "object", content: "image/png", size: 1, checksum: "abc", purpose: "template-image", createdBy: "actor", wantField: "checksum"},
		{name: "wrong purpose", tenantID: uuid.NewString(), id: uuid.NewString(), objectKey: "object", content: "image/png", size: 1, checksum: strings.Repeat("a", 64), purpose: "avatar", createdBy: "actor", wantField: "purpose"},
		{name: "empty actor", tenantID: uuid.NewString(), id: uuid.NewString(), objectKey: "object", content: "image/png", size: 1, checksum: strings.Repeat("a", 64), purpose: "template-image", createdBy: " ", wantField: "createdBy"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := InsertTx(context.Background(), nil,
				testCase.tenantID, testCase.id, testCase.objectKey, testCase.content,
				testCase.size, testCase.checksum, testCase.purpose, testCase.createdBy)
			var validationErr *errs.Error
			require.ErrorAs(t, err, &validationErr)
			require.Equal(t, errs.InvalidArgument, errs.Code(err))
			require.Contains(t, validationErr.Message, testCase.wantField)
		})
	}
}

func TestFileAssetStoreRejectsIdentityAndContentUpdates(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.NewString()
	assetID := uuid.NewString()
	insertCommittedTestAsset(t, ctx, tenantID, assetID, "tenant/"+tenantID+"/"+assetID)
	t.Cleanup(func() { deleteTestAsset(t, ctx, tenantID, assetID) })

	testCases := []struct {
		name string
		stmt string
		args []any
	}{
		{name: "tenant identity", stmt: `UPDATE file_assets SET tenant_id=$1::uuid WHERE tenant_id=$2::uuid AND id=$3::uuid`, args: []any{uuid.NewString(), tenantID, assetID}},
		{name: "object content", stmt: `UPDATE file_assets SET object_key=$1 WHERE tenant_id=$2::uuid AND id=$3::uuid`, args: []any{"mutated-object-key", tenantID, assetID}},
		{name: "content metadata", stmt: `UPDATE file_assets SET checksum=$1 WHERE tenant_id=$2::uuid AND id=$3::uuid`, args: []any{strings.Repeat("b", 64), tenantID, assetID}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tx, err := BeginTenantScopedTx(ctx, tenantID)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, testCase.stmt, testCase.args...)
			require.Error(t, err)
			require.NoError(t, tx.Rollback())
		})
	}
}

func insertCommittedTestAsset(t *testing.T, ctx context.Context, tenantID, id, objectKey string) *Asset {
	t.Helper()
	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	asset, err := InsertTx(ctx, tx, tenantID, id, objectKey, "image/jpeg", 77, strings.Repeat("c", 64), "template-image", "subject:fileassets-test")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	return asset
}

func deleteTestAsset(t *testing.T, ctx context.Context, tenantID, id string) {
	t.Helper()
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(ctx, `DELETE FROM file_assets WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, id)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}
