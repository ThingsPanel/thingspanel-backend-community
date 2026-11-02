package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

// Runtime receipts (DM-RT-007). The runtime owner is the only party that can
// say a desired device change took effect, so this is the single place where a
// pending outbox row and its metadata operation leave "pending". Received and
// stale receipts never complete anything; only a trusted applied receipt does,
// and a failed receipt only ends the operation when the runtime says the
// failure is not retryable.

const (
	runtimeReceiptReceived = "received"
	runtimeReceiptApplied  = "applied"
	runtimeReceiptFailed   = "failed"
	runtimeReceiptStale    = "stale"

	deviceRuntimeOutboxApplied = "applied"
	deviceRuntimeOutboxFailed  = "failed"
)

// runtimeReceipt mirrors RuntimeConfigurationReceipt. EventID is the outbox row
// id. Tenant and device come from the authenticated transport, never from a
// body the caller can widen.
type runtimeReceipt struct {
	EventID       string
	TenantID      string
	DeviceID      string
	Generation    int64
	ContentDigest string
	State         string
	FailureCode   string
	Retryable     bool
	RequestID     string
}

type runtimeReceiptOutcome struct {
	OutboxState    string
	OperationState string
	// Replayed is true when the receipt changed nothing because the same
	// outcome had already been recorded.
	Replayed bool
}

