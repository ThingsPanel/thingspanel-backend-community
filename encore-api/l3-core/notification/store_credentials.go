package notification

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

func (s *Store) CreateNotificationCredential(ctx context.Context, record NotificationCredentialRecord) error {
	if err := validateNotificationCredential(record, s.now()); err != nil {
		return err
	}
	db, err := s.database()
	if err != nil {
		return err
	}
	allowed, err := canonicalAllowedTenants(record.AllowedTenantIDs)
	if err != nil {
		return err
	}
	allowedJSON, _ := json.Marshal(allowed)
	_, err = db.ExecContext(ctx, `INSERT INTO notification_credentials(id,purpose,token_hash,tenant_id,deployment_id,allowed_tenant_ids,instance_id,expires_at,revoked_at,rotated_from) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''),$8,$9,NULLIF($10,''))`, record.ID, record.Purpose, record.TokenHash, record.TenantID, record.DeploymentID, string(allowedJSON), record.InstanceID, record.ExpiresAt, record.RevokedAt, record.RotatedFrom)
	return classifyStoreError(err)
}

func (s *Store) FindNotificationCredential(ctx context.Context, purpose CredentialPurpose, tokenHash string) (NotificationCredentialRecord, error) {
	if !validCredentialPurpose(purpose) || !validTokenHash(tokenHash) {
		return NotificationCredentialRecord{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return NotificationCredentialRecord{}, err
	}
	var record NotificationCredentialRecord
	var allowedJSON []byte
	var revoked sql.NullTime
	err = db.QueryRowContext(ctx, `SELECT id,purpose,token_hash,COALESCE(tenant_id,''),COALESCE(deployment_id,''),allowed_tenant_ids,COALESCE(instance_id,''),expires_at,revoked_at,COALESCE(rotated_from,'') FROM notification_credentials WHERE purpose=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at>$3`, purpose, tokenHash, s.now()).Scan(&record.ID, &record.Purpose, &record.TokenHash, &record.TenantID, &record.DeploymentID, &allowedJSON, &record.InstanceID, &record.ExpiresAt, &revoked, &record.RotatedFrom)
	if err != nil {
		return NotificationCredentialRecord{}, classifyStoreError(err)
	}
	if revoked.Valid {
		record.RevokedAt = &revoked.Time
	}
	if json.Unmarshal(allowedJSON, &record.AllowedTenantIDs) != nil {
		return NotificationCredentialRecord{}, storeErr(StoreErrorUnavailable)
	}
	if record.AllowedTenantIDs == nil {
		record.AllowedTenantIDs = []string{}
	}
	return record, nil
}

func (s *Store) RevokeNotificationCredential(ctx context.Context, purpose CredentialPurpose, id string) error {
	if !validCredentialPurpose(purpose) || id == "" {
		return storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `UPDATE notification_credentials SET revoked_at=$3 WHERE purpose=$1 AND id=$2 AND revoked_at IS NULL`, purpose, id, s.now())
	if err != nil {
		return classifyStoreError(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return storeErr(StoreErrorUnavailable)
	}
	if rows > 0 {
		return nil
	}
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_credentials WHERE purpose=$1 AND id=$2)`, purpose, id).Scan(&exists); err != nil {
		return classifyStoreError(err)
	}
	if !exists {
		return storeErr(StoreErrorNotFound)
	}
	return nil
}

func (s *Store) RotateNotificationCredential(ctx context.Context, oldPurpose CredentialPurpose, oldID string, next NotificationCredentialRecord) error {
	if !validCredentialPurpose(oldPurpose) || oldID == "" || next.RotatedFrom != oldID || next.Purpose != oldPurpose {
		return storeErr(StoreErrorInvalid)
	}
	if err := validateNotificationCredential(next, s.now()); err != nil {
		return err
	}
	db, err := s.database()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return classifyStoreError(err)
	}
	defer tx.Rollback()
	old, err := readCredentialByIDTx(ctx, tx, oldPurpose, oldID, true)
	if err != nil {
		return err
	}
	if old.RevokedAt != nil || !old.ExpiresAt.After(s.now()) {
		return storeErr(StoreErrorConflict)
	}
	if !sameCredentialScope(old, next) {
		return storeErr(StoreErrorInvalid)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_credentials SET revoked_at=$3 WHERE purpose=$1 AND id=$2 AND revoked_at IS NULL`, oldPurpose, oldID, s.now()); err != nil {
		return classifyStoreError(err)
	}
	allowed, _ := canonicalAllowedTenants(next.AllowedTenantIDs)
	allowedJSON, _ := json.Marshal(allowed)
	_, err = tx.ExecContext(ctx, `INSERT INTO notification_credentials(id,purpose,token_hash,tenant_id,deployment_id,allowed_tenant_ids,instance_id,expires_at,revoked_at,rotated_from) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''),$8,$9,$10)`, next.ID, next.Purpose, next.TokenHash, next.TenantID, next.DeploymentID, string(allowedJSON), next.InstanceID, next.ExpiresAt, next.RevokedAt, oldID)
	if err != nil {
		return classifyStoreError(err)
	}
	return classifyStoreError(tx.Commit())
}

