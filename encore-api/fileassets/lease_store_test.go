package fileassets

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestURLLeaseStoreRoundTripUpsertAndRollback(t *testing.T) {
	ctx := context.Background()
	tenantID, assetID := uuid.NewString(), uuid.NewString()
	tx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, insertLeaseTestAsset(ctx, tx, tenantID, assetID))
	require.NoError(t, LockAssetTx(ctx, tx, tenantID, assetID))

	missing, err := LoadURLLeaseTx(ctx, tx, tenantID, assetID)
	require.NoError(t, err)
	require.Nil(t, missing)

	box := newTestSecretBox(t, "k1", map[string][]byte{"k1": bytesOfKey('k')})
	url1 := "https://storage.invalid/signed?token=do-not-store-in-plaintext"
	envelope1, err := box.Seal([]byte(url1), []byte("tenant="+tenantID+";asset="+assetID+";purpose=image-url"))
	require.NoError(t, err)
	expires1 := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Microsecond)
	require.NoError(t, SaveURLLeaseTx(ctx, tx, tenantID, assetID, URLLease{Envelope: envelope1, ExpiresAt: expires1}))
	loaded, err := LoadURLLeaseTx(ctx, tx, tenantID, assetID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, envelope1, loaded.Envelope)
	require.Equal(t, expires1, loaded.ExpiresAt)

	rawEnvelope, err := json.Marshal(loaded.Envelope)
	require.NoError(t, err)
	require.NotContains(t, string(rawEnvelope), url1)
	var envelopeFields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rawEnvelope, &envelopeFields))
	require.ElementsMatch(t, []string{"version", "keyId", "nonce", "ciphertext"}, mapKeys(envelopeFields))

	url2 := "https://storage.invalid/signed?token=second-secret"
	envelope2, err := box.Seal([]byte(url2), []byte("tenant="+tenantID+";asset="+assetID+";purpose=image-url"))
	require.NoError(t, err)
	expires2 := expires1.Add(time.Minute)
	require.NoError(t, SaveURLLeaseTx(ctx, tx, tenantID, assetID, URLLease{Envelope: envelope2, ExpiresAt: expires2}))
	loaded, err = LoadURLLeaseTx(ctx, tx, tenantID, assetID)
	require.NoError(t, err)
	require.Equal(t, envelope2, loaded.Envelope)
	require.Equal(t, expires2, loaded.ExpiresAt)
	require.NoError(t, tx.Rollback())

	checkTx, err := BeginTenantScopedTx(ctx, tenantID)
	require.NoError(t, err)
	defer checkTx.Rollback()
	afterRollback, err := LoadURLLeaseTx(ctx, checkTx, tenantID, assetID)
	require.NoError(t, err)
	require.Nil(t, afterRollback)
}

func TestURLLeaseStoreRejectsCrossTenantAssetAccess(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB, assetID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	tx, err := BeginTenantScopedTx(ctx, tenantA)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, insertLeaseTestAsset(ctx, tx, tenantA, assetID))

	require.Equal(t, errs.NotFound, errs.Code(LockAssetTx(ctx, tx, tenantB, assetID)))
	lease, err := LoadURLLeaseTx(ctx, tx, tenantB, assetID)
	require.NoError(t, err)
	require.Nil(t, lease)

	box := newTestSecretBox(t, "k1", map[string][]byte{"k1": bytesOfKey('x')})
	envelope, err := box.Seal([]byte("opaque url"), []byte("wrong tenant"))
	require.NoError(t, err)
	err = SaveURLLeaseTx(ctx, tx, tenantB, assetID, URLLease{Envelope: envelope, ExpiresAt: time.Now().UTC().Add(time.Minute)})
	require.Equal(t, errs.Internal, errs.Code(err))
	require.NoError(t, tx.Rollback())
}

func insertLeaseTestAsset(ctx context.Context, tx *sqldb.Tx, tenantID, assetID string) error {
	_, err := InsertTx(ctx, tx, tenantID, assetID, "tenant/"+tenantID+"/"+assetID, "image/png", 128, strings.Repeat("a", 64), "template-image", "subject:lease-test")
	return err
}

func bytesOfKey(value byte) []byte {
	return []byte(strings.Repeat(string(value), 32))
}

func mapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
