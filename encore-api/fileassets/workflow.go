package fileassets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"encore.app/internal/authz"
	"encore.app/internal/nativehttp"
	"encore.app/internal/tenantidentity"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

const (
	assetLeaseTTL           = time.Hour
	assetLeaseRefreshBefore = 5 * time.Minute
	assetReplayWindow       = 24 * time.Hour
	createAssetPath         = "/api/v2/file-assets"
)

// FileAsset is the public projection, deliberately excluding catalog internals.
type FileAsset struct {
	ID          string    `json:"id"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	Checksum    string    `json:"checksum"`
	DownloadURL string    `json:"downloadUrl"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type FileAssetResponse struct {
	Data FileAsset              `json:"data"`
	Meta nativehttp.RequestMeta `json:"meta"`
}

type assetPrincipal struct{ tenantID, subjectID string }

func currentAssetPrincipal(action string) (assetPrincipal, error) {
	return assetPrincipalFromData(authn.CurrentData(), action)
}

func assetPrincipalFromData(data *authn.Data, action string) (assetPrincipal, error) {
	rawTenant, err := tenantidentity.RequiredTenantID(data)
	if err != nil {
		return assetPrincipal{}, err
	}
	if !authz.Can(data, "template", action) {
		return assetPrincipal{}, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized for template assets"}
	}
	tenantID := tenantidentity.NormalizePrincipalID(rawTenant)
	if _, err := canonicalUUID(tenantID); err != nil {
		return assetPrincipal{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated tenant"}
	}
	subjectID := strings.TrimSpace(data.UserID)
	if !utf8.ValidString(subjectID) || subjectID == "" || len(subjectID) > 128 {
		return assetPrincipal{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated subject"}
	}
	for _, ch := range subjectID {
		if ch < 0x20 || ch >= 0x7f && ch <= 0x9f {
			return assetPrincipal{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated subject"}
		}
	}
	return assetPrincipal{tenantID, subjectID}, nil
}

func assetUnavailable() error {
	return &errs.Error{Code: errs.Unavailable, Message: "file asset service is unavailable"}
}

func (s *Service) requireStorage() error {
	if s == nil || s.objects == nil || s.box == nil {
		return assetUnavailable()
	}
	return nil
}

func assetAAD(kind, tenantID, assetID string, expiresAt time.Time) []byte {
	encoded, _ := json.Marshal([]string{"file-assets-v1", kind, tenantID, assetID, expiresAt.UTC().Format(time.RFC3339Nano)})
	return encoded
}

func receiptAAD(scope ReceiptScope, requestHash string, expiresAt time.Time) []byte {
	encoded, _ := json.Marshal([]string{"file-assets-v1", "create-receipt", scope.TenantID, scope.SubjectID,
		scope.Operation, scope.Path, scope.Key, requestHash, expiresAt.UTC().Format(time.RFC3339Nano)})
	return encoded
}

func databaseNow(ctx context.Context, tx *sqldb.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, assetUnavailable()
	}
	return now.UTC(), nil
}

// The asset row lock serializes lease renewal across replicas. The encrypted
// short lease stabilizes the complete representation, including its strong ETag.
func (s *Service) leaseAssetTx(ctx context.Context, tx *sqldb.Tx, asset *Asset) (*FileAsset, error) {
	now, err := databaseNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	lease, err := LoadURLLeaseTx(ctx, tx, asset.TenantID, asset.ID)
	if err != nil {
		return nil, assetUnavailable()
	}
	var downloadURL string
	if lease != nil && lease.ExpiresAt.After(now.Add(assetLeaseRefreshBefore)) {
		plaintext, openErr := s.box.Open(lease.Envelope, assetAAD("download-lease", asset.TenantID, asset.ID, lease.ExpiresAt))
		if openErr != nil {
			return nil, assetUnavailable()
		}
		downloadURL = string(plaintext)
	} else {
		// Set the reported expiry before signing, so it is never later than
		// the object's actual capability expiry due to SDK signing latency.
		expiresAt := now.Add(assetLeaseTTL).Truncate(time.Second)
		downloadURL, err = s.objects.SignedDownloadURL(ctx, asset.ObjectKey, assetLeaseTTL)
		if err != nil {
			return nil, assetUnavailable()
		}
		if !validDownloadURL(downloadURL) {
			return nil, assetUnavailable()
		}
		envelope, sealErr := s.box.Seal([]byte(downloadURL), assetAAD("download-lease", asset.TenantID, asset.ID, expiresAt))
		if sealErr != nil {
			return nil, assetUnavailable()
		}
		lease = &URLLease{Envelope: envelope, ExpiresAt: expiresAt}
		if err := SaveURLLeaseTx(ctx, tx, asset.TenantID, asset.ID, *lease); err != nil {
			return nil, assetUnavailable()
		}
	}
	if !validDownloadURL(downloadURL) {
		return nil, assetUnavailable()
	}
	return &FileAsset{ID: asset.ID, ContentType: asset.ContentType, SizeBytes: asset.SizeBytes,
		Checksum: asset.Checksum, DownloadURL: downloadURL, ExpiresAt: lease.ExpiresAt.UTC()}, nil
}

func validDownloadURL(value string) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 2048 {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.Fragment == ""
}

func (s *Service) getAsset(ctx context.Context, principal assetPrincipal, id string) (*FileAsset, error) {
	if _, err := canonicalUUID(id); err != nil {
		return nil, errInvalidAssetID
	}
	if err := s.requireStorage(); err != nil {
		return nil, err
	}
	tx, err := BeginTenantScopedTx(ctx, principal.tenantID)
	if err != nil {
		return nil, assetUnavailable()
	}
	defer tx.Rollback()
	if err := LockAssetTx(ctx, tx, principal.tenantID, id); err != nil {
		return nil, err
	}
	asset, err := LoadTx(ctx, tx, principal.tenantID, id)
	if err != nil {
		return nil, err
	}
	result, err := s.leaseAssetTx(ctx, tx, asset)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, assetUnavailable()
	}
	return result, nil
}

func validAssetIdempotencyKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:-", c)) {
			return false
		}
	}
	return true
}

