package devicemetadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"encore.app/devicemetadata/accessconfig"
	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type nativeDeviceWriteIdentity struct {
	nativeDeviceReadIdentity
	subjectID      string
	canWriteGroups bool
}

func currentNativeDeviceWriteIdentity(create bool) (nativeDeviceWriteIdentity, error) {
	read, err := currentNativeDeviceReadIdentity()
	if err != nil {
		return nativeDeviceWriteIdentity{}, err
	}
	data := authn.CurrentData()
	if !authz.Can(data, "device", "write") {
		return nativeDeviceWriteIdentity{}, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to write device metadata"}
	}
	if !validAccessTemplateText(data.UserID, 1, 128, true) {
		return nativeDeviceWriteIdentity{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated subject"}
	}
	if create && len(read.deviceIDs) > 0 {
		return nativeDeviceWriteIdentity{}, &errs.Error{Code: errs.PermissionDenied, Message: "device creation requires tenant-wide metadata write authority"}
	}
	return nativeDeviceWriteIdentity{nativeDeviceReadIdentity: read, subjectID: data.UserID, canWriteGroups: authz.Can(data, "group", "write")}, nil
}

// This integration port must resolve a fixed provider/schema and validate
// shared and per-device parameter domains without merging them. A nil owner
// refuses configured creation. It is never a permissive validator or a legacy
// protocol_config fallback; the connector owner adapter remains required.
type nativeDeviceConfigurationCheck func(ctx context.Context, tenantID, deviceKind string, access, deviceParameters json.RawMessage) error

func canonicalNativeWriteBody(body []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func nativeDeviceETag(device *NativeDevice) (string, error) {
	encoded, err := json.Marshal(device)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`"sha256-%x"`, sha256.Sum256(encoded)), nil
}

func loadNativeDeviceEncoreTx(ctx context.Context, tx *sqldb.Tx, tenantID, id string) (*NativeDevice, error) {
	return scanNativeDeviceRow(tx.QueryRow(ctx, "SELECT "+nativeDeviceSelectFields+nativeDeviceFromSQL+` WHERE d.tenant_id=$1 AND d.id=$2 AND d.deleted_at IS NULL`, tenantID, id))
}

func nativeDeviceMutationResponse(device *NativeDevice, requestID string) (metadataWriteResponse, error) {
	body, err := json.Marshal(NativeDeviceResponse{Data: *device, Meta: RequestMeta{RequestID: requestID}})
	if err != nil {
		return metadataWriteResponse{}, err
	}
	etag, err := nativeDeviceETag(device)
	return metadataWriteResponse{Status: 200, Body: body, ETag: etag}, err
}

func nativeOptionalString(field NativeDeviceField[string]) any {
	if !field.Present || field.Null {
		return nil
	}
	return field.Value
}
func nativeOptionalJSON(field NativeDeviceField[json.RawMessage]) any {
	if !field.Present || field.Null {
		return nil
	}
	return []byte(field.Value)
}

func validateNativeProductTx(ctx context.Context, tx *sqldb.Tx, tenantID string, field NativeDeviceField[string]) (*string, error) {
	if !field.Present || field.Null {
		return nil, nil
	}
	id, err := nativeReadUUID(field.Value)
	if err != nil {
		return nil, invalid("productId", "must be a UUID")
	}
	var found string
	err = tx.QueryRow(ctx, `SELECT id::text FROM products WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`, tenantID, id).Scan(&found)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "product not found"}
	}
	if err != nil {
		return nil, err
	}
	return &found, nil
}

