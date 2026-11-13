package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"encore.app/devicemetadata/accessconfig"
	"encore.dev/beta/errs"
)

// validateNativeDeviceConfiguration validates the locally supported built-in
// MQTT configuration. Other connector modes require their owning provider
// capability/schema service and fail closed until that integration is supplied.
// tenantID must come from the authenticated principal, never from request data.
func validateNativeDeviceConfiguration(ctx context.Context, tenantID, deviceKind string, rawAccess, deviceParameters json.RawMessage) error {
	_ = ctx // reserved for the future authoritative connector validator
	canonicalTenant, err := nativeReadUUID(tenantID)
	if err != nil || canonicalTenant != tenantID {
		return invalid("tenantId", "must be a canonical non-zero UUID")
	}
	if deviceKind != "standalone" && deviceKind != "gateway" && deviceKind != "subdevice" {
		return invalid("deviceKind", "invalid enum value")
	}
	if len(rawAccess) == 0 {
		return invalid("access", "required")
	}
	if err := rejectDuplicateJSONKeys(rawAccess); err != nil {
		return invalid("access", "invalid configuration")
	}
	wrapped, err := json.Marshal(struct {
		DeviceKind             string          `json:"deviceKind"`
		Model                  json.RawMessage `json:"model"`
		Access                 json.RawMessage `json:"access"`
		TopicMappings          []any           `json:"topicMappings"`
		ProcessingScripts      []any           `json:"processingScripts"`
		CustomFieldDefinitions []any           `json:"customFieldDefinitions"`
	}{deviceKind, json.RawMessage("null"), rawAccess, []any{}, []any{}, []any{}})
	if err != nil {
		return invalid("access", "invalid configuration")
	}
	configuration, err := accessconfig.DecodeConfiguration(wrapped)
	if err != nil {
		return invalid("access", "invalid configuration")
	}
	if err := rejectSecretParameterKeys(configuration.Access.Parameters, "access.parameters"); err != nil {
		return err
	}
	if len(deviceParameters) == 0 {
		return invalid("deviceParameters", "must be an object")
	}
	if err := validateJSONMap(deviceParameters, "deviceParameters", 100, maxDeviceJSONBytes); err != nil {
		return err
	}
	if err := rejectSecretParameterKeys(deviceParameters, "deviceParameters"); err != nil {
		return err
	}
	access := configuration.Access
	if access.Mode == "direct" && access.Protocol == "mqtt" {
		if !bytes.Equal(bytes.TrimSpace(access.ProviderID), []byte("null")) ||
			(len(access.ProviderVersion) > 0 && !bytes.Equal(bytes.TrimSpace(access.ProviderVersion), []byte("null"))) ||
			!bytes.Equal(bytes.TrimSpace(access.ServiceInstanceID), []byte("null")) {
			return invalid("access", "built-in MQTT cannot reference a provider or service instance")
		}
		if !emptyJSONObject(deviceParameters) {
			return invalid("deviceParameters", "must be empty without a provider schema")
		}
		return nil
	}
	return &errs.Error{Code: errs.Unavailable, Message: "connector configuration validation is unavailable"}
}

func emptyJSONObject(raw []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil && len(object) == 0
}

var forbiddenPublicParameterKeys = map[string]struct{}{
	"password": {}, "passwd": {}, "token": {}, "accesstoken": {}, "refreshtoken": {}, "idtoken": {},
	"secret": {}, "clientsecret": {}, "privatekey": {}, "apikey": {}, "credential": {}, "credentials": {},
}

func rejectSecretParameterKeys(raw []byte, field string) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return invalid(field, "invalid JSON")
	}
	if hasSecretParameterKey(value) {
		return invalid(field, "must not contain credential or secret fields")
	}
	return nil
}

func hasSecretParameterKey(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
			if _, forbidden := forbiddenPublicParameterKeys[normalized]; forbidden || hasSecretParameterKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if hasSecretParameterKey(child) {
				return true
			}
		}
	}
	return false
}
