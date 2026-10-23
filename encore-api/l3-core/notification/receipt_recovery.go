package notification

import (
	"context"
	"database/sql"
	"time"
)

type pendingReceiptCandidate struct {
	tenantID, instanceID, eventID string
	providerID                    string
	requestedDeliveryID           sql.NullString
	status                        string
	occurredAt                    time.Time
}

// ReconcileReceipts rechecks a bounded batch of durable pending/unmatched
// receipts that now have at least one attempted delivery candidate. Each event
// uses a short transaction and locks delivery before inbox to match Finish's
// lock order. A locked inbox row is skipped to avoid inversion with receipt ACK.
func (s *Store) ReconcileReceipts(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT r.tenant_id,r.instance_id,r.event_id,r.provider_message_id,r.requested_delivery_id,r.occurred_at
		FROM notification_receipt_inbox r
		WHERE r.processing_status IN ('pending','unmatched')
		AND ((r.requested_delivery_id IS NOT NULL AND EXISTS (
			SELECT 1 FROM notification_deliveries d WHERE d.tenant_id=r.tenant_id AND d.instance_id=r.instance_id AND d.id=r.requested_delivery_id AND d.attempt_count>0
		)) OR (r.requested_delivery_id IS NULL AND EXISTS (
			SELECT 1 FROM notification_deliveries d WHERE d.tenant_id=r.tenant_id AND d.instance_id=r.instance_id AND d.provider_message_id=r.provider_message_id AND d.attempt_count>0
		)))
		ORDER BY r.received_at,r.tenant_id,r.instance_id,r.event_id LIMIT $1`, limit)
	if err != nil {
		return 0, classifyStoreError(err)
	}
	var candidates []pendingReceiptCandidate
	for rows.Next() {
		var candidate pendingReceiptCandidate
		if err := rows.Scan(&candidate.tenantID, &candidate.instanceID, &candidate.eventID, &candidate.providerID, &candidate.requestedDeliveryID, &candidate.occurredAt); err != nil {
			rows.Close()
			return 0, classifyStoreError(err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, classifyStoreError(err)
	}
	rows.Close()
	processed := 0
	for _, candidate := range candidates {
		matched, err := s.reconcileReceipt(ctx, candidate)
		if err != nil {
			return processed, err
		}
		if matched {
			processed++
		}
	}
	return processed, nil
}

func (s *Store) reconcileReceipt(ctx context.Context, candidate pendingReceiptCandidate) (bool, error) {
	db, err := s.database()
	if err != nil {
		return false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, classifyStoreError(err)
	}
	defer tx.Rollback()
	if candidate.requestedDeliveryID.Valid {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM notification_deliveries WHERE tenant_id=$1 AND instance_id=$2 AND id=$3 AND attempt_count>0 FOR UPDATE SKIP LOCKED`, candidate.tenantID, candidate.instanceID, candidate.requestedDeliveryID.String).Scan(&id)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, classifyStoreError(err)
		}
	} else {
		var count int
		var deliveryID string
		if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(min(id),'') FROM notification_deliveries WHERE tenant_id=$1 AND instance_id=$2 AND provider_message_id=$3 AND attempt_count>0`, candidate.tenantID, candidate.instanceID, candidate.providerID).Scan(&count, &deliveryID); err != nil {
			return false, classifyStoreError(err)
		}
		if count == 0 {
			return false, nil
		}
		if count == 1 {
			var lockedID string
			err := tx.QueryRowContext(ctx, `SELECT id FROM notification_deliveries WHERE tenant_id=$1 AND instance_id=$2 AND id=$3 FOR UPDATE SKIP LOCKED`, candidate.tenantID, candidate.instanceID, deliveryID).Scan(&lockedID)
			if err == sql.ErrNoRows {
				return false, nil
			}
			if err != nil {
				return false, classifyStoreError(err)
			}
		}
	}
	var stored pendingReceiptCandidate
	err = tx.QueryRowContext(ctx, `SELECT tenant_id,instance_id,event_id,provider_message_id,requested_delivery_id,receipt_status,occurred_at FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3 AND processing_status IN ('pending','unmatched') FOR UPDATE SKIP LOCKED`, candidate.tenantID, candidate.instanceID, candidate.eventID).Scan(&stored.tenantID, &stored.instanceID, &stored.eventID, &stored.providerID, &stored.requestedDeliveryID, &stored.status, &stored.occurredAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, classifyStoreError(err)
	}
	if err := s.matchOneReceiptTx(ctx, tx, stored.tenantID, stored.instanceID, stored.eventID, stored.providerID, valueOrEmptyNullString(stored.requestedDeliveryID), stored.status, stored.occurredAt.UTC()); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, classifyStoreError(err)
	}
	return true, nil
}

func valueOrEmptyNullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
