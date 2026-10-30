package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"encore.app/internal/legacy"
	"encore.dev/beta/errs"
	"encore.dev/cron"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
)

// Outbox dispatcher (DM-RT-007). It pushes pending device_runtime_outbox rows
// to Community and records the result through recordRuntimeReceiptTx.
//
// Only kinds whose effect Community really implements are dispatched:
//
//	device_renumber  -> PUT /api/v1/device {device_number}
//	device_deletion  -> DELETE /api/v1/device/{id}
//	gateway_binding  -> PUT /api/v1/device {parent_id, sub_device_addr} (bind)
//	                    PUT /api/v1/device/sub-remove {sub_device_id}      (unbind)
//
// credential_change -> POST /api/v1/device/update/voucher (rotation and revocation
//                       both replace the device's voucher; see native_runtime_credential.go)
//
// administrative_state and template_binding are not applied: the community edition
// neither stops devices nor changes their template after creation, so those rows
// stay pending instead of being reported as applied.

const runtimeDispatchEnv = "TP_RUNTIME_OUTBOX_DISPATCH"

type runtimeOutboxItem struct {
	EventID    string
	TenantID   string
	DeviceID   string
	Kind       string
	Generation int64
	Desired    []byte
}

type runtimeApplyOutcome struct {
	// Skip means this applier cannot honestly apply the item; nothing is recorded.
	Skip        bool
	Applied     bool
	FailureCode string
	Retryable   bool
}

type runtimeApplier interface {
	Apply(ctx context.Context, item runtimeOutboxItem) runtimeApplyOutcome
}

type runtimeDispatchSummary struct {
	Applied, Failed, Skipped int
}

