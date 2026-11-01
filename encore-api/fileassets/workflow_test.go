package fileassets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type workflowFakeObject struct {
	contents    []byte
	contentType string
}

type workflowFakeObjectStore struct {
	mu          sync.Mutex
	objects     map[string]workflowFakeObject
	putCalls    int
	signCalls   int
	deleteCalls int
	putErr      error
	signErr     error
	deleteErr   error
}

func newWorkflowFakeObjectStore() *workflowFakeObjectStore {
	return &workflowFakeObjectStore{objects: make(map[string]workflowFakeObject)}
}

func (store *workflowFakeObjectStore) Put(_ context.Context, key string, contents []byte, contentType string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.putCalls++
	if store.putErr != nil {
		return store.putErr
	}
	store.objects[key] = workflowFakeObject{contents: append([]byte(nil), contents...), contentType: contentType}
	return nil
}

func (store *workflowFakeObjectStore) SignedDownloadURL(_ context.Context, key string, _ time.Duration) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.signCalls++
	if store.signErr != nil {
		return "", store.signErr
	}
	return fmt.Sprintf("http://objects.test/%s?signature=signature-%d", key, store.signCalls), nil
}

func (store *workflowFakeObjectStore) Delete(_ context.Context, key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.deleteCalls++
	if store.deleteErr != nil {
		return store.deleteErr
	}
	delete(store.objects, key)
	return nil
}

func (store *workflowFakeObjectStore) counts() (puts, signs, deletes, objectCount int) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.putCalls, store.signCalls, store.deleteCalls, len(store.objects)
}

func workflowService(t *testing.T, store *workflowFakeObjectStore) (*Service, *SecretBox) {
	t.Helper()
	box, err := NewSecretBox("test-key", map[string][]byte{"test-key": bytes.Repeat([]byte{0x6b}, 32)})
	require.NoError(t, err)
	return &Service{objects: store, box: box}, box
}

func workflowPNG(t *testing.T) ([]byte, ImageMetadata) {
	t.Helper()
	contents := encodedFixture(t, png.Encode)
	parsed, metadata, err := ReadTemplateImageUpload(bytes.NewReader(contents), "image/png")
	require.NoError(t, err)
	if !bytes.Equal(contents, parsed) {
		t.Fatal("PNG input bytes changed during sniffing")
	}
	return contents, metadata
}

type workflowDBCounts struct {
	assets   int64
	audits   int64
	receipts int64
	intents  int64
}

