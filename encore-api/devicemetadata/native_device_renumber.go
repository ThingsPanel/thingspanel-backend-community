package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

// A number change keeps the device ID and its history; only the business number
// moves. Uniqueness is enforced by uq_devices_tenant_device_number inside the same
// transaction, so a lost race leaves the original number untouched. The runtime
// owner is told through the outbox; the operation stays pending until it reports
// back (DM-RT-007) and is never marked succeeded here.

type nativeRenumberInput struct {
	DeviceNumber          string
	ExpectedDeviceVersion int64
	Reason                string
}

func decodeNativeRenumberDevice(body []byte) (nativeRenumberInput, error) {
	var out nativeRenumberInput
	fields, err := strictJSONObject(body)
	if err != nil {
		return out, err
	}
	if err := rejectUnknown(fields, setOf("deviceNumber", "expectedDeviceVersion", "reason")); err != nil {
		return out, err
	}
	if err := requiredDeviceNumber(fields, &out.DeviceNumber); err != nil {
		return out, err
	}
	raw, ok := fields["expectedDeviceVersion"]
	if !ok {
		return out, invalid("expectedDeviceVersion", "is required")
	}
	if out.ExpectedDeviceVersion, err = decodeExpectedDeviceVersion(raw); err != nil {
		return out, err
	}
	if _, ok := fields["reason"]; !ok {
		return out, invalid("reason", "is required")
	}
	if err := decodeString(fields["reason"], "reason", &out.Reason); err != nil {
		return out, err
	}
	if n := len([]rune(out.Reason)); n < 1 || n > 512 || !validAccessTemplateText(out.Reason, 1, 512, true) {
		return out, invalid("reason", "must be 1 to 512 printable characters")
	}
	return out, nil
}

func renumberNativeDevice(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	input, err := decodeNativeRenumberDevice(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	path := "/api/v2/devices/" + deviceID + "/number-changes"
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "changeDeviceNumber",
		Method: http.MethodPost, Path: path, Key: key, RequestID: requestID, CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		var version int64
		var oldNumber string
		err := tx.QueryRow(ctx, `SELECT resource_version,device_number FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
			identity.tenantID, deviceID).Scan(&version, &oldNumber)
		if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "device not found"}
		}
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if version != input.ExpectedDeviceVersion {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "device has changed; read it again before changing its number"}
		}
		if oldNumber == input.DeviceNumber {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "device already has this number"}
		}
		if version >= 9007199254740991 {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "device version is exhausted"}
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET device_number=$4,resource_version=resource_version+1,updated_by=$5,updated_at=NOW()
			WHERE tenant_id=$1 AND id=$2 AND resource_version=$3 AND deleted_at IS NULL`,
			identity.tenantID, deviceID, version, input.DeviceNumber, identity.subjectID); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		next := version + 1
		digest := operationRequestDigest(command.Method, path, "", canonical)
		operationID, err := createDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID, operationKindDeviceRenumber,
			deviceID, command.Operation, requestID, digest,
			map[string]any{"awaiting": "runtime-application", "releaseTask": "DM-RT-007"})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if err := enqueueDeviceRuntimeTx(ctx, tx, identity.tenantID, deviceID, operationID, operationKindDeviceRenumber, next,
			map[string]any{"deviceId": deviceID, "previousDeviceNumber": oldNumber, "deviceNumber": input.DeviceNumber, "resourceVersion": next}); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		operation, err := loadNativeOperationEncoreTx(ctx, tx, identity.tenantID, operationID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := operationAcceptedResponse(operation, operationID, requestID, "")
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		// The reason is operator free text and stays out of the audit change set.
		changes, _ := json.Marshal(map[string]any{"previousDeviceNumber": oldNumber, "deviceNumber": input.DeviceNumber})
		return response, metadataWriteAudit{
			ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &version, AfterVersion: &next, RedactedChanges: changes,
		}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/number-changes
func (s *Service) ChangeNativeDeviceNumber(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceWriteIdentity(false)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := nativeDeviceWriteBody(w, r)
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := renumberNativeDevice(r.Context(), identity, deviceID, key, body, requestID)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}
