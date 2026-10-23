package notification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"encore.app/l3-core/notification/contract"
	"github.com/google/uuid"
)

func (s *Store) FinishDelivery(ctx context.Context, finish DeliveryFinish) error {
	if finish.TenantID == "" || finish.DeliveryID == "" || finish.AttemptID == "" || finish.LeaseToken == "" || finish.Result.DeliveryID != finish.DeliveryID || finish.FinishedAt.IsZero() {
		return storeErr(StoreErrorInvalid)
	}
	if finish.Result.Outcome != "accepted" && finish.Result.Outcome != "rejected" && finish.Result.Outcome != "unknown" {
		return storeErr(StoreErrorInvalid)
	}
	if finish.Result.ProviderMessageID != nil && (len(*finish.Result.ProviderMessageID) > 256 || strings.TrimSpace(*finish.Result.ProviderMessageID) != *finish.Result.ProviderMessageID) {
		return storeErr(StoreErrorInvalid)
	}
	if finish.Result.Outcome == "rejected" && (finish.Result.Error == nil || finish.Result.Error.Code == "") {
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
	sum := sha256.Sum256([]byte(finish.LeaseToken))
	leaseHash := hex.EncodeToString(sum[:])
	var currentAttempt, currentHash string
	var attemptCount int
	var expires time.Time
	var instanceID string
	err = tx.QueryRowContext(ctx, `SELECT current_attempt_id,current_lease_hash,attempt_count,expires_at,instance_id FROM notification_deliveries WHERE tenant_id=$1 AND id=$2 AND dispatch_status='sending' FOR UPDATE`, finish.TenantID, finish.DeliveryID).Scan(&currentAttempt, &currentHash, &attemptCount, &expires, &instanceID)
	if err != nil {
		if err == sql.ErrNoRows {
			var exists bool
			if checkErr := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_deliveries WHERE tenant_id=$1 AND id=$2)`, finish.TenantID, finish.DeliveryID).Scan(&exists); checkErr != nil {
				return classifyStoreError(checkErr)
			}
			if exists {
				return storeErr(StoreErrorStaleClaim)
			}
		}
		return classifyStoreError(err)
	}
	if currentAttempt != finish.AttemptID || currentHash != leaseHash {
		return storeErr(StoreErrorStaleClaim)
	}
	var receiptMatched bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2 AND delivery_id=$3 AND processing_status='matched')`, finish.TenantID, instanceID, finish.DeliveryID).Scan(&receiptMatched); err != nil {
		return classifyStoreError(err)
	}
	var provider any
	if finish.Result.ProviderMessageID != nil {
		provider = *finish.Result.ProviderMessageID
	}
	now := finish.FinishedAt.UTC()
	nextStatus := "unknown"
	deliveryStatus := "unknown"
	attemptStatus := finish.Result.Outcome
	var retryAt any
	if receiptMatched {
		nextStatus = "accepted"
		deliveryStatus = "pending"
	}
	if !receiptMatched {
		switch finish.Result.Outcome {
		case "accepted":
			nextStatus = "accepted"
			if finish.Result.ReceiptExpected {
				deliveryStatus = "pending"
			} else {
				deliveryStatus = "unsupported"
			}
		case "rejected":
			var next time.Time
			if expires.After(now) && attemptCount < maxDeliveryAttempts {
				if scheduled, ok := s.retry.NextRetry(finish.Result, attemptCount, now, expires); ok {
					if scheduled.After(now) && scheduled.Before(expires) {
						next = scheduled
					}
				}
			}
			if !next.IsZero() {
				nextStatus = "queued"
				deliveryStatus = "pending"
				retryAt = next
			} else {
				nextStatus = "failed"
				deliveryStatus = "failed"
			}
		case "unknown":
			nextStatus = "unknown"
			deliveryStatus = "unknown"
		}
	}
	var outcome any
	if finish.Result.Error != nil {
		outcome = safeProviderCode(finish.Result.Error.Code)
	}
	_, err = tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET status=$4,finished_at=$5,outcome_code=$6,provider_message_id=$7,retry_at=$8 WHERE tenant_id=$1 AND delivery_id=$2 AND attempt_id=$3 AND status='sending'`, finish.TenantID, finish.DeliveryID, finish.AttemptID, attemptStatus, now, outcome, provider, retryAt)
	if err != nil {
		return classifyStoreError(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE notification_deliveries SET dispatch_status=$3,delivery_status=$4,provider_message_id=COALESCE($5,provider_message_id),error_code=$6,next_attempt_at=$7,current_attempt_id=NULL,current_lease_hash=NULL,lease_expires_at=NULL,updated_at=$8 WHERE tenant_id=$1 AND id=$2`, finish.TenantID, finish.DeliveryID, nextStatus, deliveryStatus, provider, outcome, retryAt, now)
	if err != nil {
		return classifyStoreError(err)
	}
	if provider != nil {
		if err := s.matchPendingReceiptsTx(ctx, tx, finish.TenantID, instanceID, *finish.Result.ProviderMessageID, now); err != nil {
			return err
		}
	}
	return classifyStoreError(tx.Commit())
}