// dispatchRuntimeOutbox applies up to limit eligible rows, oldest first. A row
// is eligible when it is pending, or failed with an operation that is still
// pending (a retryable failure).
func dispatchRuntimeOutbox(ctx context.Context, applier runtimeApplier, limit int) (runtimeDispatchSummary, error) {
	var summary runtimeDispatchSummary
	rows, err := db.Query(ctx, `SELECT o.id::text,o.tenant_id::text,o.device_id::text,o.kind,o.generation,o.desired
		FROM device_runtime_outbox o
		JOIN metadata_operations m ON m.tenant_id=o.tenant_id AND m.id=o.operation_id
		WHERE o.state IN ('pending','failed') AND m.state='pending'
		ORDER BY o.created_at,o.id LIMIT $1`, limit)
	if err != nil {
		return summary, wrapDBErr(err, "list runtime outbox failed")
	}
	var items []runtimeOutboxItem
	for rows.Next() {
		var item runtimeOutboxItem
		if err := rows.Scan(&item.EventID, &item.TenantID, &item.DeviceID, &item.Kind, &item.Generation, &item.Desired); err != nil {
			rows.Close()
			return summary, wrapDBErr(err, "list runtime outbox failed")
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return summary, wrapDBErr(err, "list runtime outbox failed")
	}
	for _, item := range items {
		outcome := applier.Apply(ctx, item)
		if outcome.Skip {
			summary.Skipped++
			continue
		}
		digest, err := runtimeDesiredDigest(item.Desired)
		if err != nil {
			return summary, &errs.Error{Code: errs.Internal, Message: "failed to digest runtime event"}
		}
		receipt := runtimeReceipt{EventID: item.EventID, TenantID: item.TenantID, DeviceID: item.DeviceID,
			Generation: item.Generation, ContentDigest: digest, State: runtimeReceiptApplied, RequestID: "runtime-dispatch"}
		if !outcome.Applied {
			receipt.State, receipt.FailureCode, receipt.Retryable = runtimeReceiptFailed, outcome.FailureCode, outcome.Retryable
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			return summary, wrapDBErr(err, "record runtime receipt failed")
		}
		if _, err := recordRuntimeReceiptTx(ctx, tx, receipt); err != nil {
			tx.Rollback()
			rlog.Error("runtime receipt rejected", "event_id", item.EventID, "error", err)
			continue
		}
		if err := tx.Commit(); err != nil {
			return summary, wrapDBErr(err, "record runtime receipt failed")
		}
		if outcome.Applied {
			summary.Applied++
		} else {
			summary.Failed++
		}
	}
	return summary, nil
}

// communityRuntimeApplier maps outbox rows onto Community calls.
type communityRuntimeApplier struct {
	client *legacy.Client
}

func (a communityRuntimeApplier) Apply(ctx context.Context, item runtimeOutboxItem) runtimeApplyOutcome {
	var desired map[string]any
	if err := json.Unmarshal(item.Desired, &desired); err != nil {
		return runtimeApplyOutcome{FailureCode: "DESIRED_INVALID"}
	}
	str := func(key string) string { s, _ := desired[key].(string); return s }
	switch item.Kind {
	case operationKindDeviceRenumber, operationKindDeviceDeletion, operationKindGatewayBinding, operationKindCredentialChange:
	default:
		return runtimeApplyOutcome{Skip: true}
	}
	legacyID, err := lookupRuntimeDeviceID(ctx, item.TenantID, item.DeviceID)
	if err != nil {
		return runtimeApplyOutcome{FailureCode: "RUNTIME_IDENTITY_UNKNOWN", Retryable: true}
	}
	switch item.Kind {
	case operationKindDeviceRenumber:
		err = a.client.SetDeviceNumber(ctx, legacyID, str("deviceNumber"))
	case operationKindDeviceDeletion:
		err = a.client.RemoveDevice(ctx, legacyID)
		if errors.Is(err, legacy.ErrRejected) {
			// A previous pass may have deleted the device before its receipt was
			// recorded; a repeat is then refused. Count that as applied only if
			// Community confirms the device no longer exists.
			if gone, goneErr := a.client.DeviceGone(ctx, legacyID); goneErr == nil && gone {
				err = nil
			}
		}
	case operationKindCredentialChange:
		var change credentialChangeDesired
		if json.Unmarshal(item.Desired, &change) != nil {
			return runtimeApplyOutcome{FailureCode: "DESIRED_INVALID"}
		}
		plan := planCredentialVoucher(ctx, item.TenantID, item.DeviceID, change)
		if plan.FailureCode != "" {
			return runtimeApplyOutcome{FailureCode: plan.FailureCode, Retryable: plan.Retryable}
		}
		err = a.client.SetDeviceVoucher(ctx, legacyID, plan.Voucher)
	case operationKindGatewayBinding:
		if str("gatewayId") == "" {
			// No gateway means "unbind": the sub-device becomes free to bind elsewhere.
			err = a.client.UnbindSubdevice(ctx, legacyID)
			break
		}
		gatewayLegacy, gerr := lookupRuntimeDeviceID(ctx, item.TenantID, str("gatewayId"))
		if gerr != nil {
			return runtimeApplyOutcome{FailureCode: "RUNTIME_IDENTITY_UNKNOWN", Retryable: true}
		}
		err = a.client.BindSubdevice(ctx, legacyID, gatewayLegacy, str("subdeviceAddress"))
	}
	if err == nil {
		return runtimeApplyOutcome{Applied: true}
	}
	if errors.Is(err, legacy.ErrRejected) {
		return runtimeApplyOutcome{FailureCode: "RUNTIME_REJECTED"}
	}
	return runtimeApplyOutcome{FailureCode: "RUNTIME_UNAVAILABLE", Retryable: true}
}

// lookupRuntimeDeviceID reads the Community identity straight from the access
// binding so it also works for devices already soft-deleted.
func lookupRuntimeDeviceID(ctx context.Context, tenantID, deviceID string) (string, error) {
	var namespace, externalID sql.NullString
	err := db.QueryRow(ctx, `SELECT runtime_namespace,runtime_external_id FROM device_access_bindings
		WHERE tenant_id=$1 AND device_id=$2`, tenantID, deviceID).Scan(&namespace, &externalID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, sqldb.ErrNoRows) {
		return "", wrapDBErr(err, "read runtime identity failed")
	}
	var ref *RuntimeRef
	if namespace.Valid && externalID.Valid {
		ref = &RuntimeRef{Namespace: namespace.String, ExternalID: externalID.String}
	}
	return resolveRuntimeDeviceID(ref, deviceID)
}

var _ = cron.NewJob("dispatch-device-runtime-outbox", cron.JobConfig{
	Title:    "Apply pending device changes to the Community runtime",
	Every:    1 * cron.Minute,
	Endpoint: DispatchDeviceRuntimeOutbox,
})

// DispatchDeviceRuntimeOutbox is private operational work. It does nothing
// unless TP_RUNTIME_OUTBOX_DISPATCH=on, so an environment without a reachable
// Community runtime keeps its operations truthfully pending.
//
//encore:api private
func DispatchDeviceRuntimeOutbox(ctx context.Context) error {
	if strings.TrimSpace(os.Getenv(runtimeDispatchEnv)) != "on" {
		return nil
	}
	summary, err := dispatchRuntimeOutbox(ctx, communityRuntimeApplier{client: nativeRuntimeLegacy()}, 50)
	if err != nil {
		return err
	}
	if summary.Applied+summary.Failed > 0 {
		rlog.Info("device runtime outbox dispatched", "applied", summary.Applied, "failed", summary.Failed, "skipped", summary.Skipped)
	}
	return nil
}
