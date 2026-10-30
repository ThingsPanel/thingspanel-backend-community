package devicemetadata

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"encore.dev/beta/errs"
)

// RuntimeRef is the stable runtime identity owned by the runtime service.
// It intentionally carries no connectivity or applied-state fields.
type RuntimeRef struct {
	ProviderID        string  `json:"providerId"`
	Namespace         string  `json:"namespace"`
	ExternalID        string  `json:"externalId"`
	ServiceInstanceID *string `json:"serviceInstanceId"`
}

// NativeDevice is the public device snapshot defined by the native metadata
// contract. Nullable contract fields remain present in JSON with explicit null.
type NativeDevice struct {
	ID                  string             `json:"id"`
	TenantID            string             `json:"tenantId"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
	CreatedBy           string             `json:"createdBy"`
	UpdatedBy           string             `json:"updatedBy"`
	ResourceVersion     int64              `json:"resourceVersion"`
	Name                string             `json:"name"`
	DeviceNumber        string             `json:"deviceNumber"`
	DeviceKind          string             `json:"deviceKind"`
	ProductID           *string            `json:"productId"`
	Description         *string            `json:"description"`
	Labels              json.RawMessage    `json:"labels"`
	Location            json.RawMessage    `json:"location"`
	TemplateID          *string            `json:"templateId"`
	ModelBinding        *ThingModelBinding `json:"modelBinding"`
	AdministrativeState string             `json:"administrativeState"`
	ActivationState     string             `json:"activationState"`
	ActivatedAt         *time.Time         `json:"activatedAt"`
	GatewayID           *string            `json:"gatewayId"`
	RuntimeRef          *RuntimeRef        `json:"runtimeRef"`
	Tags                []string           `json:"tags"`
	CustomFields        json.RawMessage    `json:"customFields"`
	BatchNumber         *string            `json:"batchNumber"`
}

// nativeDeviceSelectFields is the shared projection used by page reads and
// single-record reads. Keep its order in sync with scanNativeDeviceRow.
const nativeDeviceSelectFields = `
	d.id::text,
	d.tenant_id::text,
	d.created_at,
	d.updated_at,
	d.created_by,
	d.updated_by,
	d.resource_version,
	d.name,
	d.device_number,
	d.device_kind,
	d.product_id::text,
	d.description,
	d.labels,
	d.location,
	b.device_id::text,
	b.template_id::text,
	t.id::text,
	t.thing_model_id::text,
	t.thing_model_snapshot_id::text,
	s.version,
	s.canonical_checksum,
	s.resources_checksum,
	d.administrative_state,
	d.activation_state,
	d.activated_at,
	gm.gateway_device_id::text,
	b.runtime_provider_id::text,
	b.runtime_namespace,
	b.runtime_external_id,
	b.service_instance_id,
	d.tags,
	d.custom_fields,
	d.batch_number`

// nativeDeviceFromSQL contains only joins; callers append their own tenant,
// id, filtering, ordering, and pagination clauses to avoid list N+1 reads.
const nativeDeviceFromSQL = `
FROM devices d
LEFT JOIN device_access_bindings b
	ON b.tenant_id = d.tenant_id AND b.device_id = d.id
LEFT JOIN device_access_templates t
	ON t.tenant_id = b.tenant_id AND t.id = b.template_id
LEFT JOIN thing_model_snapshots s
	ON s.tenant_id = t.tenant_id AND s.thing_model_id = t.thing_model_id AND s.id = t.thing_model_snapshot_id
LEFT JOIN gateway_memberships gm
	ON gm.tenant_id = d.tenant_id AND gm.child_device_id = d.id`

type nativeDeviceRowScanner interface {
	Scan(...any) error
}

func scanNativeDeviceRow(row nativeDeviceRowScanner) (*NativeDevice, error) {
	var out NativeDevice
	var createdAt, updatedAt sql.NullTime
	var createdBy, updatedBy, name, deviceNumber, deviceKind sql.NullString
	var productID, description, bindingDeviceID, templateID, templateRecordID sql.NullString
	var modelID, snapshotID, semanticDigest, resourcesDigest sql.NullString
	var publishedRevision sql.NullInt64
	var labels, location, tags, customFields []byte
	var gatewayID, runtimeProviderID, runtimeNamespace, runtimeExternalID, serviceInstanceID sql.NullString
	var administrativeState, activationState, batchNumber sql.NullString
	var activatedAt sql.NullTime
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&createdAt,
		&updatedAt,
		&createdBy,
		&updatedBy,
		&out.ResourceVersion,
		&name,
		&deviceNumber,
		&deviceKind,
		&productID,
		&description,
		&labels,
		&location,
		&bindingDeviceID,
		&templateID,
		&templateRecordID,
		&modelID,
		&snapshotID,
		&publishedRevision,
		&semanticDigest,
		&resourcesDigest,
		&administrativeState,
		&activationState,
		&activatedAt,
		&gatewayID,
		&runtimeProviderID,
		&runtimeNamespace,
		&runtimeExternalID,
		&serviceInstanceID,
		&tags,
		&customFields,
		&batchNumber,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
		}
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to read device snapshot"}
	}
	if out.ID == "" || out.TenantID == "" {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device snapshot identity is incomplete"}
	}
	if bindingDeviceID.String != out.ID {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device access binding is incomplete"}
	}
	if !createdBy.Valid || createdBy.String == "" || !updatedBy.Valid || updatedBy.String == "" ||
		!name.Valid || name.String == "" || !deviceNumber.Valid || deviceNumber.String == "" ||
		!deviceKind.Valid || !administrativeState.Valid || !activationState.Valid ||
		!createdAt.Valid || !updatedAt.Valid || out.ResourceVersion < 1 || out.ResourceVersion > 9007199254740991 ||
		len(labels) == 0 || len(tags) == 0 || len(customFields) == 0 {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device snapshot is incomplete"}
	}
	if deviceKind.String != "standalone" && deviceKind.String != "gateway" && deviceKind.String != "subdevice" {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device kind snapshot is invalid"}
	}
	if administrativeState.String != "enabled" && administrativeState.String != "disabled" {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device administrative state is invalid"}
	}
	if activationState.String != "inactive" && activationState.String != "active" {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device activation state is invalid"}
	}
	if !json.Valid(labels) || !json.Valid(tags) || !json.Valid(customFields) {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device JSON snapshot is invalid"}
	}
	var labelObject map[string]string
	var tagList []string
	var customObject map[string]json.RawMessage
	if json.Unmarshal(labels, &labelObject) != nil || labelObject == nil ||
		json.Unmarshal(tags, &tagList) != nil || tagList == nil ||
		json.Unmarshal(customFields, &customObject) != nil || customObject == nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device JSON snapshot has an invalid shape"}
	}
	for _, value := range customObject {
		var scalar any
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		if decoder.Decode(&scalar) != nil {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "device custom field snapshot is invalid"}
		}
		switch scalar.(type) {
		case nil, string, bool, json.Number:
		default:
			return nil, &errs.Error{Code: errs.Unavailable, Message: "device custom field snapshot is invalid"}
		}
	}
	if len(location) == 0 {
		location = []byte("null")
	} else if !json.Valid(location) {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "device location snapshot is invalid"}
	}

	out.CreatedBy = createdBy.String
	out.UpdatedBy = updatedBy.String
	out.CreatedAt = createdAt.Time.UTC()
	out.UpdatedAt = updatedAt.Time.UTC()
	out.Name = name.String
	out.DeviceNumber = deviceNumber.String
	out.DeviceKind = deviceKind.String
	out.ProductID = nullableStringPointer(productID)
	out.Description = nullableStringPointer(description)
	out.Labels = append(json.RawMessage(nil), labels...)
	out.Location = append(json.RawMessage(nil), location...)
	out.TemplateID = nullableStringPointer(templateID)
	out.AdministrativeState = administrativeState.String
	out.ActivationState = activationState.String
	out.Tags = tagList
	out.CustomFields = append(json.RawMessage(nil), customFields...)
	out.BatchNumber = nullableStringPointer(batchNumber)
	if activatedAt.Valid {
		activated := activatedAt.Time.UTC()
		out.ActivatedAt = &activated
	}
	if gatewayID.Valid {
		out.GatewayID = stringPointer(gatewayID.String)
	}

	if runtimeProviderID.Valid || runtimeNamespace.Valid || runtimeExternalID.Valid || serviceInstanceID.Valid {
		if !runtimeProviderID.Valid || runtimeProviderID.String == "" || !runtimeNamespace.Valid || runtimeNamespace.String == "" ||
			!runtimeExternalID.Valid || runtimeExternalID.String == "" {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "runtime reference snapshot is incomplete"}
		}
		out.RuntimeRef = &RuntimeRef{
			ProviderID: runtimeProviderID.String, Namespace: runtimeNamespace.String,
			ExternalID: runtimeExternalID.String, ServiceInstanceID: nullableStringPointer(serviceInstanceID),
		}
	}

	if templateID.Valid {
		if !templateRecordID.Valid {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "access template is unavailable"}
		}
		if modelID.Valid || snapshotID.Valid || publishedRevision.Valid || semanticDigest.Valid || resourcesDigest.Valid {
			if !modelID.Valid || !snapshotID.Valid || !publishedRevision.Valid || publishedRevision.Int64 < 1 ||
				publishedRevision.Int64 > 9007199254740991 || !semanticDigest.Valid || !resourcesDigest.Valid ||
				!accessTemplateDigestPattern.MatchString(semanticDigest.String) || !accessTemplateDigestPattern.MatchString(resourcesDigest.String) {
				return nil, &errs.Error{Code: errs.Unavailable, Message: "ThingModel binding snapshot is incomplete"}
			}
			out.ModelBinding = &ThingModelBinding{
				ModelID: modelID.String, SnapshotID: snapshotID.String,
				PublishedRevision: uint64(publishedRevision.Int64),
				SnapshotDigest:    semanticDigest.String, ResourcesDigest: resourcesDigest.String,
			}
		}
	} else if templateRecordID.Valid {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "access template binding is inconsistent"}
	}
	return &out, nil
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return stringPointer(value.String)
}

func stringPointer(value string) *string { return &value }

func getNativeDeviceRecordTx(ctx context.Context, tx *sql.Tx, tenantID, id string) (*NativeDevice, error) {
	if tx == nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "device read requires an existing transaction"}
	}
	query := `SELECT ` + nativeDeviceSelectFields + ` ` + nativeDeviceFromSQL + ` WHERE d.tenant_id=$1 AND d.id=$2 AND d.deleted_at IS NULL`
	device, err := scanNativeDeviceRow(tx.QueryRowContext(ctx, query, tenantID, id))
	if err != nil {
		return nil, err
	}
	if device.TenantID != tenantID {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	return device, nil
}
