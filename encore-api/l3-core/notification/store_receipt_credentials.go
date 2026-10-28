package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const receiptCredentialRotationOperation = "receipt-credential-rotation"

// RotateReceiptCredential atomically stores a new hash-only receipt credential
// and its encrypted one-time response snapshot. An empty oldID creates the
// first credential. The same idempotency key replays the original response;
// reusing it with a different digest conflicts without changing credentials.
func (s *Store) RotateReceiptCredential(ctx context.Context, tenantID, idempotencyKey, requestDigest, oldID string, next NotificationCredentialRecord, responseSnapshot json.RawMessage) (json.RawMessage, bool, error) {
	if tenantID == "" || len(idempotencyKey) < 8 || len(idempotencyKey) > 128 || !validTokenHash(requestDigest) || next.Purpose != CredentialPurposeReceipt || next.TenantID != tenantID || next.InstanceID == "" || (oldID != "" && next.RotatedFrom != oldID) || (oldID == "" && next.RotatedFrom != "") || !json.Valid(responseSnapshot) {
		return nil, false, storeErr(StoreErrorInvalid)
	}
	now := s.now().UTC()
	if err := validateNotificationCredential(next, now); err != nil {
		return nil, false, err
	}
	db, err := s.database()
	if err != nil {
		return nil, false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, classifyStoreError(err)
	}
	defer tx.Rollback()
	// PostgreSQL text values cannot contain NUL bytes. A delimiter collision
	// only causes harmless extra serialization, so a printable key is enough.
	// Serialize all rotations for the instance, including different keys. A
	// key-specific lock would let simultaneous first rotations create two
	// full-lifetime active credentials without placing either into grace.
	lockKey := tenantID + ":" + next.InstanceID + ":" + receiptCredentialRotationOperation
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1),hashtext($2))`, lockKey[:min(len(lockKey), 128)], lockKey[min(len(lockKey), 128):]); err != nil {
		return nil, false, classifyStoreError(err)
	}
	var existingDigest, keyRef string
	var ciphertext []byte
	var aadVersion int
	err = tx.QueryRowContext(ctx, `SELECT request_digest,response_key_ref,response_ciphertext,response_aad_version FROM notification_credential_replays WHERE tenant_id=$1 AND instance_id=$2 AND operation=$3 AND idempotency_key=$4`, tenantID, next.InstanceID, receiptCredentialRotationOperation, idempotencyKey).Scan(&existingDigest, &keyRef, &ciphertext, &aadVersion)
	if err == nil {
		if existingDigest != requestDigest {
			return nil, false, storeErr(StoreErrorConflict)
		}
		plain, openErr := s.open(ctx, receiptCredentialReplayAAD(tenantID, next.InstanceID, idempotencyKey), ProtectedSnapshot{KeyRef: keyRef, Ciphertext: ciphertext, AADVersion: aadVersion})
		if openErr != nil || !json.Valid(plain) {
			return nil, false, storeErr(StoreErrorUnavailable)
		}
		if err := tx.Commit(); err != nil {
			return nil, false, classifyStoreError(err)
		}
		return json.RawMessage(plain), true, nil
	}
	if err != sql.ErrNoRows {
		return nil, false, classifyStoreError(err)
	}
	// Lock every currently active credential for this instance. Previous
	// grace-period tokens may still be active; every rotation shortens them
	// when needed, while the newest credential becomes the new lineage parent.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM notification_credentials WHERE purpose='receipt' AND tenant_id=$1 AND instance_id=$2 AND revoked_at IS NULL AND expires_at>$3 ORDER BY created_at DESC,id DESC FOR UPDATE`, tenantID, next.InstanceID, now)
	if err != nil {
		return nil, false, classifyStoreError(err)
	}
	var activeIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, false, classifyStoreError(err)
		}
		activeIDs = append(activeIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, false, classifyStoreError(err)
	}
	rows.Close()
	effectiveOldID := oldID
	if effectiveOldID == "" && len(activeIDs) > 0 {
		effectiveOldID = activeIDs[0]
	}
	if oldID != "" {
		found := false
		for _, id := range activeIDs {
			if id == oldID {
				found = true
				break
			}
		}
		if !found {
			return nil, false, storeErr(StoreErrorConflict)
		}
		old, err := readCredentialByIDTx(ctx, tx, CredentialPurposeReceipt, oldID, false)
		if err != nil {
			return nil, false, err
		}
		if old.TenantID != tenantID || old.InstanceID != next.InstanceID || !sameCredentialScope(old, next) {
			return nil, false, storeErr(StoreErrorInvalid)
		}
	}
	if effectiveOldID != "" {
		next.RotatedFrom = effectiveOldID
		graceEnd := now.Add(15 * time.Minute)
		if _, err := tx.ExecContext(ctx, `UPDATE notification_credentials SET expires_at=LEAST(expires_at,$4) WHERE purpose='receipt' AND tenant_id=$1 AND instance_id=$2 AND revoked_at IS NULL AND expires_at>$3`, tenantID, next.InstanceID, now, graceEnd); err != nil {
			return nil, false, classifyStoreError(err)
		}
	}
	protected, err := s.seal(ctx, receiptCredentialReplayAAD(tenantID, next.InstanceID, idempotencyKey), responseSnapshot)
	if err != nil {
		return nil, false, storeErr(StoreErrorUnavailable)
	}
	allowedJSON, err := json.Marshal([]string{})
	if err != nil {
		return nil, false, storeErr(StoreErrorUnavailable)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_credentials(id,purpose,token_hash,tenant_id,instance_id,allowed_tenant_ids,expires_at,rotated_from) VALUES($1,'receipt',$2,$3,$4,$5,$6,NULLIF($7,''))`, next.ID, next.TokenHash, tenantID, next.InstanceID, string(allowedJSON), next.ExpiresAt, next.RotatedFrom); err != nil {
		return nil, false, classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_credential_replays(tenant_id,instance_id,operation,idempotency_key,request_digest,credential_id,response_key_ref,response_ciphertext,response_aad_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenantID, next.InstanceID, receiptCredentialRotationOperation, idempotencyKey, requestDigest, next.ID, protected.KeyRef, protected.Ciphertext, protected.AADVersion); err != nil {
		return nil, false, classifyStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, false, classifyStoreError(err)
	}
	return append(json.RawMessage(nil), responseSnapshot...), false, nil
}

func (s *Store) RevokeReceiptCredential(ctx context.Context, tenantID, instanceID, credentialID string) error {
	if tenantID == "" || instanceID == "" || credentialID == "" {
		return storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `UPDATE notification_credentials SET revoked_at=$4 WHERE purpose='receipt' AND tenant_id=$1 AND instance_id=$2 AND id=$3 AND revoked_at IS NULL`, tenantID, instanceID, credentialID, s.now().UTC())
	if err != nil {
		return classifyStoreError(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return storeErr(StoreErrorUnavailable)
	}
	if changed > 0 {
		return nil
	}
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_credentials WHERE purpose='receipt' AND tenant_id=$1 AND instance_id=$2 AND id=$3)`, tenantID, instanceID, credentialID).Scan(&exists); err != nil {
		return classifyStoreError(err)
	}
	if !exists {
		return storeErr(StoreErrorNotFound)
	}
	return nil
}

func receiptCredentialReplayAAD(tenantID, instanceID, idempotencyKey string) SnapshotAAD {
	return SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: instanceID, Version: 1, Purpose: receiptCredentialRotationOperation, Slot: idempotencyKey}
}
