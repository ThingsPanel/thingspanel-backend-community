package notification

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// CreateSourceProjectionCredentialPair is used only by the local operator
// provisioner. Both purposes are created atomically for exactly one shared
// deployment and tenant allowlist; an unexpired credential in that scope
// blocks an accidental second issuance.
func (s *Store) CreateSourceProjectionCredentialPair(ctx context.Context, source, projection NotificationCredentialRecord) error {
	now := s.now()
	if source.Purpose != CredentialPurposeSource || projection.Purpose != CredentialPurposeProjection ||
		source.TenantID != "" || projection.TenantID != "" || source.InstanceID != "" || projection.InstanceID != "" ||
		source.DeploymentID == "" || source.DeploymentID != projection.DeploymentID ||
		!sameTenantAllowlist(source.AllowedTenantIDs, projection.AllowedTenantIDs) ||
		!source.ExpiresAt.Equal(projection.ExpiresAt) || source.ID == projection.ID || source.TokenHash == projection.TokenHash {
		return storeErr(StoreErrorInvalid)
	}
	if err := validateNotificationCredential(source, now); err != nil {
		return err
	}
	if err := validateNotificationCredential(projection, now); err != nil {
		return err
	}
	db, err := s.database()
	if err != nil {
		return err
	}
	allowed, err := canonicalAllowedTenants(source.AllowedTenantIDs)
	if err != nil || len(allowed) != 1 {
		return storeErr(StoreErrorInvalid)
	}
	allowedJSON, err := json.Marshal(allowed)
	if err != nil {
		return storeErr(StoreErrorInvalid)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return classifyStoreError(err)
	}
	defer tx.Rollback()
	// Serialize concurrent operator invocations for this exact scope, even
	// when no credential row exists yet.
	lockKey := "notification-source-credential-pair:" + source.DeploymentID + ":" + allowed[0]
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return classifyStoreError(err)
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM notification_credentials
		WHERE deployment_id=$1 AND allowed_tenant_ids=$2::jsonb
		  AND purpose IN ($3,$4) AND revoked_at IS NULL AND expires_at>$5
	)`, source.DeploymentID, string(allowedJSON), CredentialPurposeSource, CredentialPurposeProjection, now).Scan(&active)
	if err != nil {
		return classifyStoreError(err)
	}
	if active {
		return storeErr(StoreErrorConflict)
	}
	for _, record := range []NotificationCredentialRecord{source, projection} {
		_, err = tx.ExecContext(ctx, `INSERT INTO notification_credentials(id,purpose,token_hash,tenant_id,deployment_id,allowed_tenant_ids,instance_id,expires_at,revoked_at,rotated_from) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''),$8,$9,NULLIF($10,''))`,
			record.ID, record.Purpose, strings.ToLower(record.TokenHash), record.TenantID, record.DeploymentID, string(allowedJSON), record.InstanceID, record.ExpiresAt, record.RevokedAt, record.RotatedFrom)
		if err != nil {
			return classifyStoreError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return classifyStoreError(err)
	}
	return nil
}

// RevokeSourceProjectionCredentialPair rolls back a just-issued pair when a
// private operator output cannot be delivered. It only revokes the exact
// source/projection rows together and leaves unrelated credentials untouched.
func (s *Store) RevokeSourceProjectionCredentialPair(ctx context.Context, sourceID, projectionID string) error {
	if sourceID == "" || projectionID == "" || sourceID == projectionID {
		return storeErr(StoreErrorInvalid)
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
	rows, err := tx.QueryContext(ctx, `SELECT id,purpose FROM notification_credentials WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`, sourceID, projectionID)
	if err != nil {
		return classifyStoreError(err)
	}
	var sourceCount, projectionCount int
	for rows.Next() {
		var id, purpose string
		if err := rows.Scan(&id, &purpose); err != nil {
			_ = rows.Close()
			return classifyStoreError(err)
		}
		if id == sourceID && purpose == string(CredentialPurposeSource) {
			sourceCount++
		}
		if id == projectionID && purpose == string(CredentialPurposeProjection) {
			projectionCount++
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return classifyStoreError(err)
	}
	_ = rows.Close()
	if sourceCount != 1 || projectionCount != 1 {
		return storeErr(StoreErrorNotFound)
	}
	now := s.now()
	if _, err := tx.ExecContext(ctx, `UPDATE notification_credentials SET revoked_at=$3 WHERE (id=$1 AND purpose='source') OR (id=$2 AND purpose='projection')`, sourceID, projectionID, now); err != nil {
		return classifyStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return classifyStoreError(err)
	}
	return nil
}

func sameTenantAllowlist(a, b []string) bool {
	aa, errA := canonicalAllowedTenants(a)
	bb, errB := canonicalAllowedTenants(b)
	if errA != nil || errB != nil || len(aa) != len(bb) || len(aa) != 1 {
		return false
	}
	return aa[0] == bb[0]
}

// RenewSourceProjectionCredentialPair updates exactly two active hashes in one
// transaction. Scope, purpose and revocation cannot change during renewal.
func (s *Store) RenewSourceProjectionCredentialPair(ctx context.Context, deployment string, tenants []string, sourceHash, projectionHash string, expires time.Time) error {
	now := s.now()
	allowed, err := canonicalAllowedTenants(tenants)
	if err != nil || len(allowed) != 1 || deployment == "" || len(sourceHash) != 64 || len(projectionHash) != 64 || sourceHash == projectionHash || !expires.After(now) || expires.After(now.Add(8*time.Hour)) {
		return storeErr(StoreErrorInvalid)
	}
	raw, _ := json.Marshal(allowed)
	db, err := s.database()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return classifyStoreError(err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE notification_credentials SET expires_at=$5
 WHERE deployment_id=$1 AND allowed_tenant_ids=$2::jsonb AND tenant_id IS NULL AND instance_id IS NULL
 AND revoked_at IS NULL AND expires_at>$6
 AND ((purpose='source' AND token_hash=$3) OR (purpose='projection' AND token_hash=$4))`, deployment, string(raw), sourceHash, projectionHash, expires, now)
	if err != nil {
		return classifyStoreError(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return classifyStoreError(err)
	}
	if count != 2 {
		return storeErr(StoreErrorNotFound)
	}
	if err := tx.Commit(); err != nil {
		return classifyStoreError(err)
	}
	return nil
}