func (s *Store) AcceptReceipt(ctx context.Context, principal ReceiptPrincipal, event contract.PluginReceiptV1, eventDigest string) (contract.ReceiptAck, error) {
	if principal.TenantID == "" || principal.InstanceID == "" || event.EventID == "" || event.ProviderMessageID == "" || len(event.ProviderMessageID) > 256 || strings.TrimSpace(event.ProviderMessageID) != event.ProviderMessageID || len(eventDigest) != 64 {
		return contract.ReceiptAck{}, storeErr(StoreErrorInvalid)
	}
	if event.DeliveryID != nil && (*event.DeliveryID == "" || len(*event.DeliveryID) > 128 || strings.TrimSpace(*event.DeliveryID) != *event.DeliveryID) {
		return contract.ReceiptAck{}, storeErr(StoreErrorInvalid)
	}
	if _, err := hex.DecodeString(eventDigest); err != nil {
		return contract.ReceiptAck{}, storeErr(StoreErrorInvalid)
	}
	occurred, err := time.Parse(time.RFC3339, event.OccurredAt)
	if err != nil {
		return contract.ReceiptAck{}, storeErr(StoreErrorInvalid)
	}
	if event.Status != "delivered" && event.Status != "failed" && event.Status != "unknown" {
		return contract.ReceiptAck{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.ReceiptAck{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.ReceiptAck{}, classifyStoreError(err)
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_instances WHERE tenant_id=$1 AND id=$2)`, principal.TenantID, principal.InstanceID).Scan(&exists); err != nil {
		return contract.ReceiptAck{}, classifyStoreError(err)
	}
	if !exists {
		return contract.ReceiptAck{}, storeErr(StoreErrorNotFound)
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return contract.ReceiptAck{}, storeErr(StoreErrorInvalid)
	}
	protected, err := s.seal(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: principal.TenantID, ResourceID: principal.InstanceID, Version: 1, Purpose: "receipt-event", Slot: event.EventID}, eventJSON)
	if err != nil {
		return contract.ReceiptAck{}, err
	}
	var candidate pendingReceiptCandidate
	var storedDigest string
	err = tx.QueryRowContext(ctx, `SELECT event_digest,tenant_id,instance_id,event_id,provider_message_id,requested_delivery_id,receipt_status,occurred_at FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3 FOR UPDATE`, principal.TenantID, principal.InstanceID, event.EventID).Scan(&storedDigest, &candidate.tenantID, &candidate.instanceID, &candidate.eventID, &candidate.providerID, &candidate.requestedDeliveryID, &candidate.status, &candidate.occurredAt)
	if err != nil && err != sql.ErrNoRows {
		return contract.ReceiptAck{}, classifyStoreError(err)
	}
	if err == sql.ErrNoRows {
		// A new direct-delivery receipt is validated under the delivery lock
		// before it becomes durable. This lock precedes the inbox insert, matching
		// FinishDelivery and recovery's delivery -> inbox order.
		if event.DeliveryID != nil {
			var knownProvider sql.NullString
			err := tx.QueryRowContext(ctx, `SELECT provider_message_id FROM notification_deliveries WHERE tenant_id=$1 AND instance_id=$2 AND id=$3 FOR UPDATE`, principal.TenantID, principal.InstanceID, *event.DeliveryID).Scan(&knownProvider)
			if err == sql.ErrNoRows {
				return contract.ReceiptAck{}, storeErr(StoreErrorNotFound)
			}
			if err != nil {
				return contract.ReceiptAck{}, classifyStoreError(err)
			}
			if knownProvider.Valid && knownProvider.String != event.ProviderMessageID {
				return contract.ReceiptAck{}, storeErr(StoreErrorConflict)
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO notification_receipt_inbox(tenant_id,instance_id,event_id,event_digest,key_ref,ciphertext,aad_version,processing_status,provider_message_id,requested_delivery_id,receipt_status,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,'unmatched',$8,NULLIF($9,''),$10,$11) ON CONFLICT DO NOTHING`, principal.TenantID, principal.InstanceID, event.EventID, eventDigest, protected.KeyRef, protected.Ciphertext, protected.AADVersion, event.ProviderMessageID, valueOrEmpty(event.DeliveryID), event.Status, occurred.UTC())
		if err != nil {
			return contract.ReceiptAck{}, classifyStoreError(err)
		}
		err = tx.QueryRowContext(ctx, `SELECT event_digest,tenant_id,instance_id,event_id,provider_message_id,requested_delivery_id,receipt_status,occurred_at FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3 FOR UPDATE`, principal.TenantID, principal.InstanceID, event.EventID).Scan(&storedDigest, &candidate.tenantID, &candidate.instanceID, &candidate.eventID, &candidate.providerID, &candidate.requestedDeliveryID, &candidate.status, &candidate.occurredAt)
		if err != nil {
			return contract.ReceiptAck{}, classifyStoreError(err)
		}
	}
	if storedDigest != eventDigest {
		_, err = tx.ExecContext(ctx, `INSERT INTO notification_receipt_conflicts(id,tenant_id,instance_id,event_id,stored_digest,incoming_digest) VALUES($1,$2,$3,$4,$5,$6)`, uuid.NewString(), principal.TenantID, principal.InstanceID, event.EventID, storedDigest, eventDigest)
		if err != nil {
			return contract.ReceiptAck{}, classifyStoreError(err)
		}
		if err := tx.Commit(); err != nil {
			return contract.ReceiptAck{}, classifyStoreError(err)
		}
		return contract.ReceiptAck{}, storeErr(StoreErrorConflict)
	}
	if err := tx.Commit(); err != nil {
		return contract.ReceiptAck{}, classifyStoreError(err)
	}
	// The immutable inbox event is durable now. Matching is a separate repairable
	// transaction: transient SQL failure must not turn a durable ACK into a NACK.
	_, _ = s.reconcileReceipt(ctx, candidate)
	return contract.ReceiptAck{Accepted: true}, nil
}

