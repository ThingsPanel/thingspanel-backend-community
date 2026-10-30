package devicemetadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"encore.app/devicemetadata/accessconfig"
	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
)

// NativeDeviceTemplateBinding is the current seven-field device binding
// projection. ThingModelBinding is the associated template's current selection
// of a published ThingModel snapshot; publishing a model alone does not change it.
type NativeDeviceTemplateBinding struct {
	DeviceID                string             `json:"deviceId"`
	TemplateID              *string            `json:"templateId"`
	ModelBinding            *ThingModelBinding `json:"modelBinding"`
	ConfigurationGeneration int64              `json:"configurationGeneration"`
	ResourceVersion         int64              `json:"resourceVersion"`
	DirectAccess            *json.RawMessage   `json:"directAccess"`
	DeviceParameters        json.RawMessage    `json:"deviceParameters"`
}

type NativeDeviceTemplateBindingResponse struct {
	Data NativeDeviceTemplateBinding `json:"data"`
	Meta RequestMeta                 `json:"meta"`
	ETag string                      `json:"-" header:"ETag"`
}

func readNativeDeviceTemplateBinding(
	ctx context.Context,
	identity nativeDeviceReadIdentity,
	deviceID, requestID string,
) (*NativeDeviceTemplateBindingResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "read device template binding failed")
	}
	defer tx.Rollback()

	response, err := loadNativeDeviceTemplateBindingTx(ctx, tx, identity.tenantID, deviceID, requestID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "read device template binding failed")
	}
	return response, nil
}