// runtimeDesiredDigest is the SHA-256 of the desired document with object keys
// in sorted order. Go marshals map keys sorted, which matches RFC 8785 for the
// string, boolean and integer values the producers emit; the cross-language
// golden vectors the contract asks for are still pending.
func runtimeDesiredDigest(desired []byte) (string, error) {
	var decoded any
	if err := json.Unmarshal(desired, &decoded); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func validateRuntimeReceipt(receipt runtimeReceipt) error {
	invalid := func(message string) error {
		return &errs.Error{Code: errs.InvalidArgument, Message: message}
	}
	if receipt.EventID == "" || receipt.TenantID == "" || receipt.DeviceID == "" {
		return invalid("receipt must identify event, tenant and device")
	}
	if receipt.Generation < 1 {
		return invalid("receipt generation must be positive")
	}
	if len(receipt.ContentDigest) != 64 || strings.Trim(receipt.ContentDigest, "0123456789abcdef") != "" {
		return invalid("receipt contentDigest must be 64 lowercase hex characters")
	}
	switch receipt.State {
	case runtimeReceiptReceived, runtimeReceiptApplied, runtimeReceiptStale:
		if receipt.FailureCode != "" {
			return invalid("only a failed receipt may carry a failure code")
		}
	case runtimeReceiptFailed:
		if n := len(strings.TrimSpace(receipt.FailureCode)); n < 1 || n > 128 {
			return invalid("a failed receipt requires a failureCode of 1 to 128 characters")
		}
	default:
		return invalid("unknown receipt state")
	}
	return nil
}

// recordRuntimeReceiptTx applies one receipt inside the caller's transaction.
// The outbox row is locked first, then its operation, so concurrent receipts
// for the same event serialize.
func recordRuntimeReceiptTx(ctx context.Context, tx *sqldb.Tx, receipt runtimeReceipt) (runtimeReceiptOutcome, error) {
	if err := validateRuntimeReceipt(receipt); err != nil {
		return runtimeReceiptOutcome{}, err
	}
	var (
		operationID, state, kind string
		generation               int64
		desired                  []byte
	)
	err := tx.QueryRow(ctx, `SELECT operation_id::text,state,generation,desired,kind
		FROM device_runtime_outbox
		WHERE tenant_id=$1 AND id=$2 AND device_id=$3
		FOR UPDATE`, receipt.TenantID, receipt.EventID, receipt.DeviceID).
		Scan(&operationID, &state, &generation, &desired, &kind)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
		return runtimeReceiptOutcome{}, &errs.Error{Code: errs.NotFound, Message: "runtime event not found"}
	}
	if err != nil {
		return runtimeReceiptOutcome{}, wrapDBErr(err, "read runtime event failed")
	}
	digest, err := runtimeDesiredDigest(desired)
	if err != nil {
		return runtimeReceiptOutcome{}, &errs.Error{Code: errs.Internal, Message: "failed to digest runtime event"}
	}
	if generation != receipt.Generation || digest != receipt.ContentDigest {
		return runtimeReceiptOutcome{}, &errs.Error{Code: errs.FailedPrecondition,
			Message: "receipt generation or content digest does not match the event"}
	}
	var operationState string
	if err := tx.QueryRow(ctx, `SELECT state FROM metadata_operations WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
		receipt.TenantID, operationID).Scan(&operationState); err != nil {
		return runtimeReceiptOutcome{}, wrapDBErr(err, "read operation failed")
	}
	outcome := runtimeReceiptOutcome{OutboxState: state, OperationState: operationState}

	switch receipt.State {
	case runtimeReceiptReceived:
		// Durable receipt only: counts the attempt, completes nothing.
		if state == deviceRuntimeOutboxPending {
			if _, err := tx.Exec(ctx, `UPDATE device_runtime_outbox SET attempts=attempts+1,updated_by='runtime'
				WHERE tenant_id=$1 AND id=$2`, receipt.TenantID, receipt.EventID); err != nil {
				return outcome, wrapDBErr(err, "record receipt failed")
			}
		}
		outcome.Replayed = state != deviceRuntimeOutboxPending
		return outcome, nil
	case runtimeReceiptStale:
		outcome.Replayed = true
		return outcome, nil
	case runtimeReceiptApplied:
		if state == deviceRuntimeOutboxApplied {
			outcome.Replayed = true
			return outcome, nil
		}
		if operationState == "failed" || operationState == "cancelled" {
			return outcome, &errs.Error{Code: errs.FailedPrecondition, Message: "operation already ended without being applied"}
		}
		if _, err := tx.Exec(ctx, `UPDATE device_runtime_outbox
			SET state='applied',applied_at=NOW(),last_error=NULL,attempts=attempts+1,updated_by='runtime'
			WHERE tenant_id=$1 AND id=$2`, receipt.TenantID, receipt.EventID); err != nil {
			return outcome, wrapDBErr(err, "record applied receipt failed")
		}
		if _, err := tx.Exec(ctx, `UPDATE metadata_operations
			SET state='succeeded',completed_items=total_items,failed_items=0,finished_at=NOW(),
				result=jsonb_build_object('resourceIds',jsonb_build_array($3::text)),updated_by='runtime'
			WHERE tenant_id=$1 AND id=$2`, receipt.TenantID, operationID, receipt.DeviceID); err != nil {
			return outcome, wrapDBErr(err, "complete operation failed")
		}
		if kind == operationKindDeviceDeletion {
			// The runtime has confirmed the device is gone, so the metadata record
			// follows in the same transaction. History is kept (soft delete); the
			// device stops appearing in lists and its number becomes reusable.
			if _, err := tx.Exec(ctx, `UPDATE devices
				SET deleted_at=NOW(),resource_version=resource_version+1,updated_by='runtime',updated_at=NOW()
				WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, receipt.TenantID, receipt.DeviceID); err != nil {
				return outcome, wrapDBErr(err, "remove deleted device failed")
			}
		}
		if kind == operationKindCredentialChange {
			if err := applyCredentialChangeReceiptTx(ctx, tx, receipt.TenantID, receipt.DeviceID, desired); err != nil {
				return outcome, wrapDBErr(err, "apply credential change failed")
			}
		}
		outcome.OutboxState, outcome.OperationState = deviceRuntimeOutboxApplied, "succeeded"
		return outcome, nil
	default: // failed
		if state == deviceRuntimeOutboxApplied {
			return outcome, &errs.Error{Code: errs.FailedPrecondition, Message: "event was already applied"}
		}
		if _, err := tx.Exec(ctx, `UPDATE device_runtime_outbox
			SET state='failed',last_error=$3,attempts=attempts+1,updated_by='runtime'
			WHERE tenant_id=$1 AND id=$2`, receipt.TenantID, receipt.EventID, receipt.FailureCode); err != nil {
			return outcome, wrapDBErr(err, "record failed receipt failed")
		}
		outcome.OutboxState = deviceRuntimeOutboxFailed
		if receipt.Retryable || operationState != "pending" {
			// The outbox row stays eligible for redelivery; the operation keeps
			// reporting pending rather than claiming a final failure.
			return outcome, nil
		}
		problem, err := json.Marshal(map[string]any{
			"type":      "about:blank",
			"title":     "Runtime could not apply the change",
			"status":    502,
			"detail":    "The device runtime rejected the change and will not retry it.",
			"instance":  "/api/v2/metadata-operations/" + operationID,
			"code":      receipt.FailureCode,
			"requestId": requestIDOrDefault(receipt.RequestID),
		})
		if err != nil {
			return outcome, &errs.Error{Code: errs.Internal, Message: "failed to encode operation failure"}
		}
		if _, err := tx.Exec(ctx, `UPDATE metadata_operations
			SET state='failed',failed_items=total_items,completed_items=total_items,finished_at=NOW(),
				failure=$3::jsonb,updated_by='runtime'
			WHERE tenant_id=$1 AND id=$2`, receipt.TenantID, operationID, problem); err != nil {
			return outcome, wrapDBErr(err, "fail operation failed")
		}
		outcome.OperationState = "failed"
		return outcome, nil
	}
}

func requestIDOrDefault(id string) string {
	if strings.TrimSpace(id) == "" {
		return "runtime-receipt"
	}
	return id
}
