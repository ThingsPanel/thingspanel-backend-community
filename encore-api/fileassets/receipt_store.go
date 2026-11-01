package fileassets

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// ReceiptScope is the trusted identity tuple for a successful idempotent write.
type ReceiptScope struct {
	TenantID  string
	SubjectID string
	Operation string
	Path      string
	Key       string
}

// Receipt stores only encrypted response bytes; expired rows remain present so
// an idempotency key cannot be reused after its replay window.
type Receipt struct {
	RequestHash string
	Location    string
	RequestID   string
	Response    Envelope
	ExpiresAt   time.Time
}

// LockReceiptTx serializes receipt checks and inserts for the exact canonical
// scope until the caller's transaction ends.
func LockReceiptTx(ctx context.Context, tx *sqldb.Tx, scope ReceiptScope) error {
	if err := validateReceiptTxInput(tx, scope); err != nil {
		return err
	}
	encodedScope, err := json.Marshal(scope)
	if err != nil {
		return invalidReceiptScope()
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(encodedScope)); err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset receipt scope could not be locked")
	}
	return nil
}

// LoadReceiptTx loads both active and expired receipts. The caller decides
// whether the stored response remains replayable; expired keys stay reserved.
func LoadReceiptTx(ctx context.Context, tx *sqldb.Tx, scope ReceiptScope) (*Receipt, error) {
	if err := validateReceiptTxInput(tx, scope); err != nil {
		return nil, err
	}
	var raw []byte
	receipt := &Receipt{}
	err := tx.QueryRow(ctx, `
		SELECT request_hash, location, request_id, encrypted_response, expires_at
		FROM file_asset_write_receipts
		WHERE tenant_id=$1::uuid AND subject_id=$2 AND operation=$3 AND canonical_path=$4 AND idempotency_key=$5`,
		scope.TenantID, scope.SubjectID, scope.Operation, scope.Path, scope.Key,
	).Scan(&receipt.RequestHash, &receipt.Location, &receipt.RequestID, &raw, &receipt.ExpiresAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "file asset receipt could not be loaded")
	}
	if err := json.Unmarshal(raw, &receipt.Response); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "stored file asset receipt is invalid")
	}
	if !validReceipt(*receipt) {
		return nil, &errs.Error{Code: errs.Internal, Message: "stored file asset receipt is invalid"}
	}
	receipt.ExpiresAt = receipt.ExpiresAt.UTC()
	return receipt, nil
}

// InsertReceiptTx records a successful response inside the caller's existing
// write transaction. It never creates an in-flight receipt or commits.
func InsertReceiptTx(ctx context.Context, tx *sqldb.Tx, scope ReceiptScope, receipt Receipt) error {
	if err := validateReceiptTxInput(tx, scope); err != nil {
		return err
	}
	if !validReceipt(receipt) {
		return invalidReceiptValue()
	}
	encoded, err := json.Marshal(receipt.Response)
	if err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset receipt could not be encoded")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO file_asset_write_receipts
			(id, tenant_id, subject_id, operation, canonical_path, idempotency_key,
			 request_hash, location, request_id, encrypted_response, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11)`,
		uuid.NewString(), scope.TenantID, scope.SubjectID, scope.Operation, scope.Path, scope.Key,
		receipt.RequestHash, receipt.Location, receipt.RequestID, string(encoded), receipt.ExpiresAt.UTC(),
	)
	if err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset receipt could not be stored")
	}
	return nil
}

// InsertAssetAuditTx appends an asset-created event using content metadata
// read from the tenant-scoped asset row, never caller-supplied copies.
func InsertAssetAuditTx(ctx context.Context, tx *sqldb.Tx, tenantID, assetID, actor, requestID string) error {
	normalizedTenantID, normalizedAssetID, err := validateTenantAssetTxInput(tx, tenantID, assetID)
	if err != nil {
		return err
	}
	if !validOpaqueIdentifier(actor, 128) || !validOpaqueIdentifier(requestID, 128) {
		return invalidReceiptValue()
	}
	var contentType, checksum string
	var sizeBytes int64
	err = tx.QueryRow(ctx, `
		SELECT content_type, size_bytes, checksum FROM file_assets
		WHERE tenant_id=$1::uuid AND id=$2::uuid`, normalizedTenantID, normalizedAssetID,
	).Scan(&contentType, &sizeBytes, &checksum)
	if errors.Is(err, sqldb.ErrNoRows) {
		return &errs.Error{Code: errs.NotFound, Message: "file asset not found"}
	}
	if err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset audit source could not be loaded")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO file_asset_audit_events
			(id, tenant_id, asset_id, actor, operation, request_id, content_type, size_bytes, checksum)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'file_asset.created', $5, $6, $7, $8)`,
		uuid.NewString(), normalizedTenantID, normalizedAssetID, actor, requestID, contentType, sizeBytes, checksum,
	)
	if err != nil {
		return errs.WrapCode(err, errs.Internal, "file asset audit event could not be stored")
	}
	return nil
}

func validateReceiptTxInput(tx *sqldb.Tx, scope ReceiptScope) error {
	if tx == nil {
		return invalidStoreTransaction()
	}
	tenantID, err := canonicalUUID(scope.TenantID)
	if err != nil || tenantID != scope.TenantID {
		return invalidAssetInput("tenantId", "tenant ID must be a canonical non-nil UUID")
	}
	if !validOpaqueIdentifier(scope.SubjectID, 128) || !validOpaqueIdentifier(scope.Operation, 128) ||
		!validCanonicalPath(scope.Path) || !validIdempotencyKey(scope.Key) {
		return invalidReceiptScope()
	}
	return nil
}

func validReceipt(receipt Receipt) bool {
	return isLowerHexSHA256(receipt.RequestHash) && validLocation(receipt.Location) &&
		validOpaqueIdentifier(receipt.RequestID, 128) && !receipt.ExpiresAt.IsZero() && validStoredEnvelope(receipt.Response)
}

func validOpaqueIdentifier(value string, maxBytes int) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || len(value) > maxBytes {
		return false
	}
	for _, r := range value {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return false
		}
	}
	return true
}

func validCanonicalPath(value string) bool {
	return utf8.ValidString(value) && strings.HasPrefix(value, "/") && len(value) <= 2048 &&
		!strings.ContainsAny(value, "?#") && validOpaqueIdentifier(value, 2048)
}

func validLocation(value string) bool {
	return validOpaqueIdentifier(value, 2048)
}

func validIdempotencyKey(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("._:-", rune(c))) {
			return false
		}
	}
	return true
}

func isLowerHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !((value[i] >= '0' && value[i] <= '9') || (value[i] >= 'a' && value[i] <= 'f')) {
			return false
		}
	}
	return true
}

func invalidReceiptScope() error {
	return &errs.Error{Code: errs.InvalidArgument, Message: "file asset receipt scope is invalid"}
}

func invalidReceiptValue() error {
	return &errs.Error{Code: errs.InvalidArgument, Message: "file asset receipt or audit input is invalid"}
}