func readCredentialByIDTx(ctx context.Context, tx *sql.Tx, purpose CredentialPurpose, id string, lock bool) (NotificationCredentialRecord, error) {
	query := `SELECT id,purpose,token_hash,COALESCE(tenant_id,''),COALESCE(deployment_id,''),allowed_tenant_ids,COALESCE(instance_id,''),expires_at,revoked_at,COALESCE(rotated_from,'') FROM notification_credentials WHERE purpose=$1 AND id=$2`
	if lock {
		query += " FOR UPDATE"
	}
	var r NotificationCredentialRecord
	var allowedJSON []byte
	var revoked sql.NullTime
	if err := tx.QueryRowContext(ctx, query, purpose, id).Scan(&r.ID, &r.Purpose, &r.TokenHash, &r.TenantID, &r.DeploymentID, &allowedJSON, &r.InstanceID, &r.ExpiresAt, &revoked, &r.RotatedFrom); err != nil {
		return NotificationCredentialRecord{}, classifyStoreError(err)
	}
	if revoked.Valid {
		t := revoked.Time
		r.RevokedAt = &t
	}
	if json.Unmarshal(allowedJSON, &r.AllowedTenantIDs) != nil {
		return NotificationCredentialRecord{}, storeErr(StoreErrorUnavailable)
	}
	if r.AllowedTenantIDs == nil {
		r.AllowedTenantIDs = []string{}
	}
	return r, nil
}

func validateNotificationCredential(r NotificationCredentialRecord, now time.Time) error {
	if r.ID == "" || !validCredentialPurpose(r.Purpose) || !validTokenHash(r.TokenHash) || !r.ExpiresAt.After(now) || r.RevokedAt != nil {
		return storeErr(StoreErrorInvalid)
	}
	allowed, err := canonicalAllowedTenants(r.AllowedTenantIDs)
	if err != nil {
		return err
	}
	switch r.Purpose {
	case CredentialPurposeReceipt:
		if r.TenantID == "" || r.InstanceID == "" || r.DeploymentID != "" || len(allowed) != 0 {
			return storeErr(StoreErrorInvalid)
		}
	case CredentialPurposeSource, CredentialPurposeProjection:
		if r.TenantID != "" || r.InstanceID != "" || r.DeploymentID == "" || len(allowed) == 0 {
			return storeErr(StoreErrorInvalid)
		}
	}
	return nil
}

func sameCredentialScope(a, b NotificationCredentialRecord) bool {
	aa, _ := canonicalAllowedTenants(a.AllowedTenantIDs)
	ba, _ := canonicalAllowedTenants(b.AllowedTenantIDs)
	if a.Purpose != b.Purpose || a.TenantID != b.TenantID || a.DeploymentID != b.DeploymentID || a.InstanceID != b.InstanceID || len(aa) != len(ba) {
		return false
	}
	for i := range aa {
		if aa[i] != ba[i] {
			return false
		}
	}
	return true
}
func canonicalAllowedTenants(values []string) ([]string, error) {
	out := append([]string{}, values...)
	for _, value := range out {
		if value == "" || value != trimSpace(value) {
			return nil, storeErr(StoreErrorInvalid)
		}
	}
	sort.Strings(out)
	for i := 1; i < len(out); i++ {
		if out[i] == out[i-1] {
			return nil, storeErr(StoreErrorInvalid)
		}
	}
	return out, nil
}
func validCredentialPurpose(purpose CredentialPurpose) bool {
	return purpose == CredentialPurposeSource || purpose == CredentialPurposeProjection || purpose == CredentialPurposeReceipt
}
func validTokenHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func trimSpace(value string) string { return strings.TrimSpace(value) }
