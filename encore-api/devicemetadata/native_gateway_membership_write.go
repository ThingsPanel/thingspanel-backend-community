package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/jackc/pgx/v5/pgconn"
)

// nativeSetGatewayMembershipInput is the validated SetGatewayMembership
// request: both fields null detaches the device, both non-null (re)attaches it.
type nativeSetGatewayMembershipInput struct {
	GatewayID        *string
	SubdeviceAddress *string
}

func decodeNativeSetGatewayMembership(body []byte) (nativeSetGatewayMembershipInput, error) {
	var out nativeSetGatewayMembershipInput
	fields, err := strictJSONObject(body)
	if err != nil {
		return out, err
	}
	if err := rejectUnknown(fields, setOf("gatewayId", "subdeviceAddress")); err != nil {
		return out, err
	}
	var gatewayID, address NativeDeviceField[string]
	rawGateway, ok := fields["gatewayId"]
	if !ok {
		return out, invalid("gatewayId", "required")
	}
	if err := decodeNullableString(rawGateway, "gatewayId", 36, true, &gatewayID); err != nil {
		return out, err
	}
	rawAddress, ok := fields["subdeviceAddress"]
	if !ok {
		return out, invalid("subdeviceAddress", "required")
	}
	if err := decodeNullableString(rawAddress, "subdeviceAddress", 128, false, &address); err != nil {
		return out, err
	}
	if nonNull(gatewayID) != nonNull(address) {
		return out, invalid("gatewayId", "gatewayId and subdeviceAddress must both be non-null or both be null")
	}
	if nonNull(gatewayID) {
		out.GatewayID = &gatewayID.Value
		out.SubdeviceAddress = &address.Value
	}
	return out, nil
}

func nativeGatewayMembershipETag(membership NativeGatewayMembership) (string, error) {
	encoded, err := json.Marshal(membership)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return `"sha256-` + hex.EncodeToString(sum[:]) + `"`, nil
}

// loadNativeGatewayMembershipEncoreTx reads the same projection as
// readNativeGatewayMembership so its ETag is the If-Match validator a client
// obtained from GET /gateway-membership.
func loadNativeGatewayMembershipEncoreTx(ctx context.Context, tx *sqldb.Tx, tenantID string, device *NativeDevice) (NativeGatewayMembership, error) {
	if device.ResourceVersion < 1 || device.ResourceVersion > 9007199254740991 {
		return NativeGatewayMembership{}, &errs.Error{Code: errs.Unavailable, Message: "device resource version is outside the supported range"}
	}
	result := NativeGatewayMembership{SubdeviceID: device.ID, ResourceVersion: device.ResourceVersion}
	var gatewayID, address string
	err := tx.QueryRow(ctx, `
		SELECT gm.gateway_device_id::text,gm.subdevice_address
		FROM gateway_memberships gm
		WHERE gm.tenant_id=$1 AND gm.child_device_id=$2`, tenantID, device.ID).Scan(&gatewayID, &address)
	if err != nil {
		if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return result, nil
		}
		return NativeGatewayMembership{}, wrapDBErr(err, "read gateway membership failed")
	}
	result.GatewayID = &gatewayID
	result.SubdeviceAddress = &address
	return result, nil
}

func mapNativeGatewayMembershipError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23514" &&
		(strings.HasPrefix(pg.Message, "gateway hierarchy") || strings.HasPrefix(pg.Message, "membership ") ||
			strings.HasPrefix(pg.Message, "gateway membership")) {
		// These messages are fixed strings raised by validate_gateway_membership_tree.
		return invalid("gatewayId", pg.Message)
	}
	return mapNativeDeviceWriteError(err)
}