func workflowCounts(t *testing.T, ctx context.Context, tenantID string) workflowDBCounts {
	t.Helper()
	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer tx.Rollback()
	var counts workflowDBCounts
	err = tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM file_assets WHERE tenant_id=$1::uuid),
			(SELECT count(*) FROM file_asset_audit_events WHERE tenant_id=$1::uuid),
			(SELECT count(*) FROM file_asset_write_receipts WHERE tenant_id=$1::uuid),
			(SELECT count(*) FROM file_asset_upload_intents WHERE tenant_id=$1::uuid)`, tenantID,
	).Scan(&counts.assets, &counts.audits, &counts.receipts, &counts.intents)
	require.NoError(t, err)
	return counts
}

func TestCreateAssetWorkflowPersistsEncryptedStateAndReplaysStableResponse(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.NewString()
	principal := assetPrincipal{tenantID: tenantID, subjectID: "subject:fileasset-workflow"}
	contents, metadata := workflowPNG(t)
	store := newWorkflowFakeObjectStore()
	service, box := workflowService(t, store)

	created, err := service.createAsset(ctx, principal, "asset-create-key-0001", contents, metadata, uuid.NewString())
	require.NoError(t, err)
	require.NotNil(t, created)
	require.False(t, created.replayed)
	require.Equal(t, "/api/v2/file-assets/"+mustAssetIDFromLocation(t, created.location), created.location)
	var createdResponse FileAssetResponse
	require.NoError(t, json.Unmarshal(created.body, &createdResponse))
	if createdResponse.Data.DownloadURL == "" {
		t.Fatal("successful create omitted its signed download capability")
	}
	assetID := createdResponse.Data.ID
	require.Equal(t, created.requestID, createdResponse.Meta.RequestID)

	replay, err := service.createAsset(ctx, principal, "asset-create-key-0001", contents, metadata, uuid.NewString())
	require.NoError(t, err)
	require.NotNil(t, replay)
	require.True(t, replay.replayed)
	if !bytes.Equal(created.body, replay.body) {
		t.Fatal("idempotent replay response bytes changed")
	}
	require.Equal(t, created.location, replay.location)
	require.Equal(t, created.requestID, replay.requestID)
	puts, signs, _, objectCount := store.counts()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, signs)
	require.Equal(t, 1, objectCount)

	counts := workflowCounts(t, ctx, tenantID)
	require.Equal(t, workflowDBCounts{assets: 1, audits: 1, receipts: 1, intents: 1}, counts)

	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer tx.Rollback()
	require.Equal(t, "committed", workflowIntentState(t, ctx, tx, tenantID, assetID))
	var leaseText, receiptText string
	require.NoError(t, tx.QueryRow(ctx, `SELECT encrypted_envelope::text FROM file_asset_url_leases WHERE tenant_id=$1::uuid AND asset_id=$2::uuid`, tenantID, assetID).Scan(&leaseText))
	require.NoError(t, tx.QueryRow(ctx, `SELECT encrypted_response::text FROM file_asset_write_receipts WHERE tenant_id=$1::uuid AND subject_id=$2 AND operation='createFileAsset' AND canonical_path=$3 AND idempotency_key=$4`, tenantID, principal.subjectID, createAssetPath, "asset-create-key-0001").Scan(&receiptText))
	if strings.Contains(leaseText, createdResponse.Data.DownloadURL) || strings.Contains(receiptText, createdResponse.Data.DownloadURL) {
		t.Fatal("a persisted encrypted envelope contains the signed URL in plaintext")
	}
	var leaseEnvelope Envelope
	require.NoError(t, json.Unmarshal([]byte(leaseText), &leaseEnvelope))
	for _, wrongAAD := range [][]byte{
		assetAAD("download-lease", uuid.NewString(), assetID, createdResponse.Data.ExpiresAt),
		assetAAD("download-lease", tenantID, uuid.NewString(), createdResponse.Data.ExpiresAt),
		assetAAD("other-purpose", tenantID, assetID, createdResponse.Data.ExpiresAt),
	} {
		_, err = box.Open(leaseEnvelope, wrongAAD)
		var secretErr *SecretBoxError
		require.ErrorAs(t, err, &secretErr)
		require.Equal(t, SecretBoxIntegrityError, secretErr.Code)
	}

	changedContents := append(append([]byte(nil), contents...), 0)
	_, changedMetadata, err := ReadTemplateImageUpload(bytes.NewReader(changedContents), "image/png")
	require.NoError(t, err)
	_, err = service.createAsset(ctx, principal, "asset-create-key-0001", changedContents, changedMetadata, uuid.NewString())
	require.Equal(t, errs.AlreadyExists, errs.Code(err))
	puts, signs, _, _ = store.counts()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, signs)

	restartedService := &Service{objects: store, box: box}
	restartedReplay, err := restartedService.createAsset(ctx, principal, "asset-create-key-0001", contents, metadata, uuid.NewString())
	require.NoError(t, err)
	if !bytes.Equal(created.body, restartedReplay.body) {
		t.Fatal("restarted service did not replay the persisted response bytes")
	}
	beforeETag := assetETag(&createdResponse.Data)
	readBack, err := restartedService.getAsset(ctx, principal, assetID)
	require.NoError(t, err)
	if readBack.DownloadURL != createdResponse.Data.DownloadURL || assetETag(readBack) != beforeETag {
		t.Fatal("service restart did not reuse the same signed URL lease and ETag")
	}
	readAgain, err := restartedService.getAsset(ctx, principal, assetID)
	require.NoError(t, err)
	if readAgain.DownloadURL != readBack.DownloadURL || assetETag(readAgain) != beforeETag {
		t.Fatal("repeated GET changed the signed URL lease or ETag")
	}
	puts, signs, _, _ = store.counts()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, signs)

	crossTenant := assetPrincipal{tenantID: uuid.NewString(), subjectID: principal.subjectID}
	_, err = restartedService.getAsset(ctx, crossTenant, assetID)
	require.Equal(t, errs.NotFound, errs.Code(err))
	puts, signs, _, _ = store.counts()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, signs)
}

func TestCreateAssetWorkflowIsolatesReceiptScopes(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	contents, metadata := workflowPNG(t)
	store := newWorkflowFakeObjectStore()
	service, _ := workflowService(t, store)

	principals := []assetPrincipal{
		{tenantID: tenantA, subjectID: "subject:first"},
		{tenantID: tenantA, subjectID: "subject:second"},
		{tenantID: tenantB, subjectID: "subject:first"},
	}
	assetIDs := make(map[string]bool)
	for index, principal := range principals {
		result, err := service.createAsset(ctx, principal, "same-key-0000001", contents, metadata, fmt.Sprintf("scope-request-%d", index))
		require.NoError(t, err)
		var response FileAssetResponse
		require.NoError(t, json.Unmarshal(result.body, &response))
		assetIDs[response.Data.ID] = true
	}
	require.Len(t, assetIDs, len(principals))
	puts, signs, _, _ := store.counts()
	require.Equal(t, len(principals), puts)
	require.Equal(t, len(principals), signs)
	require.Equal(t, workflowDBCounts{assets: 2, audits: 2, receipts: 2, intents: 2}, workflowCounts(t, ctx, tenantA))
	require.Equal(t, workflowDBCounts{assets: 1, audits: 1, receipts: 1, intents: 1}, workflowCounts(t, ctx, tenantB))
}

func TestCreateAssetWorkflowCompensatesPutAndSigningFailures(t *testing.T) {
	ctx := context.Background()
	contents, metadata := workflowPNG(t)
	putFailure := errors.New("synthetic object put failure")
	signFailure := errors.New("synthetic URL signing failure")
	for _, testCase := range []struct {
		name      string
		putErr    error
		signErr   error
		wantPuts  int
		wantSigns int
	}{
		{name: "put failure", putErr: putFailure, wantPuts: 1},
		{name: "sign failure", signErr: signFailure, wantPuts: 1, wantSigns: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tenantID := uuid.NewString()
			store := newWorkflowFakeObjectStore()
			store.putErr, store.signErr = testCase.putErr, testCase.signErr
			service, _ := workflowService(t, store)
			_, err := service.createAsset(ctx, assetPrincipal{tenantID: tenantID, subjectID: "subject:failure-test"}, "failure-key-0001", contents, metadata, uuid.NewString())
			require.Equal(t, errs.Unavailable, errs.Code(err))
			puts, signs, deletes, objectCount := store.counts()
			require.Equal(t, testCase.wantPuts, puts)
			require.Equal(t, testCase.wantSigns, signs)
			require.Equal(t, 1, deletes)
			require.Zero(t, objectCount)
			counts := workflowCounts(t, ctx, tenantID)
			require.Equal(t, workflowDBCounts{intents: 1}, counts)
			tx, err := BeginTenantScopedTx(ctx, tenantID)
			require.NoError(t, err)
			defer tx.Rollback()
			rows, err := tx.Query(ctx, `SELECT state FROM file_asset_upload_intents WHERE tenant_id=$1::uuid`, tenantID)
			require.NoError(t, err)
			defer rows.Close()
			require.True(t, rows.Next())
			var state string
			require.NoError(t, rows.Scan(&state))
			require.Equal(t, "cleaned", state)
			require.False(t, rows.Next())
		})
	}
}

func TestCreateAssetWorkflowConcurrentSameKeyWritesOnlyOnce(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.NewString()
	principal := assetPrincipal{tenantID: tenantID, subjectID: "subject:concurrent"}
	contents, metadata := workflowPNG(t)
	store := newWorkflowFakeObjectStore()
	service, _ := workflowService(t, store)
	type response struct {
		result *assetCreateResult
		err    error
	}
	results := make(chan response, 2)
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		go func(index int) {
			ready <- struct{}{}
			<-start
			result, err := service.createAsset(ctx, principal, "concurrent-key-0001", contents, metadata, fmt.Sprintf("concurrent-request-%d", index))
			results <- response{result: result, err: err}
		}(index)
	}
	<-ready
	<-ready
	close(start)
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.NotNil(t, first.result)
	require.NotNil(t, second.result)
	if !bytes.Equal(first.result.body, second.result.body) {
		t.Fatal("concurrent idempotent response bytes differ")
	}
	if first.result.replayed == second.result.replayed {
		t.Fatal("concurrent idempotency did not produce exactly one stored response and one replay")
	}
	require.Equal(t, first.result.location, second.result.location)
	require.Equal(t, first.result.requestID, second.result.requestID)
	puts, signs, _, objects := store.counts()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, signs)
	require.Equal(t, 1, objects)
	require.Equal(t, workflowDBCounts{assets: 1, audits: 1, receipts: 1, intents: 1}, workflowCounts(t, ctx, tenantID))
}

func TestCreateAssetWorkflowExpiredReceiptConflictDoesNotReuseKey(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.NewString()
	principal := assetPrincipal{tenantID: tenantID, subjectID: "subject:expired"}
	scope := ReceiptScope{TenantID: tenantID, SubjectID: principal.subjectID, Operation: "createFileAsset", Path: createAssetPath, Key: "expired-key-0001"}
	contents, metadata := workflowPNG(t)
	hash := uploadRequestHash(metadata)
	expiresAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	service, _ := workflowService(t, newWorkflowFakeObjectStore())
	envelope, err := service.box.Seal([]byte("expired-response-placeholder"), receiptAAD(scope, hash, expiresAt))
	require.NoError(t, err)
	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	require.NoError(t, LockReceiptTx(ctx, tx, scope))
	require.NoError(t, InsertReceiptTx(ctx, tx, scope, Receipt{
		RequestHash: hash, Location: createAssetPath + "/" + uuid.NewString(), RequestID: "expired-request",
		Response: envelope, ExpiresAt: expiresAt,
	}))
	require.NoError(t, tx.Commit())

	_, err = service.createAsset(ctx, principal, scope.Key, contents, metadata, uuid.NewString())
	require.Equal(t, errs.AlreadyExists, errs.Code(err))
	counts := workflowCounts(t, ctx, tenantID)
	require.Equal(t, workflowDBCounts{receipts: 1}, counts)
	puts, signs, _, objects := service.objects.(*workflowFakeObjectStore).counts()
	require.Zero(t, puts)
	require.Zero(t, signs)
	require.Zero(t, objects)
}

func workflowIntentState(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID, assetID string) string {
	t.Helper()
	var state string
	require.NoError(t, tx.QueryRow(ctx, `SELECT state FROM file_asset_upload_intents WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, assetID).Scan(&state))
	return state
}

func mustAssetIDFromLocation(t *testing.T, location string) string {
	t.Helper()
	const prefix = "/api/v2/file-assets/"
	if !strings.HasPrefix(location, prefix) {
		t.Fatal("file asset create returned an unexpected Location path")
	}
	id := strings.TrimPrefix(location, prefix)
	if _, err := uuid.Parse(id); err != nil {
		t.Fatal("file asset create Location did not contain an asset ID")
	}
	return id
}
