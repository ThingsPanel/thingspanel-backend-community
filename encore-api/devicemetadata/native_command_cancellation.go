package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

// Cancelling a command that is still queued in the Community expected-message queue.
// Only when_online executions that Community still reports as pending can be
// cancelled; anything already published is a 409 and its receipt is left untouched.

func decodeNativeCancelReason(body []byte) (string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return "", invalidStructure("request body must be a JSON object")
	}
	for key := range raw {
		if key != "reason" {
			return "", invalid(key, "unknown field")
		}
	}
	value, ok := raw["reason"]
	if !ok {
		return "", invalid("reason", "field is required")
	}
	var reason string
	if json.Unmarshal(value, &reason) != nil || strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > commandReasonMaxRunes {
		return "", invalid("reason", "reason must be a non-empty string of at most 500 characters")
	}
	return reason, nil
}

func cancelNativeDeviceCommand(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, executionID, key string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	reason, err := decodeNativeCancelReason(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "cancelDeviceCommandExecution",
		Method: http.MethodPost, Path: nativeCommandLocation(deviceID, executionID) + "/cancellations", Key: key,
		RequestID: requestID, CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		var state, mode string
		var messageID *string
		if err := tx.QueryRow(ctx, `SELECT state,delivery_mode,runtime_message_id FROM device_command_executions
			WHERE tenant_id=$1 AND device_id=$2 AND id=$3 FOR UPDATE`, identity.tenantID, deviceID, executionID).Scan(&state, &mode, &messageID); err != nil {
			if errors.Is(err, sqldb.ErrNoRows) {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "command execution not found"}
			}
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		device, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if state != "cancelled" {
			if mode != "when_online" || state != "accepted" || messageID == nil || !strings.HasPrefix(*messageID, expectedMessagePrefix) {
				return metadataWriteResponse{}, metadataWriteAudit{}, nativeCommandConflict("only a queued when_online command can be cancelled")
			}
			expectedID := strings.TrimPrefix(*messageID, expectedMessagePrefix)
			legacyID, err := resolveRuntimeDeviceID(device.RuntimeRef, deviceID)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			rows, err := nativeRuntimeLegacy().ListExpectedCommands(ctx, legacyID, 1, 100)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, runtimeUpstreamError(err)
			}
			status := ""
			for _, row := range rows {
				if row.ID == expectedID {
					status = row.Status
				}
			}
			if status != "pending" {
				return metadataWriteResponse{}, metadataWriteAudit{}, nativeCommandConflict("the command is no longer queued in the device runtime")
			}
			if err := nativeRuntimeLegacy().DeleteExpected(ctx, expectedID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, runtimeUpstreamError(err)
			}
			if _, err := tx.Exec(ctx, `UPDATE device_command_executions SET state='cancelled',reason=$4,updated_at=NOW()
				WHERE tenant_id=$1 AND device_id=$2 AND id=$3`, identity.tenantID, deviceID, executionID, truncateReason(reason)); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		receipt, err := scanNativeCommandReceipt(tx.QueryRow(ctx, `SELECT `+nativeCommandColumns+` FROM device_command_executions
			WHERE tenant_id=$1 AND device_id=$2 AND id=$3`, identity.tenantID, deviceID, executionID))
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		encoded, err := json.Marshal(NativeCommandReceiptResponse{Data: receipt, Meta: RequestMeta{RequestID: requestID}})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"commandExecutionId": executionID, "state": "cancelled"})
		version := device.ResourceVersion
		return metadataWriteResponse{Status: http.StatusOK, Body: encoded},
			metadataWriteAudit{ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &version, AfterVersion: &version, RedactedChanges: changes}, nil
	})
	if err != nil {
		return nil, mapNativeDeviceWriteError(err)
	}
	return result, nil
}

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/command-executions/:executionID/cancellations
func (s *Service) CancelNativeDeviceCommand(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceControlIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	executionID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("executionID"))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := nativeDeviceWriteBody(w, r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := cancelNativeDeviceCommand(r.Context(), identity, deviceID, executionID, key, body, requestID)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}