func (s *Store) matchPendingReceiptsTx(ctx context.Context, tx *sql.Tx, tenantID, instanceID, providerID string, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT event_id,receipt_status,occurred_at,COALESCE(requested_delivery_id,'') FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2 AND provider_message_id=$3 AND processing_status IN ('pending','unmatched') ORDER BY received_at FOR UPDATE`, tenantID, instanceID, providerID)
	if err != nil {
		return classifyStoreError(err)
	}
	type receipt struct {
		id, status, requestedDeliveryID string
		at                              time.Time
	}
	var list []receipt
	for rows.Next() {
		var r receipt
		if err := rows.Scan(&r.id, &r.status, &r.at, &r.requestedDeliveryID); err != nil {
			rows.Close()
			return classifyStoreError(err)
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return classifyStoreError(err)
	}
	rows.Close()
	for _, r := range list {
		if err := s.matchOneReceiptTx(ctx, tx, tenantID, instanceID, r.id, providerID, r.requestedDeliveryID, r.status, r.at); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) matchOneReceiptTx(ctx context.Context, tx *sql.Tx, tenantID, instanceID, eventID, providerID, requestedDeliveryID, status string, occurred time.Time) error {
	var count int
	var deliveryID string
	var err error
	if requestedDeliveryID != "" {
		var knownProvider sql.NullString
		var attempts int
		err := tx.QueryRowContext(ctx, `SELECT provider_message_id,attempt_count FROM notification_deliveries WHERE tenant_id=$1 AND instance_id=$2 AND id=$3 FOR UPDATE`, tenantID, instanceID, requestedDeliveryID).Scan(&knownProvider, &attempts)
		if err == sql.ErrNoRows || err == nil && attempts == 0 {
			_, err = tx.ExecContext(ctx, `UPDATE notification_receipt_inbox SET processing_status='unmatched' WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3 AND processing_status<>'matched'`, tenantID, instanceID, eventID)
			return classifyStoreError(err)
		}
		if err != nil {
			return classifyStoreError(err)
		}
		if knownProvider.Valid && knownProvider.String != providerID {
			_, err = tx.ExecContext(ctx, `UPDATE notification_receipt_inbox SET processing_status='conflict' WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3`, tenantID, instanceID, eventID)
			return classifyStoreError(err)
		}
		if !knownProvider.Valid {
			if _, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET provider_message_id=$4 WHERE tenant_id=$1 AND instance_id=$2 AND id=$3`, tenantID, instanceID, requestedDeliveryID, providerID); err != nil {
				return classifyStoreError(err)
			}
		}
		count, deliveryID = 1, requestedDeliveryID
	} else {
		err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(min(id),'') FROM notification_deliveries WHERE tenant_id=$1 AND instance_id=$2 AND provider_message_id=$3 AND attempt_count>0`, tenantID, instanceID, providerID).Scan(&count, &deliveryID)
		if err != nil {
			return classifyStoreError(err)
		}
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE notification_receipt_inbox SET processing_status='unmatched' WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3 AND processing_status<>'matched'`, tenantID, instanceID, eventID)
		return classifyStoreError(err)
	}
	if count != 1 {
		_, err = tx.ExecContext(ctx, `UPDATE notification_receipt_inbox SET processing_status='conflict' WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3`, tenantID, instanceID, eventID)
		return classifyStoreError(err)
	}
	var dispatch, currentDeliveryStatus string
	var attempt string
	err = tx.QueryRowContext(ctx, `SELECT dispatch_status,delivery_status,COALESCE(current_attempt_id,'') FROM notification_deliveries WHERE tenant_id=$1 AND instance_id=$2 AND id=$3 FOR UPDATE`, tenantID, instanceID, deliveryID).Scan(&dispatch, &currentDeliveryStatus, &attempt)
	if err != nil {
		return classifyStoreError(err)
	}
	if (currentDeliveryStatus == "delivered" || currentDeliveryStatus == "failed") && currentDeliveryStatus != status {
		if _, err := tx.ExecContext(ctx, `UPDATE notification_receipt_inbox SET processing_status='conflict' WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3`, tenantID, instanceID, eventID); err != nil {
			return classifyStoreError(err)
		}
		return nil
	}
	var deliveryStatus string
	if status == "delivered" {
		deliveryStatus = "delivered"
	} else if status == "failed" {
		deliveryStatus = "failed"
	} else {
		deliveryStatus = "unknown"
	}
	if attempt != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET status='superseded',finished_at=$4,outcome_code='receipt' WHERE tenant_id=$1 AND delivery_id=$2 AND attempt_id=$3 AND status='sending'`, tenantID, deliveryID, attempt, occurred); err != nil {
			return classifyStoreError(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET dispatch_status='accepted',delivery_status=$4,current_attempt_id=NULL,current_lease_hash=NULL,lease_expires_at=NULL,updated_at=$5 WHERE tenant_id=$1 AND instance_id=$2 AND id=$3`, tenantID, instanceID, deliveryID, deliveryStatus, occurred); err != nil {
		return classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_receipt_inbox SET processing_status='matched',delivery_id=$4 WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3`, tenantID, instanceID, eventID, deliveryID); err != nil {
		return classifyStoreError(err)
	}
	return nil
}

func safeProviderCode(code string) string {
	if len(code) == 0 || len(code) > 64 {
		return "provider_error"
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' && r != '.' {
			return "provider_error"
		}
	}
	return code
}

var _ = sql.ErrNoRows

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