// loadNativeDeviceTemplateBindingTx builds the binding projection and its ETag
// from an existing transaction. The write path reuses it so a conditional
// update compares exactly the validator a GET of this sub-resource returned.
func loadNativeDeviceTemplateBindingTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID, deviceID, requestID string,
) (*NativeDeviceTemplateBindingResponse, error) {
	device, err := getNativeDeviceRecordTx(ctx, tx, tenantID, deviceID)
	if err != nil {
		return nil, err
	}

	var templateID sql.NullString
	var directAccess, parameters []byte
	var generation int64
	var bindingDeviceID string
	err = tx.QueryRowContext(ctx, `
		SELECT b.device_id::text,b.template_id::text,b.direct_access,b.device_parameters,
		       b.configuration_generation
		FROM device_access_bindings b
		JOIN devices d ON d.tenant_id=b.tenant_id AND d.id=b.device_id
		WHERE b.tenant_id=$1 AND b.device_id=$2 AND d.deleted_at IS NULL`,
		tenantID, deviceID,
	).Scan(&bindingDeviceID, &templateID, &directAccess, &parameters, &generation)
	if err != nil {
		if err == sql.ErrNoRows {
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
		var deviceKind string
		err := tx.QueryRowContext(ctx, `
			SELECT configuration->>'deviceKind'
			FROM device_access_templates
			WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, templateID.String,
		).Scan(&deviceKind)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, &errs.Error{Code: errs.Unavailable, Message: "bound device template is unavailable"}
			}
			return nil, wrapDBErr(err, "read bound device template failed")
		}
		if deviceKind != device.DeviceKind {
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

func jsonObjectWithinLimits(raw []byte) bool {
	if len(raw) > maxDeviceJSONBytes {
		return false
	}
	return validateJSONMap(raw, "deviceParameters", 100, maxDeviceJSONBytes) == nil
}

func validateStoredDeviceAccessShape(deviceKind string, access, parameters []byte) error {
	wrappedDirect, err := json.Marshal(struct {
		Access           json.RawMessage `json:"access"`
		ExternalDeviceID *string         `json:"externalDeviceId"`
	}{Access: json.RawMessage(access)})
	if err != nil || validateDirectAccess(wrappedDirect) != nil {
		return &errs.Error{Code: errs.Unavailable, Message: "stored device access configuration is unavailable"}
	}
	fields, err := strictJSONObject(access)
	if err != nil {
		return &errs.Error{Code: errs.Unavailable, Message: "stored device access configuration is unavailable"}
	}
	if err := rejectStoredSecretParameters(fields["parameters"]); err != nil {
		return err
	}
	wrapped, err := json.Marshal(struct {
		DeviceKind             string          `json:"deviceKind"`
		Model                  json.RawMessage `json:"model"`
		Access                 json.RawMessage `json:"access"`
		TopicMappings          []any           `json:"topicMappings"`
		ProcessingScripts      []any           `json:"processingScripts"`
		CustomFieldDefinitions []any           `json:"customFieldDefinitions"`
	}{deviceKind, json.RawMessage("null"), json.RawMessage(access), []any{}, []any{}, []any{}})
	if err != nil {
		return &errs.Error{Code: errs.Unavailable, Message: "stored device access configuration is unavailable"}
	}
	if _, err := accessconfig.DecodeConfiguration(wrapped); err != nil {
		return &errs.Error{Code: errs.Unavailable, Message: "stored device access configuration is unavailable"}
	}
	if deviceKind != "standalone" && deviceKind != "gateway" && deviceKind != "subdevice" {
		return &errs.Error{Code: errs.Unavailable, Message: "stored device access configuration is unavailable"}
	}
	if err := rejectStoredSecretParameters(parameters); err != nil {
		return err
	}
	return nil
}

func rejectStoredSecretParameters(raw []byte) error {
	if err := rejectSecretParameterKeys(raw, "deviceParameters"); err != nil {
		return &errs.Error{Code: errs.Unavailable, Message: "stored device parameters are unavailable"}
	}
	return nil
}

func decodeStoredDirectAccess(raw []byte) (json.RawMessage, []byte, error) {
	if !json.Valid(raw) {
		return nil, nil, &errs.Error{Code: errs.Unavailable, Message: "stored direct access is invalid"}
	}
	var value struct {
		Access           json.RawMessage `json:"access"`
		ExternalDeviceID json.RawMessage `json:"externalDeviceId"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || len(value.Access) == 0 || len(value.ExternalDeviceID) == 0 {
		return nil, nil, &errs.Error{Code: errs.Unavailable, Message: "stored direct access is incomplete"}
	}
	if !json.Valid(value.Access) || !json.Valid(value.ExternalDeviceID) ||
		(!bytes.Equal(bytes.TrimSpace(value.ExternalDeviceID), []byte("null")) && bytes.TrimSpace(value.ExternalDeviceID)[0] != '"') {
		return nil, nil, &errs.Error{Code: errs.Unavailable, Message: "stored direct access is invalid"}
	}
	var externalID *string
	if !bytes.Equal(bytes.TrimSpace(value.ExternalDeviceID), []byte("null")) {
		var parsed string
		if err := json.Unmarshal(value.ExternalDeviceID, &parsed); err != nil || parsed == "" {
			return nil, nil, &errs.Error{Code: errs.Unavailable, Message: "stored direct access is invalid"}
		}
		externalID = &parsed
	}
	public, err := json.Marshal(struct {
		Access           json.RawMessage `json:"access"`
		ExternalDeviceID *string         `json:"externalDeviceId"`
	}{Access: append(json.RawMessage(nil), value.Access...), ExternalDeviceID: externalID})
	if err != nil {
		return nil, nil, &errs.Error{Code: errs.Unavailable, Message: "stored direct access is invalid"}
	}
	return public, append([]byte(nil), value.Access...), nil
}

func nativeDeviceTemplateBindingETag(binding NativeDeviceTemplateBinding) (string, error) {
	encoded, err := json.Marshal(binding)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return `"sha256-` + hex.EncodeToString(sum[:]) + `"`, nil
}

func writeNativeDeviceTemplateBindingJSON(w http.ResponseWriter, requestID, instance string, status int, etag string, payload any) {
	nativehttp.WriteJSON(w, requestID, instance, status, etag, payload)
}

func writeNativeDeviceTemplateBindingProblem(w http.ResponseWriter, requestID, instance string, err error) {
	nativehttp.WriteProblem(w, requestID, instance, err, 0, false)
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/template-binding
func (s *Service) GetDeviceTemplateBinding(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		writeNativeDeviceTemplateBindingProblem(w, requestID, r.URL.Path, err)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeNativeDeviceTemplateBindingProblem(w, requestID, r.URL.Path, err)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeNativeDeviceTemplateBindingProblem(w, requestID, r.URL.Path, err)
		return
	}
	response, err := readNativeDeviceTemplateBinding(r.Context(), identity, deviceID, requestID)
	if err != nil {
		writeNativeDeviceTemplateBindingProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeNativeDeviceTemplateBindingJSON(w, requestID, r.URL.Path, http.StatusOK, response.ETag, response)
}
