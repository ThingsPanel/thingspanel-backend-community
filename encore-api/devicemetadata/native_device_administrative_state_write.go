package devicemetadata

import (
	"bytes"
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

type nativeSetAdministrativeStateInput struct {
	State string
}

func decodeNativeSetAdministrativeState(body []byte) (nativeSetAdministrativeStateInput, error) {
	var out nativeSetAdministrativeStateInput
	fields, err := strictJSONObject(body)
	if err != nil {
		return out, err
	}
	if err := rejectUnknown(fields, setOf("state")); err != nil {
		return out, err
	}
	if err := requiredEnum(fields, "state", []string{"enabled", "disabled"}, &out.State); err != nil {
		return out, err
	}
	return out, nil
}

func setNativeDeviceAdministrativeState(
	ctx context.Context,
	identity nativeDeviceWriteIdentity,
	deviceID, key, ifMatch string,
	body []byte,
	requestID string,
) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	input, err := decodeNativeSetAdministrativeState(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	path := "/api/v2/devices/" + deviceID + "/administrative-state"
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "setDeviceAdministrativeState",
		Method: http.MethodPut, Path: path, Key: key, RequestID: requestID, IfMatch: ifMatch,
		CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		var lockedID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
			identity.tenantID, deviceID).Scan(&lockedID); err != nil {
			if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "device not found"}
			}
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		current, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		currentETag, err := nativeAdministrativeStateETag(NativeAdministrativeState{
			State: current.AdministrativeState, ResourceVersion: current.ResourceVersion,
		})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if currentETag != ifMatch {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "administrative state has changed; read it again before updating"}
		}
		digest := operationRequestDigest(command.Method, path, ifMatch, canonical)
		before := current.ResourceVersion
		if current.AdministrativeState == input.State {
			// Repeating the current desired state must not emit another runtime
			// event; the operation is terminal because nothing is left to apply.
			operationID, err := createCompletedDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID,
				operationKindAdministrativeState, deviceID, command.Operation, requestID, digest)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			operation, err := loadNativeOperationEncoreTx(ctx, tx, identity.tenantID, operationID)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			response, err := operationAcceptedResponse(operation, operationID, requestID, currentETag)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			changes, _ := json.Marshal(map[string]any{"administrativeState": input.State, "changed": false})
			return response, metadataWriteAudit{
				ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &before, AfterVersion: &before,
				RedactedChanges: changes,
			}, nil
		}
		if before >= 9007199254740991 {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "device version is exhausted"}
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET administrative_state=$4,resource_version=resource_version+1,
			updated_by=$5,updated_at=NOW() WHERE tenant_id=$1 AND id=$2 AND resource_version=$3 AND deleted_at IS NULL`,
			identity.tenantID, deviceID, before, input.State, identity.subjectID); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		updated, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		nextETag, err := nativeAdministrativeStateETag(NativeAdministrativeState{
			State: updated.AdministrativeState, ResourceVersion: updated.ResourceVersion,
		})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		operationID, err := createDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID,
			operationKindAdministrativeState, deviceID, command.Operation, requestID, digest,
			map[string]any{"awaiting": "runtime-application", "releaseTask": "DM-RT-007"})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if err := enqueueDeviceRuntimeTx(ctx, tx, identity.tenantID, deviceID, operationID,
			operationKindAdministrativeState, updated.ResourceVersion,
			map[string]any{"deviceId": deviceID, "state": input.State, "resourceVersion": updated.ResourceVersion}); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		operation, err := loadNativeOperationEncoreTx(ctx, tx, identity.tenantID, operationID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := operationAcceptedResponse(operation, operationID, requestID, nextETag)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"administrativeState": input.State, "changed": true})
		return response, metadataWriteAudit{
			ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &before, AfterVersion: &updated.ResourceVersion,
			RedactedChanges: changes,
		}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

func operationAcceptedResponse(operation *NativeOperation, operationID, requestID, etag string) (metadataWriteResponse, error) {
	body, err := json.Marshal(NativeOperationResponse{Data: *operation, Meta: RequestMeta{RequestID: requestID}})
	if err != nil {
		return metadataWriteResponse{}, err
	}
	return metadataWriteResponse{
		Status: http.StatusAccepted, Body: body, ETag: etag,
		Location: "/api/v2/metadata-operations/" + operationID,
	}, nil
}

func writeNativeDeviceOperationError(w http.ResponseWriter, r *http.Request, requestID string, err error) {
	var inputErr *NativeDeviceInputError
	if errors.As(err, &inputErr) {
		writeNativeDeviceInputProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeNativeDeviceMutationError(w, r, requestID, err, true)
}

func nativeDeviceWriteBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(http.MaxBytesReader(w, r.Body, nativeDeviceWriteMaxBytes)); err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "device request body is too large or unreadable"}
	}
	return body.Bytes(), nil
}

//encore:api auth raw method=PUT path=/api/v2/devices/:deviceID/administrative-state
func (s *Service) SetDeviceAdministrativeState(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceWriteIdentity(false)
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
	ifMatch := r.Header.Values("If-Match")
	if len(ifMatch) == 0 {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.FailedPrecondition, Message: "If-Match is required"}, http.StatusPreconditionRequired)
		return
	}
	if len(ifMatch) != 1 || !validDeviceGroupIfMatch(ifMatch[0]) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "If-Match must contain one strong administrative-state ETag"}, http.StatusBadRequest)
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
	result, err := setNativeDeviceAdministrativeState(r.Context(), identity, deviceID, key, ifMatch[0], body, requestID)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}