func uploadRequestHash(meta ImageMetadata) string {
	// Multipart boundaries and filenames are transport details, not intent.
	b, _ := json.Marshal(struct {
		Purpose, ContentType, Checksum string
		SizeBytes                      int64
	}{templateImagePurpose, meta.ContentType, meta.Checksum, meta.SizeBytes})
	h := sha256.Sum256(append([]byte("POST\n"+createAssetPath+"\n"), b...))
	return hex.EncodeToString(h[:])
}

type assetCreateResult struct {
	body                []byte
	location, requestID string
	replayed            bool
}

func assetConflict(message string) error {
	return &errs.Error{Code: errs.AlreadyExists, Message: message}
}

func (s *Service) createAsset(ctx context.Context, principal assetPrincipal, key string, contents []byte, meta ImageMetadata, requestID string) (*assetCreateResult, error) {
	if !validAssetIdempotencyKey(key) {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid Idempotency-Key"}
	}
	// Validate trusted workflow inputs too, not only the raw HTTP parser.
	validated, actualMeta, err := ReadTemplateImageUpload(bytes.NewReader(contents), meta.ContentType)
	if err != nil || actualMeta != meta {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid file contents"}
	}
	contents = validated
	if err := s.requireStorage(); err != nil {
		return nil, err
	}
	scope := ReceiptScope{TenantID: principal.tenantID, SubjectID: principal.subjectID, Operation: "createFileAsset", Path: createAssetPath, Key: key}
	hash := uploadRequestHash(meta)
	tx, err := BeginTenantScopedTx(ctx, principal.tenantID)
	if err != nil {
		return nil, assetUnavailable()
	}
	defer tx.Rollback()
	if err := LockReceiptTx(ctx, tx, scope); err != nil {
		return nil, assetUnavailable()
	}
	now, err := databaseNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	receipt, err := LoadReceiptTx(ctx, tx, scope)
	if err != nil {
		return nil, assetUnavailable()
	}
	if receipt != nil {
		if !receipt.ExpiresAt.After(now) {
			return nil, assetConflict("idempotency key has expired")
		}
		if receipt.RequestHash != hash {
			return nil, assetConflict("idempotency key was already used for a different request")
		}
		body, openErr := s.box.Open(receipt.Response, receiptAAD(scope, hash, receipt.ExpiresAt))
		if openErr != nil {
			return nil, assetUnavailable()
		}
		var response FileAssetResponse
		if json.Unmarshal(body, &response) != nil || response.Meta.RequestID != receipt.RequestID || receipt.Location != createAssetPath+"/"+response.Data.ID {
			return nil, assetUnavailable()
		}
		return &assetCreateResult{body: body, location: receipt.Location, requestID: receipt.RequestID, replayed: true}, nil
	}
	id := uuid.NewString()
	objectKey := "templates/" + principal.tenantID + "/" + id
	// The durable intent survives a process crash between object upload and
	// database commit. Recovery checks the catalog before deleting any object.
	if err := CreateUploadIntent(ctx, principal.tenantID, id, objectKey); err != nil {
		return nil, assetUnavailable()
	}
	if err := LockUploadIntentTx(ctx, tx, principal.tenantID, id); err != nil {
		return nil, assetUnavailable()
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		_ = tx.Rollback()
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		// A failed cleanup leaves the persisted intent for the scheduled retry.
		_ = s.recoverUploadIntent(cleanupCtx, UploadIntent{ID: id, TenantID: principal.tenantID, ObjectKey: objectKey})
	}()
	if err := s.objects.Put(ctx, objectKey, contents, meta.ContentType); err != nil {
		return nil, assetUnavailable()
	}
	asset, err := InsertTx(ctx, tx, principal.tenantID, id, objectKey, meta.ContentType, meta.SizeBytes, meta.Checksum, templateImagePurpose, principal.subjectID)
	if err != nil {
		return nil, assetUnavailable()
	}
	responseAsset, err := s.leaseAssetTx(ctx, tx, asset)
	if err != nil {
		return nil, err
	}
	response := FileAssetResponse{Data: *responseAsset, Meta: nativehttp.RequestMeta{RequestID: requestID}}
	body, err := json.Marshal(response)
	if err != nil {
		return nil, assetUnavailable()
	}
	expiresAt := now.Add(assetReplayWindow).Truncate(time.Second)
	envelope, err := s.box.Seal(body, receiptAAD(scope, hash, expiresAt))
	if err != nil {
		return nil, assetUnavailable()
	}
	location := createAssetPath + "/" + id
	if err := InsertReceiptTx(ctx, tx, scope, Receipt{RequestHash: hash, Location: location, RequestID: requestID, Response: envelope, ExpiresAt: expiresAt}); err != nil {
		return nil, assetUnavailable()
	}
	if err := InsertAssetAuditTx(ctx, tx, principal.tenantID, id, principal.subjectID, requestID); err != nil {
		return nil, assetUnavailable()
	}
	if err := MarkUploadIntentTx(ctx, tx, principal.tenantID, id, "committed"); err != nil {
		return nil, assetUnavailable()
	}
	if err := tx.Commit(); err != nil {
		return nil, assetUnavailable()
	}
	completed = true
	return &assetCreateResult{body: body, location: location, requestID: requestID}, nil
}

func assetETag(asset *FileAsset) string {
	encoded, _ := json.Marshal(asset)
	return fmt.Sprintf(`"sha256-%x"`, sha256.Sum256(encoded))
}
