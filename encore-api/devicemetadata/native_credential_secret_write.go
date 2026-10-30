package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

const credentialSecretReplayWindow = 10 * time.Minute

// runCredentialSecretWriteTx shares metadata_idempotency and audit ownership
// with ordinary writes, but never persists plaintext requests or responses.
// authorize runs inside the transaction on both first writes and replays.
func runCredentialSecretWriteTx(ctx context.Context, command metadataWriteCommand, owner CredentialAAD, protector *CredentialSecretProtector, authorize func(*sqldb.Tx) error, apply func(*sqldb.Tx, CredentialAAD, time.Time) (metadataWriteResponse, metadataWriteAudit, error)) (*metadataWriteResponse, error) {
	if protector == nil {
		return nil, credentialProtectionUnavailable()
	}
	if command.TenantID != owner.TenantID || !validCredentialUUID(command.TenantID) || !validCredentialUUID(owner.OwnerID) || !validAccessTemplateText(command.SubjectID, 1, 128, true) {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated credential identity"}
	}
	if authorize == nil || apply == nil || command.Method != "POST" || command.IfMatch != "" || !validIdempotencyKey(command.Key) || !validAccessTemplateText(command.Operation, 1, 128, true) || !strings.HasPrefix(command.Path, "/api/v2/") || len(command.Path) > 1024 || strings.ContainsAny(command.Path, "?#\r\n") || !validAccessTemplateText(command.RequestID, 1, 128, true) || len(command.CanonicalBody) > 64<<10 || !json.Valid(command.CanonicalBody) {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid credential write command"}
	}
	owner.CredentialID = uuid.NewString()
	owner.Use = CredentialUseMaterial
	owner.ReplayBinding = ""
	if validateCredentialAAD(owner) != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid credential owner"}
	}
	scope, _ := json.Marshal([]string{command.TenantID, command.SubjectID, command.Operation, command.Path, command.Key})
	scopeHash := sha256.Sum256(scope)
	replayAAD := owner
	replayAAD.Use = CredentialUseReplayResponse
	replayAAD.ReplayBinding = hex.EncodeToString(scopeHash[:])
	tx, err := beginTenantScopedSQLTx(ctx, command.TenantID, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(scope)); err != nil {
		return nil, err
	}
	if err := authorize(tx); err != nil {
		return nil, err
	}
	args := []any{command.TenantID, command.SubjectID, command.Operation, command.Path, command.Key}
	var storedHash, state string
	var credentialID, requestKey, responseKey, suite sql.NullString
	var nonce, ciphertext []byte
	var expired bool
	err = tx.QueryRow(ctx, `SELECT request_hash,state,credential_id::text,request_key_id,response_key_id,response_cipher_suite,response_nonce,response_ciphertext,replay_until<=clock_timestamp()
		FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5 FOR UPDATE`, args...).Scan(&storedHash, &state, &credentialID, &requestKey, &responseKey, &suite, &nonce, &ciphertext, &expired)
	if err == nil {
		if state == "expired" || expired {
			if _, err := tx.Exec(ctx, `UPDATE metadata_idempotency SET state='expired',http_status=409,response=NULL,response_hash=NULL,response_key_id=NULL,response_cipher_suite=NULL,response_nonce=NULL,response_ciphertext=NULL,updated_at=NOW()
				WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, args...); err != nil {
				return nil, err
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return nil, idempotencyConflict("idempotency key has expired")
		}
		if !credentialID.Valid || !requestKey.Valid || state != "completed" || !responseKey.Valid || !suite.Valid {
			return nil, credentialProtectionUnavailable()
		}
		replayAAD.CredentialID = credentialID.String
		digest, decodeErr := hex.DecodeString(storedHash)
		if decodeErr != nil {
			return nil, credentialProtectionUnavailable()
		}
		matches, verifyErr := VerifyCredentialFingerprint(replayAAD, command.CanonicalBody, CredentialFingerprint{KeyID: requestKey.String, Fingerprint: digest}, protector.keys)
		if verifyErr != nil {
			return nil, credentialProtectionUnavailable()
		}
		if !matches {
			return nil, idempotencyConflict("idempotency key was already used for a different request")
		}
		var usableID string
		err := tx.QueryRow(ctx, `SELECT id::text FROM device_credentials WHERE tenant_id=$1 AND id=$2 AND credential_owner_kind=$3 AND credential_owner_id=$4 AND purpose=$5 AND status<>'revoked' AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE`, command.TenantID, credentialID.String, string(owner.OwnerKind), owner.OwnerID, string(owner.Purpose)).Scan(&usableID)
		if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, idempotencyConflict("credential is no longer available for response replay")
		}
		if err != nil {
			return nil, err
		}
		plaintext, openErr := protector.Open(replayAAD, ProtectedCredential{KeyID: responseKey.String, CipherSuite: suite.String, Nonce: nonce, Ciphertext: ciphertext})
		var result metadataWriteResponse
		if openErr != nil || json.Unmarshal(plaintext, &result) != nil || result.Status < 200 || result.Status > 299 || !json.Valid(result.Body) {
			return nil, credentialProtectionUnavailable()
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		result.Replayed = true
		return &result, nil
	}
	if !errors.Is(err, sqldb.ErrNoRows) && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&createdAt); err != nil {
		return nil, err
	}
	expiresAt := createdAt.UTC().Add(credentialSecretReplayWindow)
	result, audit, err := apply(tx, owner, expiresAt)
	if err != nil {
		return nil, err
	}
	if result.Status < 200 || result.Status > 299 || !json.Valid(result.Body) || len(result.Body) > 250<<10 || audit.ResourceKind != "device_credential" || audit.ResourceID != owner.CredentialID || !json.Valid(audit.RedactedChanges) {
		return nil, &errs.Error{Code: errs.Internal, Message: "invalid credential mutation result"}
	}
	result.RequestID = command.RequestID
	result.Replayed = false
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, credentialProtectionUnavailable()
	}
	protected, err := protector.Seal(replayAAD, encoded)
	if err != nil {
		return nil, credentialProtectionUnavailable()
	}
	requestDigest, err := protector.Fingerprint(replayAAD, command.CanonicalBody)
	if err != nil {
		return nil, credentialProtectionUnavailable()
	}
	insertArgs := append(args, hex.EncodeToString(requestDigest.Fingerprint), requestDigest.KeyID, owner.CredentialID, result.Status, protected.KeyID, protected.CipherSuite, protected.Nonce, protected.Ciphertext, expiresAt)
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_idempotency(tenant_id,subject_id,operation_name,canonical_path,idempotency_key,request_hash,request_key_id,credential_id,state,http_status,response_key_id,response_cipher_suite,response_nonce,response_ciphertext,replay_until)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,'completed',$9,$10,$11,$12,$13,$14)`, insertArgs...); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_audit_events(id,tenant_id,created_by,request_id,operation_name,resource_kind,resource_id,before_version,after_version,outcome,redacted_changes)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'succeeded',$10::jsonb)`, uuid.NewString(), command.TenantID, command.SubjectID, command.RequestID, command.Operation, audit.ResourceKind, audit.ResourceID, audit.BeforeVersion, audit.AfterVersion, []byte(audit.RedactedChanges)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &result, nil
}

func credentialProtectionUnavailable() error {
	return &errs.Error{Code: errs.Unavailable, Message: "credential protection is unavailable"}
}