// setNativeGatewayMembership attaches the device to a gateway under a
// tenant-local address, moves it, or detaches it. Detaching leaves a valid
// standalone-parent-less device; it never deletes anything. The relationship
// is committed with a pending operation and a runtime outbox row; only the
// runtime owner (DM-RT-007) can finish the operation.
func setNativeGatewayMembership(
	ctx context.Context,
	identity nativeDeviceWriteIdentity,
	deviceID, key, ifMatch string,
	body []byte,
	requestID string,
) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	input, err := decodeNativeSetGatewayMembership(body)
	if err != nil {
		return nil, err
	}
	if input.GatewayID != nil && !identity.permits(*input.GatewayID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "gateway not found"}
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	path := "/api/v2/devices/" + deviceID + "/gateway-membership"
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "setGatewayMembership",
		Method: http.MethodPut, Path: path, Key: key, RequestID: requestID, IfMatch: ifMatch,
		CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		// Same lock order as the membership trigger: tenant tree first, then rows.
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
		current, err := loadNativeGatewayMembershipEncoreTx(ctx, tx, identity.tenantID, device)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		currentETag, err := nativeGatewayMembershipETag(current)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Internal, Message: "failed to encode gateway membership"}
		}
		if currentETag != ifMatch {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceStaleValidator("gateway membership has changed; read it again before updating")
		}

		if input.GatewayID != nil {
			if *input.GatewayID == deviceID {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("gatewayId", "a device cannot be its own gateway")
			}
			if device.DeviceKind == "standalone" {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("gatewayId", "a standalone device cannot be a gateway child")
			}
			var gatewayKind string
			if err := tx.QueryRow(ctx, `SELECT device_kind FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`,
				identity.tenantID, *input.GatewayID).Scan(&gatewayKind); err != nil {
				if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
					return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "gateway not found"}
				}
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			if gatewayKind != "gateway" {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("gatewayId", "must reference a gateway device")
			}
		}

		unchanged := (input.GatewayID == nil && current.GatewayID == nil) ||
			(input.GatewayID != nil && current.GatewayID != nil && *input.GatewayID == *current.GatewayID &&
				current.SubdeviceAddress != nil && *input.SubdeviceAddress == *current.SubdeviceAddress)
		digest := operationRequestDigest(command.Method, path, ifMatch, canonical)
		before := device.ResourceVersion

		if unchanged {
			operationID, err := createCompletedDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID,
				operationKindGatewayBinding, deviceID, command.Operation, requestID, digest)
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
			changes, _ := json.Marshal(map[string]any{"changed": false})
			return response, metadataWriteAudit{
				ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &before, AfterVersion: &before,
				RedactedChanges: changes,
			}, nil
		}

		if before >= 9007199254740991 {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceConflict("device version is exhausted")
		}
		switch {
		case input.GatewayID == nil:
			if _, err := tx.Exec(ctx, `DELETE FROM gateway_memberships WHERE tenant_id=$1 AND child_device_id=$2`,
				identity.tenantID, deviceID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		case current.GatewayID != nil:
			if _, err := tx.Exec(ctx, `UPDATE gateway_memberships SET gateway_device_id=$3,subdevice_address=$4,updated_by=$5
				WHERE tenant_id=$1 AND child_device_id=$2`,
				identity.tenantID, deviceID, *input.GatewayID, *input.SubdeviceAddress, identity.subjectID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		default:
			if _, err := tx.Exec(ctx, `INSERT INTO gateway_memberships(tenant_id,child_device_id,gateway_device_id,subdevice_address,updated_by)
				VALUES($1,$2,$3,$4,$5)`,
				identity.tenantID, deviceID, *input.GatewayID, *input.SubdeviceAddress, identity.subjectID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		// The device snapshot exposes gatewayId, so its version advances too.
		if _, err := tx.Exec(ctx, `UPDATE devices SET resource_version=resource_version+1,updated_by=$4,updated_at=NOW()
			WHERE tenant_id=$1 AND id=$2 AND resource_version=$3 AND deleted_at IS NULL`,
			identity.tenantID, deviceID, before, identity.subjectID); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		updatedDevice, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if updatedDevice.ResourceVersion != before+1 {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceConflict("device changed concurrently; read it again before updating")
		}
		updated, err := loadNativeGatewayMembershipEncoreTx(ctx, tx, identity.tenantID, updatedDevice)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		nextETag, err := nativeGatewayMembershipETag(updated)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Internal, Message: "failed to encode gateway membership"}
		}
		operationID, err := createDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID,
			operationKindGatewayBinding, deviceID, command.Operation, requestID, digest,
			map[string]any{"awaiting": "runtime-application", "releaseTask": "DM-RT-007"})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if err := enqueueDeviceRuntimeTx(ctx, tx, identity.tenantID, deviceID, operationID,
			operationKindGatewayBinding, updated.ResourceVersion,
			map[string]any{"deviceId": deviceID, "gatewayId": updated.GatewayID, "subdeviceAddress": updated.SubdeviceAddress}); err != nil {
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
		changes, _ := json.Marshal(map[string]any{"changed": true, "attached": updated.GatewayID != nil})
		return response, metadataWriteAudit{
			ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &before, AfterVersion: &updated.ResourceVersion,
			RedactedChanges: changes,
		}, nil
	})
	return result, mapNativeGatewayMembershipError(err)
}

//encore:api auth raw method=PUT path=/api/v2/devices/:deviceID/gateway-membership
func (s *Service) SetNativeGatewayMembership(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	write, ok := readNativeConditionalDeviceWrite(w, r, requestID, "gateway-membership", true)
	if !ok {
		return
	}
	result, err := setNativeGatewayMembership(r.Context(), write.identity, write.deviceID, write.key, write.ifMatch, write.body, requestID)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}
