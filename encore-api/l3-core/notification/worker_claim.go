package notification

import (
	"context"
	"time"
)

type fairClaimCandidate struct {
	tenantID, deliveryID, instanceID string
	configVersion                    int64
	expiresAt                        time.Time
}

// ClaimDeliveriesFair returns at most one earliest-due delivery per tenant,
// starting after afterTenant and wrapping around. The cursor is owned by the
// worker process; database row locks still coordinate all worker replicas.
func (s *Store) ClaimDeliveriesFair(ctx context.Context, limit int, leaseFor time.Duration, afterTenant string) ([]DeliveryClaim, string, error) {
	if limit < 1 || limit > 100 || leaseFor <= 0 || leaseFor > 5*time.Minute {
		return nil, afterTenant, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return nil, afterTenant, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, afterTenant, classifyStoreError(err)
	}
	defer tx.Rollback()
	now := s.now()
	rows, err := tx.QueryContext(ctx, `WITH first_per_tenant AS MATERIALIZED (
		SELECT DISTINCT ON (d.tenant_id) d.tenant_id,d.id,d.instance_id,d.config_version,d.expires_at,d.next_attempt_at,d.created_at
		FROM notification_deliveries d
		JOIN notification_instances i ON i.tenant_id=d.tenant_id AND i.id=d.instance_id
		JOIN notification_instance_configs c ON c.tenant_id=d.tenant_id AND c.instance_id=d.instance_id AND c.config_version=d.config_version
		JOIN notification_plugin_grants g ON g.tenant_id=d.tenant_id AND g.registration_id=c.plugin_registration_id AND g.enabled=TRUE
		JOIN notification_plugin_registrations p ON p.id=c.plugin_registration_id AND p.enabled=TRUE
		WHERE d.dispatch_status='queued' AND d.next_attempt_at<=$1 AND d.expires_at>$1 AND i.enabled=TRUE AND c.enabled=TRUE
		AND NOT EXISTS (SELECT 1 FROM notification_deliveries active WHERE active.tenant_id=d.tenant_id AND active.instance_id=d.instance_id AND active.dispatch_status='sending')
		ORDER BY d.tenant_id,d.next_attempt_at,d.created_at,d.id
	)
	SELECT c.tenant_id,c.id,c.instance_id,c.config_version,c.expires_at
	FROM first_per_tenant c
	JOIN notification_deliveries d ON d.tenant_id=c.tenant_id AND d.id=c.id
	JOIN notification_instances i ON i.tenant_id=c.tenant_id AND i.id=c.instance_id
	WHERE NOT EXISTS (SELECT 1 FROM notification_deliveries active WHERE active.tenant_id=c.tenant_id AND active.instance_id=c.instance_id AND active.dispatch_status='sending')
	ORDER BY (c.tenant_id <= $3),c.tenant_id,c.next_attempt_at,c.created_at,c.id
	LIMIT $2 FOR UPDATE OF d,i SKIP LOCKED`, now, limit, afterTenant)
	if err != nil {
		return nil, afterTenant, classifyStoreError(err)
	}
	var candidates []fairClaimCandidate
	for rows.Next() {
		var candidate fairClaimCandidate
		if err := rows.Scan(&candidate.tenantID, &candidate.deliveryID, &candidate.instanceID, &candidate.configVersion, &candidate.expiresAt); err != nil {
			rows.Close()
			return nil, afterTenant, classifyStoreError(err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, afterTenant, classifyStoreError(err)
	}
	rows.Close()
	claims := make([]DeliveryClaim, 0, len(candidates))
	nextTenant := afterTenant
	for _, candidate := range candidates {
		claim, err := s.claimOneTx(ctx, tx, claimCandidate{tenantID: candidate.tenantID, deliveryID: candidate.deliveryID, instanceID: candidate.instanceID, configVersion: candidate.configVersion, expiresAt: candidate.expiresAt}, now, leaseFor)
		if err != nil {
			return nil, afterTenant, err
		}
		claims = append(claims, claim)
		nextTenant = candidate.tenantID
	}
	if err := tx.Commit(); err != nil {
		return nil, afterTenant, classifyStoreError(err)
	}
	return claims, nextTenant, nil
}

// RecoverExpiredClaimsBounded converts a bounded batch of expired in-flight
// attempts to unknown. An expired lease is never evidence that no send occurred.
func (s *Store) RecoverExpiredClaimsBounded(ctx context.Context, now time.Time, limit int) (int, error) {
	if now.IsZero() || limit < 1 || limit > 1000 {
		return 0, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, classifyStoreError(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT tenant_id,id,current_attempt_id FROM notification_deliveries WHERE dispatch_status='sending' AND lease_expires_at<=$1 ORDER BY lease_expires_at,tenant_id,id FOR UPDATE SKIP LOCKED LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return 0, classifyStoreError(err)
	}
	type expiredClaim struct{ tenant, delivery, attempt string }
	var expired []expiredClaim
	for rows.Next() {
		var item expiredClaim
		if err := rows.Scan(&item.tenant, &item.delivery, &item.attempt); err != nil {
			rows.Close()
			return 0, classifyStoreError(err)
		}
		expired = append(expired, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, classifyStoreError(err)
	}
	rows.Close()
	for _, item := range expired {
		if _, err := tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET status='unknown',finished_at=$4 WHERE tenant_id=$1 AND delivery_id=$2 AND attempt_id=$3 AND status='sending'`, item.tenant, item.delivery, item.attempt, now.UTC()); err != nil {
			return 0, classifyStoreError(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET dispatch_status='unknown',delivery_status='unknown',current_attempt_id=NULL,current_lease_hash=NULL,lease_expires_at=NULL,updated_at=$3 WHERE tenant_id=$1 AND id=$2`, item.tenant, item.delivery, now.UTC()); err != nil {
			return 0, classifyStoreError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, classifyStoreError(err)
	}
	return len(expired), nil
}
