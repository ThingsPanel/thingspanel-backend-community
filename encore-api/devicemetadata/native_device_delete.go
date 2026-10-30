package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

// requestNativeDeviceDeletion records a controlled deletion request.
//
// The contract (DM-DEV-005) is two-phase: the request is accepted and a
// runtime outbox row asks the runtime owner to isolate the device and revoke
// its credentials; only after that confirmation is the metadata tombstoned.
// This function therefore does not delete or modify the device row. A device
// that still has live sub-devices cannot be deleted. Telemetry history is not
// touched. References held by other services (automation rules, markets) are
// not visible to this service yet; they must be enforced by the metadata
// reference mechanism when the runtime owner confirms the deletion.
func requestNativeDeviceDeletion(
	ctx context.Context,
	identity nativeDeviceWriteIdentity,
	deviceID, key, ifMatch string,
	requestID string,
) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	canonical := []byte(`{}`)
	path := "/api/v2/devices/" + deviceID
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "deleteDevice",
		Method: http.MethodDelete, Path: path, Key: key, RequestID: requestID, IfMatch: ifMatch,
		CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('device-gateway-tree:' || $1::text,72311028))`, identity.tenantID); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		var lockedID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
			identity.tenantID, deviceID).Scan(&lockedID); err != nil {
			if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "device not found"}
			}
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		device, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		currentETag, err := nativeDeviceETag(device)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Internal, Message: "failed to encode device"}
		}
		if currentETag != ifMatch {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceStaleValidator("device has changed; fetch it again before deleting")
		}

		var hasChildren bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM gateway_memberships gm
				JOIN devices child ON child.tenant_id=gm.tenant_id AND child.id=gm.child_device_id
				WHERE gm.tenant_id=$1 AND gm.gateway_device_id=$2 AND child.deleted_at IS NULL)`,
			identity.tenantID, deviceID).Scan(&hasChildren); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if hasChildren {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceConflict("device still has sub-devices; detach or delete them first")
		}
		var pending bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM metadata_operations
				WHERE tenant_id=$1 AND kind=$3 AND resource_kind='device' AND resource_id=$2 AND state IN ('pending','running'))`,
			identity.tenantID, deviceID, operationKindDeviceDeletion).Scan(&pending); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if pending {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceConflict("a deletion of this device is already pending")
		}

		before := device.ResourceVersion
		digest := operationRequestDigest(command.Method, path, ifMatch, canonical)
		operationID, err := createDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID,
			operationKindDeviceDeletion, deviceID, command.Operation, requestID, digest,
			map[string]any{"awaiting": "runtime-isolation-and-credential-revocation", "releaseTask": "DM-RT-007"})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if err := enqueueDeviceRuntimeTx(ctx, tx, identity.tenantID, deviceID, operationID,
			operationKindDeviceDeletion, before,
			map[string]any{"deviceId": deviceID, "resourceVersion": before, "revokeCredentials": true, "keepHistory": true}); err != nil {
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
		changes, _ := json.Marshal(map[string]any{"deletionRequested": true})
		return response, metadataWriteAudit{
			ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &before, AfterVersion: &before,
			RedactedChanges: changes,
		}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

//encore:api auth raw method=DELETE path=/api/v2/devices/:deviceID
func (s *Service) DeleteNativeDevice(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	write, ok := readNativeConditionalDeviceWrite(w, r, requestID, "device", false)
	if !ok {
		return
	}
	result, err := requestNativeDeviceDeletion(r.Context(), write.identity, write.deviceID, write.key, write.ifMatch, requestID)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}
