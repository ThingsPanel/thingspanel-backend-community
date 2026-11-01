package fileassets

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// Asset is the immutable, tenant-owned file catalog record. It intentionally
// contains no signed URL or object-storage credential.
type Asset struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantId"`
	ObjectKey   string    `json:"objectKey"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	Checksum    string    `json:"checksum"`
	Purpose     string    `json:"purpose"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

// BeginTenantScopedTx starts a caller-owned transaction and sets the trusted,
// transaction-local tenant GUC. The caller must commit or roll back the result.
func BeginTenantScopedTx(ctx context.Context, tenantID string) (*sqldb.Tx, error) {
	normalizedTenantID, err := canonicalUUID(tenantID)
	if err != nil {
		return nil, invalidAssetInput("tenantId", "tenant ID must be a canonical non-nil UUID")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "file asset transaction could not be started")
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_tenant_id', $1, true)`, normalizedTenantID); err != nil {
		_ = tx.Rollback()
		return nil, errs.WrapCode(err, errs.Internal, "file asset transaction could not be initialized")
	}
	return tx, nil
}

// InsertTx inserts an asset using a caller-owned transaction. Tenant identity
// must come from the trusted caller; this function never commits the transaction.
func InsertTx(
	ctx context.Context,
	tx *sqldb.Tx,
	tenantID, id, objectKey, contentType string,
	sizeBytes int64,
	checksum, purpose, createdBy string,
) (*Asset, error) {
	if err := validateAssetInput(tenantID, id, objectKey, contentType, sizeBytes, checksum, purpose, createdBy); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "file asset storage requires an existing transaction"}
	}
	asset := &Asset{}
	err := tx.QueryRow(ctx, `
		INSERT INTO file_assets (id, tenant_id, object_key, content_type, size_bytes, checksum, purpose, created_by)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, tenant_id::text, object_key, content_type, size_bytes, checksum, purpose, created_by, created_at`,
		id, tenantID, objectKey, contentType, sizeBytes, checksum, purpose, createdBy,
	).Scan(
		&asset.ID, &asset.TenantID, &asset.ObjectKey, &asset.ContentType, &asset.SizeBytes,
		&asset.Checksum, &asset.Purpose, &asset.CreatedBy, &asset.CreatedAt,
	)
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "file asset could not be stored")
	}
	asset.CreatedAt = asset.CreatedAt.UTC()
	return asset, nil
}

// LoadTx loads an asset only within the caller-provided canonical tenant. It
// does not commit or otherwise change the caller-owned transaction.
func LoadTx(ctx context.Context, tx *sqldb.Tx, tenantID, id string) (*Asset, error) {
	normalizedTenantID, err := canonicalUUID(tenantID)
	if err != nil {
		return nil, invalidAssetInput("tenantId", "tenant ID must be a canonical non-nil UUID")
	}
	normalizedID, err := canonicalUUID(id)
	if err != nil {
		return nil, invalidAssetInput("id", "asset ID must be a canonical non-nil UUID")
	}
	if tx == nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "file asset storage requires an existing transaction"}
	}
	asset := &Asset{}
	err = tx.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, object_key, content_type, size_bytes, checksum, purpose, created_by, created_at
		FROM file_assets WHERE tenant_id=$1::uuid AND id=$2::uuid`, normalizedTenantID, normalizedID).Scan(
		&asset.ID, &asset.TenantID, &asset.ObjectKey, &asset.ContentType, &asset.SizeBytes,
		&asset.Checksum, &asset.Purpose, &asset.CreatedBy, &asset.CreatedAt,
	)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "file asset not found"}
	}
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "file asset could not be loaded")
	}
	asset.CreatedAt = asset.CreatedAt.UTC()
	return asset, nil
}

func canonicalUUID(value string) (string, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return "", errInvalidAssetID
	}
	return parsed.String(), nil
}

var errInvalidAssetID = &errs.Error{Code: errs.InvalidArgument, Message: "invalid file asset identity"}

func validateAssetInput(tenantID, id, objectKey, contentType string, sizeBytes int64, checksum, purpose, createdBy string) error {
	if _, err := canonicalUUID(tenantID); err != nil {
		return invalidAssetInput("tenantId", "tenant ID must be a canonical non-nil UUID")
	}
	if _, err := canonicalUUID(id); err != nil {
		return invalidAssetInput("id", "asset ID must be a canonical non-nil UUID")
	}
	if !utf8.ValidString(objectKey) || strings.TrimSpace(objectKey) == "" || utf8.RuneCountInString(objectKey) > 1024 {
		return invalidAssetInput("objectKey", "object key must be non-empty and at most 1024 characters")
	}
	switch contentType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return invalidAssetInput("contentType", "unsupported image content type")
	}
	if sizeBytes < 1 || sizeBytes > MaxTemplateImageBytes {
		return invalidAssetInput("sizeBytes", "file size is outside the supported range")
	}
	if len(checksum) != 64 {
		return invalidAssetInput("checksum", "checksum must be a lowercase SHA-256 value")
	}
	for _, r := range checksum {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return invalidAssetInput("checksum", "checksum must be a lowercase SHA-256 value")
		}
	}
	if purpose != templateImagePurpose {
		return invalidAssetInput("purpose", "purpose must be template-image")
	}
	if !utf8.ValidString(createdBy) || strings.TrimSpace(createdBy) == "" || utf8.RuneCountInString(createdBy) > 128 {
		return invalidAssetInput("createdBy", "created by must be non-empty and at most 128 characters")
	}
	for _, r := range createdBy {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return invalidAssetInput("createdBy", "created by contains invalid control characters")
		}
	}
	return nil
}

func invalidAssetInput(field, message string) error {
	return &errs.Error{Code: errs.InvalidArgument, Message: field + ": " + message}
}
