package thingmodel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
	"encore.dev/storage/sqldb"
)

// receiptLockIdentity is shared by mutation and observation. The lock spans the
// owner transaction only, never a remote request. A query waits for an earlier
// transaction before observing a missing row, including commit-reply loss.
func receiptLockIdentity(tenant core.TenantID, subject, operation, key string) string {
	encoded, _ := json.Marshal(struct {
		Tenant    core.TenantID `json:"tenant"`
		Subject   string        `json:"subject"`
		Operation string        `json:"operation"`
		Key       string        `json:"key"`
	}{tenant, subject, operation, key})
	return string(encoded)
}

func (postgresRepository) QueryReceipt(ctx context.Context, q app.ReceiptQuery) (app.ReceiptObservation, error) {
	// Validate at the storage boundary too: direct callers cannot select arbitrary
	// operations or use invalid tenant/subject identifiers.
	if !validUUID(string(q.TenantID)) || !validUUID(q.SubjectID) || len(q.Key) < 8 || len(q.Key) > 128 {
		return app.ReceiptObservation{}, app.ErrInvalid
	}
	if _, err := app.ReceiptCapability(q.Operation); err != nil {
		return app.ReceiptObservation{}, err
	}
	if q.ExpectedPayloadHash != "" && !validReceiptScope(app.ReceiptScope{TenantID: q.TenantID, SubjectID: q.SubjectID, Operation: q.Operation, Key: q.Key, PayloadHash: q.ExpectedPayloadHash}) {
		return app.ReceiptObservation{}, app.ErrInvalid
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return app.ReceiptObservation{}, persistenceError(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, receiptLockIdentity(q.TenantID, q.SubjectID, q.Operation, q.Key)); err != nil {
		return app.ReceiptObservation{}, persistenceError(err)
	}
	var hash string
	var resultJSON []byte
	var expiresAt sql.NullTime
	err = tx.QueryRow(ctx, `SELECT payload_hash,result,expires_at FROM thing_model_command_receipts
  WHERE tenant_id=$1 AND subject_id=$2 AND operation=$3 AND idempotency_key=$4`, string(q.TenantID), q.SubjectID, q.Operation, q.Key).Scan(&hash, &resultJSON, &expiresAt)
	observation := app.ReceiptObservation{Operation: q.Operation, Key: q.Key}
	switch {
	case errors.Is(err, sqldb.ErrNoRows):
		observation.State = app.ReceiptNotObserved
	case err != nil:
		return app.ReceiptObservation{}, persistenceError(err)
	default:
		if q.ExpectedPayloadHash != "" && !strings.EqualFold(q.ExpectedPayloadHash, hash) {
			return app.ReceiptObservation{}, app.ErrConflict
		}
		observation.PayloadHash = hash
		if expiresAt.Valid {
			until := expiresAt.Time
			observation.RetainedUntil = &until
		}
		if !expiresAt.Valid || !expiresAt.Time.After(time.Now().UTC()) || resultJSON == nil {
			observation.State = app.ReceiptExpired
		} else {
			var result app.Result
			if err := json.Unmarshal(resultJSON, &result); err != nil {
				return app.ReceiptObservation{}, persistenceError(err)
			}
			if result.ModelID == "" || result.Status == "" {
				return app.ReceiptObservation{}, persistenceError(fmt.Errorf("incomplete persisted command receipt"))
			}
			result.Replayed = true
			observation.State = app.ReceiptConfirmed
			observation.Result = &result
		}
	}
	// Read transaction commit failures also remain unknown to the caller.
	if err := tx.Commit(); err != nil {
		return app.ReceiptObservation{}, persistenceError(err)
	}
	return observation, nil
}
