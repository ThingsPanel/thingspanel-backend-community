package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

// nativeSetTemplateBindingInput is the validated SetTemplateBinding request.
// templateId is required and may be null (unbind). directAccess and
// deviceParameters keep their source bytes so large numbers are never rounded.
type nativeSetTemplateBindingInput struct {
	TemplateID       NativeDeviceField[string]
	DirectAccess     NativeDeviceField[json.RawMessage]
	DeviceParameters json.RawMessage
}

func decodeNativeSetTemplateBinding(body []byte) (nativeSetTemplateBindingInput, error) {
	var out nativeSetTemplateBindingInput
	fields, err := strictJSONObject(body)
	if err != nil {
		return out, err
	}
	if err := rejectUnknown(fields, setOf("templateId", "directAccess", "deviceParameters")); err != nil {
		return out, err
	}
	raw, ok := fields["templateId"]
	if !ok {
		return out, invalid("templateId", "required")
	}
	if err := decodeNullableString(raw, "templateId", 36, true, &out.TemplateID); err != nil {
		return out, err
	}
	if raw, ok := fields["directAccess"]; ok {
		if err := decodeNullableJSON(raw, "directAccess", validateDirectAccess, &out.DirectAccess); err != nil {
			return out, err
		}
	}
	out.DeviceParameters = json.RawMessage(`{}`)
	if raw, ok := fields["deviceParameters"]; ok {
		if err := validateJSONMap(raw, "deviceParameters", 100, maxDeviceJSONBytes); err != nil {
			return out, err
		}
		out.DeviceParameters = cloneRaw(raw)
	}
	if nonNull(out.TemplateID) && nonNull(out.DirectAccess) {
		return out, invalid("directAccess", "cannot be combined with templateId")
	}
	return out, nil
}

