package fileassets

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

// URLLease contains only an authenticated encrypted envelope and its expiry.
// The URL itself is never persisted in plaintext by this package.
type URLLease struct {
	Envelope  Envelope
	ExpiresAt time.Time
}

// LockAssetTx locks a live catalog row before a caller creates a signed URL.
func LockAssetTx(ctx context.Context, tx *sqldb.Tx, tenantID, assetID string) error {
	normalizedTenantID, normalizedAssetID, err := validateTenantAssetTxInput(tx, tenantID, assetID)
	if err != nil {
		return err
	}
	var lockedID string
	err = tx.QueryRow(ctx, `
		SELECT id::text FROM file_assets
		WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, normalizedTenantID, normalizedAssetID).Scan(&lockedID)
	if errors.Is(err, sqldb.ErrNoRows) {
		return &errs.Error{Code: errs.NotFound, Message: "file asset not found"}
	}
	if err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset could not be locked")
	}
	return nil
}

// LoadURLLeaseTx loads the persisted encrypted URL lease; absence is not an
// error so callers can create and save a new lease in the same transaction.
func LoadURLLeaseTx(ctx context.Context, tx *sqldb.Tx, tenantID, assetID string) (*URLLease, error) {
	normalizedTenantID, normalizedAssetID, err := validateTenantAssetTxInput(tx, tenantID, assetID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	lease := &URLLease{}
	err = tx.QueryRow(ctx, `
		SELECT encrypted_envelope, expires_at FROM file_asset_url_leases
		WHERE tenant_id=$1::uuid AND asset_id=$2::uuid`, normalizedTenantID, normalizedAssetID).Scan(&raw, &lease.ExpiresAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "file asset lease could not be loaded")
	}
	if err := json.Unmarshal(raw, &lease.Envelope); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "stored file asset lease is invalid")
	}
	if !validStoredEnvelope(lease.Envelope) {
		return nil, &errs.Error{Code: errs.Internal, Message: "stored file asset lease is invalid"}
	}
	lease.ExpiresAt = lease.ExpiresAt.UTC()
	return lease, nil
}

// SaveURLLeaseTx inserts or replaces a lease for one tenant-owned asset. The
// caller is responsible for locking the asset and encrypting URL bytes first.
func SaveURLLeaseTx(ctx context.Context, tx *sqldb.Tx, tenantID, assetID string, lease URLLease) error {
	normalizedTenantID, normalizedAssetID, err := validateTenantAssetTxInput(tx, tenantID, assetID)
	if err != nil {
		return err
	}
	if lease.ExpiresAt.IsZero() || !validStoredEnvelope(lease.Envelope) {
		return invalidLeaseInput()
	}
	encoded, err := json.Marshal(lease.Envelope)
	if err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset lease could not be encoded")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO file_asset_url_leases (tenant_id, asset_id, encrypted_envelope, expires_at)
		VALUES ($1::uuid, $2::uuid, $3::jsonb, $4)
		ON CONFLICT (tenant_id, asset_id) DO UPDATE
		SET encrypted_envelope=EXCLUDED.encrypted_envelope, expires_at=EXCLUDED.expires_at`,
		normalizedTenantID, normalizedAssetID, string(encoded), lease.ExpiresAt.UTC(),
	); err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset lease could not be stored")
	}
	return nil
}

func validStoredEnvelope(envelope Envelope) bool {
	return envelope.Version == SecretBoxVersion && validKeyID(envelope.KeyID) && len(envelope.Nonce) == 12 &&
		len(envelope.Ciphertext) >= 16 && len(envelope.Ciphertext) <= MaxSecretBoxPlaintext+16
}

func validateTenantAssetTxInput(tx *sqldb.Tx, tenantID, assetID string) (string, string, error) {
	if tx == nil {
		return "", "", invalidStoreTransaction()
	}
	normalizedTenantID, err := canonicalUUID(tenantID)
	if err != nil {
		return "", "", invalidAssetInput("tenantId", "tenant ID must be a canonical non-nil UUID")
	}
	normalizedAssetID, err := canonicalUUID(assetID)
	if err != nil {
		return "", "", invalidAssetInput("assetId", "asset ID must be a canonical non-nil UUID")
	}
	return normalizedTenantID, normalizedAssetID, nil
}

func invalidLeaseInput() error {
	return &errs.Error{Code: errs.InvalidArgument, Message: "encrypted file asset lease is invalid"}
}

func invalidStoreTransaction() error {
	return &errs.Error{Code: errs.InvalidArgument, Message: "file asset transaction is required"}
}