func createNativeDevice(ctx context.Context, identity nativeDeviceWriteIdentity, key string, body []byte, requestID string, checkConfiguration nativeDeviceConfigurationCheck) (*metadataWriteResponse, error) {
	input, err := DecodeNativeCreateDevice(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, err
	}
	if !validAccessTemplateText(input.Name, 1, 128, true) {
		return nil, invalid("name", "must not contain surrounding whitespace or control characters")
	}
	command := metadataWriteCommand{TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "createDevice", Method: "POST", Path: "/api/v2/devices", Key: key, RequestID: requestID, CanonicalBody: canonical}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		groupIDs, err := resolveNativeInitialGroupsTx(ctx, tx, identity.tenantID, input.InitialGroupIDs, identity.canWriteGroups)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		productID, err := validateNativeProductTx(ctx, tx, identity.tenantID, input.ProductID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		var templateID *string
		var directAccess any
		definitions := json.RawMessage(`[]`)
		if nonNull(input.TemplateID) {
			id, err := nativeReadUUID(input.TemplateID.Value)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("templateId", "must be a UUID")
			}
			var kind, state string
			var access, selection []byte
			err = tx.QueryRow(ctx, `SELECT configuration->>'deviceKind',administrative_state,configuration->'access',configuration->'customFieldDefinitions',configuration->'model'
				FROM device_access_templates
				WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`, identity.tenantID, id).Scan(&kind, &state, &access, &definitions, &selection)
			if errors.Is(err, sqldb.ErrNoRows) {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "template not found"}
			}
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			if state != "enabled" {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "template is disabled for new bindings"}
			}
			if kind != input.DeviceKind {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("deviceKind", "must match the selected template")
			}
			// Validate the template's selected published version. Devices inherit
			// this selection through template_id instead of copying it.
			_, err = resolveAccessTemplateModelBindingTx(ctx, tx, identity.tenantID, selection)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			if checkConfiguration == nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "fixed provider schema validation is unavailable"}
			}
			if err := checkConfiguration(ctx, identity.tenantID, input.DeviceKind, access, input.DeviceParameters); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			templateID = &id
		} else if nonNull(input.DirectAccess) {
			var direct struct {
				Access json.RawMessage `json:"access"`
			}
			if err := json.Unmarshal(input.DirectAccess.Value, &direct); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("directAccess", "invalid direct access")
			}
			if checkConfiguration == nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "fixed provider schema validation is unavailable"}
			}
			if err := checkConfiguration(ctx, identity.tenantID, input.DeviceKind, direct.Access, input.DeviceParameters); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			directAccess = []byte(input.DirectAccess.Value)
		} else {
			var parameters map[string]json.RawMessage
			_ = json.Unmarshal(input.DeviceParameters, &parameters)
			if len(parameters) != 0 {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("deviceParameters", "must be empty without an access configuration")
			}
		}
		if err := validateNativeCustomFieldsTx(ctx, tx, input.CustomFields, json.RawMessage(`{}`), definitions, templateID != nil); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if nonNull(input.GatewayID) {
			if input.DeviceKind == "standalone" {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("gatewayId", "a standalone device cannot be a gateway child")
			}
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('device-gateway-tree:' || $1::text,72311028))`, identity.tenantID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		id := uuid.NewString()
		deviceNumber := input.DeviceNumber
		if !input.DeviceNumberPresent {
			deviceNumber = id
		}
		tags, _ := json.Marshal(input.Tags)
		_, err = tx.Exec(ctx, `INSERT INTO devices(id,tenant_id,name,device_number,device_kind,product_id,description,labels,location,tags,custom_fields,batch_number,
			administrative_state,activation_state,activation_time_source,created_by,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11::jsonb,$12,$13,'inactive','not_activated',$14,$14)`,
			id, identity.tenantID, input.Name, deviceNumber, input.DeviceKind, productID, nativeOptionalString(input.Description), []byte(input.Labels), nativeOptionalJSON(input.Location), tags, []byte(input.CustomFields), nativeOptionalString(input.BatchNumber), input.InitialAdministrativeState, identity.subjectID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO device_access_bindings(tenant_id,device_id,template_id,direct_access,device_parameters,updated_by) VALUES($1,$2,$3,$4::jsonb,$5::jsonb,$6)`, identity.tenantID, id, templateID, directAccess, []byte(input.DeviceParameters), identity.subjectID); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if nonNull(input.GatewayID) {
			if _, err := tx.Exec(ctx, `INSERT INTO gateway_memberships(tenant_id,child_device_id,gateway_device_id,subdevice_address,updated_by) VALUES($1,$2,$3,$4,$5)`, identity.tenantID, id, input.GatewayID.Value, input.SubdeviceAddress.Value, identity.subjectID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		for _, groupID := range groupIDs {
			var version int64
			if err := tx.QueryRow(ctx, `UPDATE device_groups SET resource_version=resource_version+1,updated_at=NOW(),updated_by=$3
				WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND resource_version<9007199254740991 RETURNING resource_version`, identity.tenantID, groupID, identity.subjectID).Scan(&version); err != nil {
				if errors.Is(err, sqldb.ErrNoRows) {
					return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "initial group is no longer writable"}
				}
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO device_group_members(tenant_id,group_id,device_id,created_by) VALUES($1,$2,$3,$4)`, identity.tenantID, groupID, id, identity.subjectID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			changes, _ := json.Marshal(map[string]any{"addedDeviceIds": []string{id}})
			if _, err := tx.Exec(ctx, `INSERT INTO metadata_audit_events
				(id,tenant_id,created_by,request_id,operation_name,resource_kind,resource_id,before_version,after_version,outcome,redacted_changes)
				VALUES($1,$2,$3,$4,'createDevice','device_group',$5,$6,$7,'succeeded',$8::jsonb)`, uuid.NewString(), identity.tenantID, identity.subjectID, requestID, groupID, version-1, version, changes); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		device, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, id)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := nativeDeviceMutationResponse(device, requestID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response.Status = 201
		response.Location = "/api/v2/devices/" + id
		changes, _ := json.Marshal(map[string]any{"created": true, "initialGroupIds": groupIDs})
		return response, metadataWriteAudit{ResourceKind: "device", ResourceID: id, AfterVersion: &device.ResourceVersion, RedactedChanges: changes}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

func updateNativeDevice(ctx context.Context, identity nativeDeviceWriteIdentity, id, key, ifMatch string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if !identity.permits(id) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	input, err := DecodeNativeUpdateDevice(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, err
	}
	command := metadataWriteCommand{TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "updateDevice", Method: "PATCH", Path: "/api/v2/devices/" + id, Key: key, RequestID: requestID, IfMatch: ifMatch, CanonicalBody: canonical}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		var lockedID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, identity.tenantID, id).Scan(&lockedID); err != nil {
			if errors.Is(err, sqldb.ErrNoRows) {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "device not found"}
			}
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		current, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, id)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		etag, err := nativeDeviceETag(current)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if etag != ifMatch {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "device has changed; fetch it again before updating"}
		}
		next := *current
		if input.Name.Present {
			if !validAccessTemplateText(input.Name.Value, 1, 128, true) {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("name", "must not contain surrounding whitespace or control characters")
			}
			next.Name = input.Name.Value
		}
		if input.Description.Present {
			next.Description = nil
			if !input.Description.Null {
				next.Description = &input.Description.Value
			}
		}
		if input.Labels.Present {
			next.Labels = input.Labels.Value
		}
		if input.Location.Present {
			next.Location = json.RawMessage(`null`)
			if !input.Location.Null {
				next.Location = input.Location.Value
			}
		}
		if input.Tags.Present {
			next.Tags = input.Tags.Value
		}
		if input.ProductID.Present {
			product, err := validateNativeProductTx(ctx, tx, identity.tenantID, input.ProductID)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			next.ProductID = product
		}
		if input.CustomFields.Present {
			definitions := json.RawMessage(`[]`)
			if current.TemplateID != nil {
				if err := tx.QueryRow(ctx, `SELECT configuration->'customFieldDefinitions' FROM device_access_templates WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`, identity.tenantID, *current.TemplateID).Scan(&definitions); err != nil {
					return metadataWriteResponse{}, metadataWriteAudit{}, err
				}
			}
			if err := validateNativeCustomFieldsTx(ctx, tx, input.CustomFields.Value, current.CustomFields, definitions, current.TemplateID != nil); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			next.CustomFields = input.CustomFields.Value
		}
		tags, _ := json.Marshal(next.Tags)
		var changed bool
		if err := tx.QueryRow(ctx, `SELECT name IS DISTINCT FROM $3::varchar OR description IS DISTINCT FROM $4::varchar
			OR labels IS DISTINCT FROM $5::jsonb OR location IS DISTINCT FROM $6::jsonb OR tags IS DISTINCT FROM $7::jsonb
			OR custom_fields IS DISTINCT FROM $8::jsonb OR product_id IS DISTINCT FROM $9::uuid FROM devices WHERE tenant_id=$1 AND id=$2`,
			identity.tenantID, id, next.Name, next.Description, []byte(next.Labels), nativeLocationStorage(next.Location), tags, []byte(next.CustomFields), next.ProductID).Scan(&changed); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		before := current.ResourceVersion
		if changed {
			if before >= 9007199254740991 {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "device version is exhausted"}
			}
			if _, err := tx.Exec(ctx, `UPDATE devices SET name=$4,description=$5,labels=$6::jsonb,location=$7::jsonb,tags=$8::jsonb,custom_fields=$9::jsonb,
				product_id=$10,resource_version=resource_version+1,updated_by=$11,updated_at=NOW() WHERE tenant_id=$1 AND id=$2 AND resource_version=$3 AND deleted_at IS NULL`,
				identity.tenantID, id, before, next.Name, next.Description, []byte(next.Labels), nativeLocationStorage(next.Location), tags, []byte(next.CustomFields), next.ProductID, identity.subjectID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			current, err = loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, id)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		response, err := nativeDeviceMutationResponse(current, requestID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"metadataChanged": changed})
		return response, metadataWriteAudit{ResourceKind: "device", ResourceID: id, BeforeVersion: &before, AfterVersion: &current.ResourceVersion, RedactedChanges: changes}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

func nativeLocationStorage(location json.RawMessage) any {
	if bytes.Equal(bytes.TrimSpace(location), []byte("null")) {
		return nil
	}
	return []byte(location)
}

func validateNativeCustomFieldsTx(ctx context.Context, tx *sqldb.Tx, values, previous, definitions json.RawMessage, hasTemplate bool) error {
	var incoming, old map[string]json.RawMessage
	_ = json.Unmarshal(values, &incoming)
	_ = json.Unmarshal(previous, &old)
	if !hasTemplate {
		if len(incoming) != 0 {
			return invalid("customFields", "must be empty without a template")
		}
		return nil
	}
	var defs []accessconfig.CustomFieldDefinition
	if json.Unmarshal(definitions, &defs) != nil || defs == nil {
		return &errs.Error{Code: errs.Unavailable, Message: "fixed custom-field definitions are unavailable"}
	}
	known := make(map[string]accessconfig.CustomFieldDefinition, len(defs))
	disabled := map[string]json.RawMessage{}
	for _, def := range defs {
		known[def.Key] = def
		if !def.Enabled {
			if prior, ok := old[def.Key]; ok {
				disabled[def.Key] = prior
			}
		}
	}
	for key, value := range incoming {
		def, ok := known[key]
		if !ok {
			return invalid("customFields", "contains an undefined field")
		}
		if !def.Enabled {
			if _, ok := old[key]; !ok {
				return invalid("customFields", "cannot assign a disabled field")
			}
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		var scalar any
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		if decoder.Decode(&scalar) != nil {
			return invalid("customFields", "contains an invalid scalar")
		}
		valid := false
		switch def.Type {
		case "string":
			_, valid = scalar.(string)
		case "number":
			_, valid = scalar.(json.Number)
		case "boolean":
			_, valid = scalar.(bool)
		case "enum":
			if text, ok := scalar.(string); ok {
				for _, item := range def.EnumValues {
					if item.Value == text {
						valid = true
						break
					}
				}
			}
		}
		if !valid {
			return invalid("customFields", "a value does not match its fixed definition")
		}
	}
	if len(disabled) > 0 {
		frozen, _ := json.Marshal(disabled)
		var preserved bool
		if err := tx.QueryRow(ctx, `SELECT $1::jsonb @> $2::jsonb`, []byte(values), frozen).Scan(&preserved); err != nil {
			return err
		}
		if !preserved {
			return invalid("customFields", "disabled historical values must be preserved unchanged")
		}
	}
	return nil
}

func mapNativeDeviceWriteError(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.Code == "23505" && pg.ConstraintName == "uq_devices_tenant_device_number" {
			return &errs.Error{Code: errs.AlreadyExists, Message: "device number is already reserved in this tenant"}
		}
		if pg.Code == "23505" && pg.ConstraintName == "gateway_memberships_parent_address_unique" {
			return &errs.Error{Code: errs.AlreadyExists, Message: "subdevice address is already reserved under this gateway"}
		}
		if pg.Code == "23503" {
			return &errs.Error{Code: errs.NotFound, Message: "a referenced metadata resource was not found"}
		}
		if pg.Code == "23514" || pg.Code == "22P02" || pg.Code == "22003" || pg.Code == "22P05" {
			return invalid("body", "one or more device values are invalid")
		}
		if pg.Code == "40P01" || pg.Code == "40001" {
			return &errs.Error{Code: errs.Aborted, Message: "metadata transaction conflicted; retry with the same key and request"}
		}
	}
	return err
}