// loadNativeDeviceTemplateBindingEncoreTx builds the same projection and ETag
// as loadNativeDeviceTemplateBindingTx, but inside an Encore write transaction.
// Both readers feed nativeDeviceTemplateBindingETag, so the validator a client
// obtained from GET /template-binding is exactly what If-Match is compared with.
func loadNativeDeviceTemplateBindingEncoreTx(ctx context.Context, tx *sqldb.Tx, tenantID, deviceID, requestID string) (*NativeDeviceTemplateBindingResponse, error) {
	device, err := loadNativeDeviceEncoreTx(ctx, tx, tenantID, deviceID)
	if err != nil {
		return nil, err
	}
	var bindingDeviceID string
	var templateID sql.NullString
	var directAccess, parameters []byte
	var generation int64
	err = tx.QueryRow(ctx, `
		SELECT b.device_id::text,b.template_id::text,b.direct_access,b.device_parameters,b.configuration_generation
		FROM device_access_bindings b
		JOIN devices d ON d.tenant_id=b.tenant_id AND d.id=b.device_id
		WHERE b.tenant_id=$1 AND b.device_id=$2 AND d.deleted_at IS NULL`, tenantID, deviceID,
	).Scan(&bindingDeviceID, &templateID, &directAccess, &parameters, &generation)
	if err != nil {
		if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "device template binding is incomplete"}
		}
		return nil, wrapDBErr(err, "read device template binding failed")
	}
	if bindingDeviceID != device.ID || templateID.Valid != (device.TemplateID != nil) ||
		(templateID.Valid && templateID.String != *device.TemplateID) {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device template binding projection is inconsistent"}
	}
	if generation < 1 || generation > 9007199254740991 || device.ResourceVersion < 1 || device.ResourceVersion > 9007199254740991 {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device template binding version is outside the safe range"}
	}
	if len(parameters) == 0 || !json.Valid(parameters) || !jsonObjectWithinLimits(parameters) {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device parameters are unavailable"}
	}
	if err := rejectStoredSecretParameters(parameters); err != nil {
		return nil, err
	}

	var direct *json.RawMessage
	if templateID.Valid {
		if len(directAccess) != 0 {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "device template binding contains conflicting access configurations"}
		}
		var deviceKind sql.NullString
		err := tx.QueryRow(ctx, `
			SELECT configuration->>'deviceKind'
			FROM device_access_templates
			WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, templateID.String,
		).Scan(&deviceKind)
		if err != nil {
			if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return nil, &errs.Error{Code: errs.Unavailable, Message: "bound device template is unavailable"}
			}
			return nil, wrapDBErr(err, "read bound device template failed")
		}
		if !deviceKind.Valid || deviceKind.String != device.DeviceKind {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "bound device template kind is inconsistent"}
		}
	} else if len(directAccess) != 0 {
		publicDirect, access, err := decodeStoredDirectAccess(directAccess)
		if err != nil {
			return nil, err
		}
		if err := validateStoredDeviceAccessShape(device.DeviceKind, access, parameters); err != nil {
			return nil, err
		}
		direct = &publicDirect
	} else if !emptyJSONObject(parameters) {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "unconfigured device has non-empty device parameters"}
	}

	binding := NativeDeviceTemplateBinding{
		DeviceID: device.ID, TemplateID: nullableStringPointer(templateID),
		ModelBinding: device.ModelBinding, ConfigurationGeneration: generation,
		ResourceVersion: device.ResourceVersion, DirectAccess: direct,
		DeviceParameters: append(json.RawMessage(nil), parameters...),
	}
	etag, err := nativeDeviceTemplateBindingETag(binding)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to encode device template binding"}
	}
	return &NativeDeviceTemplateBindingResponse{Data: binding, Meta: RequestMeta{RequestID: requestID}, ETag: etag}, nil
}

// setNativeDeviceTemplateBinding binds the device to a template's current
// configuration, stores a direct access configuration, or detaches it.
//
// The metadata change is committed with a new configuration generation, a
// pending operation and a runtime outbox row. Nothing here reports the change
// as applied: only the runtime owner (DM-RT-007) moves the operation to
// succeeded. Existing credentials, rules and history are never modified.
func setNativeDeviceTemplateBinding(
	ctx context.Context,
	identity nativeDeviceWriteIdentity,
	deviceID, key, ifMatch string,
	body []byte,
	requestID string,
	checkConfiguration nativeDeviceConfigurationCheck,
) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	input, err := decodeNativeSetTemplateBinding(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	path := "/api/v2/devices/" + deviceID + "/template-binding"
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "setDeviceTemplateBinding",
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
		current, err := loadNativeDeviceTemplateBindingEncoreTx(ctx, tx, identity.tenantID, deviceID, requestID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if current.ETag != ifMatch {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceStaleValidator("template binding has changed; read it again before updating")
		}
		device, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}

		// A device keeps the template it was created with: the community edition
		// does not move devices between templates, so changing or removing an
		// existing template is refused. Keeping the same template (for example to
		// update its device parameters) and binding a device that has none stay allowed.
		if current.Data.TemplateID != nil {
			keeps := nonNull(input.TemplateID) && strings.EqualFold(input.TemplateID.Value, *current.Data.TemplateID)
			if !keeps {
				return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceConflict("the device template cannot be changed after the device is created")
			}
		}

		// Resolve the target configuration. Exactly one of the three shapes is
		// stored: a template reference, a direct access document, or neither.
		var targetTemplateID *string
		var targetDirect any
		definitions := json.RawMessage(`[]`)
		switch {
		case nonNull(input.TemplateID):
			id, err := nativeReadUUID(input.TemplateID.Value)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("templateId", "must be a UUID")
			}
			var kind, state string
			var access, selection []byte
			err = tx.QueryRow(ctx, `SELECT configuration->>'deviceKind',administrative_state,configuration->'access',configuration->'customFieldDefinitions',configuration->'model'
				FROM device_access_templates
				WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND configuration IS NOT NULL FOR SHARE`, identity.tenantID, id).Scan(&kind, &state, &access, &definitions, &selection)
			if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "template not found"}
			}
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			sameTemplate := current.Data.TemplateID != nil && *current.Data.TemplateID == id
			if state != "enabled" && !sameTemplate {
				return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceConflict("template is disabled for new bindings")
			}
			if kind != device.DeviceKind {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("templateId", "template device kind must match this device")
			}
			// Devices inherit the template's selected published model through
			// template_id; confirm that selection still resolves.
			if _, err := resolveAccessTemplateModelBindingTx(ctx, tx, identity.tenantID, selection); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			if checkConfiguration == nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "fixed provider schema validation is unavailable"}
			}
			if err := checkConfiguration(ctx, identity.tenantID, device.DeviceKind, access, input.DeviceParameters); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			targetTemplateID = &id
		case nonNull(input.DirectAccess):
			var direct struct {
				Access json.RawMessage `json:"access"`
			}
			if err := json.Unmarshal(input.DirectAccess.Value, &direct); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("directAccess", "invalid direct access")
			}
			if checkConfiguration == nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "fixed provider schema validation is unavailable"}
			}
			if err := checkConfiguration(ctx, identity.tenantID, device.DeviceKind, direct.Access, input.DeviceParameters); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			targetDirect = []byte(input.DirectAccess.Value)
		default:
			if !emptyJSONObject(input.DeviceParameters) {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("deviceParameters", "must be empty without an access configuration")
			}
		}

		// Extension values are defined by the template. A target that cannot
		// carry the device's existing values is a conflict; values are never
		// dropped or rewritten here.
		if err := validateNativeCustomFieldsTx(ctx, tx, device.CustomFields, device.CustomFields, definitions, targetTemplateID != nil); err != nil {
			var inputErr *NativeDeviceInputError
			if errors.As(err, &inputErr) {
				return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceConflict("device extension values are incompatible with the target configuration; clear them first")
			}
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}

		var changed bool
		if err := tx.QueryRow(ctx, `SELECT template_id IS DISTINCT FROM $3::uuid
				OR direct_access IS DISTINCT FROM $4::jsonb
				OR device_parameters IS DISTINCT FROM $5::jsonb
			FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2`,
			identity.tenantID, deviceID, targetTemplateID, targetDirect, []byte(input.DeviceParameters)).Scan(&changed); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		digest := operationRequestDigest(command.Method, path, ifMatch, canonical)
		before := device.ResourceVersion

		if !changed {
			// Repeating the stored configuration emits no runtime event.
			operationID, err := createCompletedDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID,
				operationKindTemplateBinding, deviceID, command.Operation, requestID, digest)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			operation, err := loadNativeOperationEncoreTx(ctx, tx, identity.tenantID, operationID)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			response, err := operationAcceptedResponse(operation, operationID, requestID, current.ETag)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			changes, _ := json.Marshal(map[string]any{"changed": false})
			return response, metadataWriteAudit{
				ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &before, AfterVersion: &before,
				RedactedChanges: changes,
			}, nil
		}

		if before >= 9007199254740991 || current.Data.ConfigurationGeneration >= 9007199254740991 {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceConflict("device configuration version is exhausted")
		}
		operationID, err := createDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID,
			operationKindTemplateBinding, deviceID, command.Operation, requestID, digest,
			map[string]any{"awaiting": "runtime-application", "releaseTask": "DM-RT-007"})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		nextGeneration := current.Data.ConfigurationGeneration + 1
		if _, err := tx.Exec(ctx, `UPDATE device_access_bindings SET template_id=$3,direct_access=$4::jsonb,device_parameters=$5::jsonb,
				configuration_generation=$6,pending_operation_id=$7,updated_by=$8,updated_at=NOW()
			WHERE tenant_id=$1 AND device_id=$2 AND configuration_generation=$9`,
			identity.tenantID, deviceID, targetTemplateID, targetDirect, []byte(input.DeviceParameters),
			nextGeneration, operationID, identity.subjectID, current.Data.ConfigurationGeneration); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		// The device snapshot (templateId, modelBinding) is derived from the
		// binding, so its version advances with it.
		if _, err := tx.Exec(ctx, `UPDATE devices SET resource_version=resource_version+1,updated_by=$4,updated_at=NOW()
			WHERE tenant_id=$1 AND id=$2 AND resource_version=$3 AND deleted_at IS NULL`,
			identity.tenantID, deviceID, before, identity.subjectID); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		updated, err := loadNativeDeviceTemplateBindingEncoreTx(ctx, tx, identity.tenantID, deviceID, requestID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if updated.Data.ConfigurationGeneration != nextGeneration || updated.Data.ResourceVersion != before+1 {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeDeviceConflict("device configuration changed concurrently; read it again before updating")
		}
		if err := enqueueDeviceRuntimeTx(ctx, tx, identity.tenantID, deviceID, operationID,
			operationKindTemplateBinding, nextGeneration,
			map[string]any{
				"deviceId": deviceID, "templateId": targetTemplateID, "directAccess": targetDirect != nil,
				"configurationGeneration": nextGeneration,
			}); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		operation, err := loadNativeOperationEncoreTx(ctx, tx, identity.tenantID, operationID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := operationAcceptedResponse(operation, operationID, requestID, updated.ETag)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{
			"changed": true, "templateBound": targetTemplateID != nil, "directAccess": targetDirect != nil,
			"configurationGeneration": nextGeneration,
		})
		return response, metadataWriteAudit{
			ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &before, AfterVersion: &updated.Data.ResourceVersion,
			RedactedChanges: changes,
		}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

//encore:api auth raw method=PUT path=/api/v2/devices/:deviceID/template-binding
func (s *Service) SetDeviceTemplateBinding(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	write, ok := readNativeConditionalDeviceWrite(w, r, requestID, "template-binding", true)
	if !ok {
		return
	}
	// Built-in MQTT validates locally. Connector configurations require their
	// authoritative adapter and fail closed until it is supplied.
	result, err := setNativeDeviceTemplateBinding(r.Context(), write.identity, write.deviceID, write.key, write.ifMatch, write.body, requestID, validateNativeDeviceConfiguration)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}
