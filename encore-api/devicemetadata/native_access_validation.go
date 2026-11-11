package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"encore.app/devicemetadata/accessconfig"
	"encore.app/l3-core/connectorcatalog"
	"encore.app/l3-core/connectorcatalog/providerschema"
	"encore.dev/beta/errs"
)

// validateNativeDeviceConfiguration validates built-in MQTT locally and resolves
// immutable connector schemas through their authoritative catalog owner.
// tenantID must come from the authenticated principal, never from request data.
func validateNativeDeviceConfiguration(ctx context.Context, tenantID, deviceKind string, rawAccess, deviceParameters json.RawMessage) error {
	return validateNativeDeviceConfigurationWithProvider(ctx, tenantID, deviceKind, rawAccess, deviceParameters, resolveNativeConnectorRelease)
}

type nativeProviderResolver func(context.Context, string, *connectorcatalog.ResolveNativeProviderReleaseRequest) (*connectorcatalog.ResolveNativeProviderReleaseResponse, error)

func resolveNativeConnectorRelease(ctx context.Context, tenantID string, req *connectorcatalog.ResolveNativeProviderReleaseRequest) (*connectorcatalog.ResolveNativeProviderReleaseResponse, error) {
	trustedTenant, err := mustTenantID()
	if err != nil || trustedTenant != tenantID {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "invalid connector validation tenant scope"}
	}
	return connectorcatalog.ResolveNativeProviderRelease(ctx, req)
}

func validateNativeDeviceConfigurationWithProvider(ctx context.Context, tenantID, deviceKind string, rawAccess, deviceParameters json.RawMessage, resolve nativeProviderResolver) error {
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
	var providerID, version, digest string
	if json.Unmarshal(access.ProviderID, &providerID) != nil || providerID == "" ||
		json.Unmarshal(access.ProviderVersion, &version) != nil || strings.TrimSpace(version) == "" ||
		json.Unmarshal(access.SchemaDigest, &digest) != nil || digest == "" {
		return invalid("access", "connector requires a pinned provider, version and schema digest")
	}
	// Instance ownership and runtime readiness cannot be inferred from a global
	// provider release. Do not accept service bindings until their owner confirms them.
	if access.Mode == "service_connector" || !bytes.Equal(bytes.TrimSpace(access.ServiceInstanceID), []byte("null")) {
		return &errs.Error{Code: errs.Unavailable, Message: "connector instance validation is unavailable"}
	}
	if resolve == nil {
		return &errs.Error{Code: errs.Unavailable, Message: "connector catalog is unavailable"}
	}
	release, err := resolve(ctx, tenantID, &connectorcatalog.ResolveNativeProviderReleaseRequest{ProviderID: providerID, Version: version, SchemaDigest: digest})
	if err != nil {
		return err
	}
	if release == nil || release.ProviderID != providerID || release.Version != version || release.SchemaDigest != digest {
		return &errs.Error{Code: errs.Unavailable, Message: "connector catalog returned a different release"}
	}
	contains := func(values []string, expected string) bool {
		for _, value := range values {
			if value == expected {
				return true
			}
		}
		return false
	}
	if !contains(release.Protocols, access.Protocol) || !contains(release.DeviceKinds, deviceKind) {
		return invalid("access", "provider does not support this protocol or device kind")
	}
	schemas, err := providerschema.CompilePair(release.ConfigurationSchema, release.DeviceParameterSchema)
	if err != nil {
		return &errs.Error{Code: errs.Unavailable, Message: "connector catalog schema is invalid"}
	}
	if err := schemas.ValidateConfiguration(access.Parameters); err != nil {
		return invalid("access.parameters", "does not match the pinned provider schema")
	}
	if err := schemas.ValidateDeviceParameters(deviceParameters); err != nil {
		return invalid("deviceParameters", "does not match the pinned provider schema")
	}
	return nil
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
