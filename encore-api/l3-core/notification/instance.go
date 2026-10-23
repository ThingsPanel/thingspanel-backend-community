package notification

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"encore.app/l1-access/authn"
	"encore.app/l3-core/notification/contract"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidateInstance verifies either a draft or a persisted instance and asks
// its registered plugin to validate the effective configuration. It never
// stores data or invokes the delivery endpoint.
func (s *ManagementService) ValidateInstance(ctx context.Context, data *authn.Data, bearer string, request contract.InstanceValidate) (contract.PluginValidateResultV1, error) {
	// Tenant instance validation moved to the platform-selected tenant route.
	// Keep this legacy method fail-closed so internal callers cannot accidentally
	// restore self-service access to account credentials.
	return contract.PluginValidateResultV1{}, ErrIdentityForbidden
}

func (s *ManagementService) validateInstanceInTenant(ctx context.Context, tenantID string, request contract.InstanceValidate) (contract.PluginValidateResultV1, error) {
	var err error
	if s.privacy == nil || s.privacy.Check() != nil || s.validation == nil {
		return contract.PluginValidateResultV1{}, ErrPrivacyGuardClosed
	}

	var registrationID, instanceID string
	var configVersion int64
	var fullConfig json.RawMessage
	var runtime PluginRuntime
	if request.Draft != nil {
		if request.InstanceID != nil || request.ExpectedVersion != nil || request.ConfigPatch != nil {
			return contract.PluginValidateResultV1{}, ErrManagementInvalid
		}
		draft := request.Draft
		if draft.PluginRegistrationID == "" || !validChannel(draft.Channel) {
			return contract.PluginValidateResultV1{}, ErrManagementInvalid
		}
		plugin, getErr := s.plugins.GetPlugin(ctx, tenantID, draft.PluginRegistrationID)
		if getErr != nil {
			return contract.PluginValidateResultV1{}, mapManagementError(getErr)
		}
		if !plugin.Enabled || !containsChannel(plugin.Manifest.Channels, draft.Channel) {
			return contract.PluginValidateResultV1{}, ErrManagementInvalid
		}
		if _, _, _, err = splitAndValidateConfig(plugin.Manifest, draft.Config); err != nil {
			return contract.PluginValidateResultV1{}, ErrManagementInvalid
		}
		registrationID, fullConfig, configVersion = draft.PluginRegistrationID, draft.Config, 1
		instanceID = "draft"
		runtime, err = s.validation.ResolveRegistrationRuntime(ctx, tenantID, registrationID)
		if err != nil {
			return contract.PluginValidateResultV1{}, mapManagementError(err)
		}
	} else {
		if request.InstanceID == nil || request.ExpectedVersion == nil || request.ConfigPatch == nil || *request.InstanceID == "" || *request.ExpectedVersion < 1 {
			return contract.PluginValidateResultV1{}, ErrManagementInvalid
		}
		view, getErr := s.instances.GetInstance(ctx, tenantID, *request.InstanceID)
		if getErr != nil {
			return contract.PluginValidateResultV1{}, mapManagementError(getErr)
		}
		if view.Version != *request.ExpectedVersion {
			return contract.PluginValidateResultV1{}, ErrManagementConflict
		}
		plugin, getErr := s.plugins.GetPlugin(ctx, tenantID, view.PluginRegistrationID)
		if getErr != nil {
			return contract.PluginValidateResultV1{}, mapManagementError(getErr)
		}
		if !plugin.Enabled || !containsChannel(plugin.Manifest.Channels, view.Channel) {
			return contract.PluginValidateResultV1{}, ErrManagementInvalid
		}
		if err := validateConfigPatch(plugin.Manifest, request.ConfigPatch); err != nil {
			return contract.PluginValidateResultV1{}, ErrManagementInvalid
		}
		current, resolveErr := s.validation.ResolveInstanceConfig(ctx, tenantID, view.ID, view.ConfigVersion)
		if resolveErr != nil {
			return contract.PluginValidateResultV1{}, mapManagementError(resolveErr)
		}
		fullConfig, err = mergeValidationConfigPatch(current, request.ConfigPatch)
		if err != nil {
			return contract.PluginValidateResultV1{}, ErrManagementInvalid
		}
		fullSchema, schemaErr := CompileNotificationSchema(plugin.Manifest.ConfigSchema)
		if schemaErr != nil || ValidateNotificationSchema(fullSchema, fullConfig) != nil {
			return contract.PluginValidateResultV1{}, ErrManagementInvalid
		}
		registrationID, instanceID, configVersion = view.PluginRegistrationID, view.ID, view.ConfigVersion
		// Validation pins the immutable registration ID from the persisted
		// instance snapshot. It must work while the instance itself is disabled;
		// the send resolver remains enabled-only.
		runtime, err = s.validation.ResolveRegistrationRuntime(ctx, tenantID, registrationID)
		if err != nil {
			return contract.PluginValidateResultV1{}, mapManagementError(err)
		}
	}

	if runtime.AuthSecret == "" || runtime.Origin == "" || runtime.RegistrationID != registrationID {
		return contract.PluginValidateResultV1{}, ErrManagementUnavailable
	}
	if len(fullConfig) == 0 || len(fullConfig) > 64<<10 {
		return contract.PluginValidateResultV1{}, ErrManagementInvalid
	}
	result, err := s.pluginHTTP.ValidateInstance(ctx, runtime, contract.PluginValidateRequestV1{
		APIVersion: "1.0", TenantID: tenantID, InstanceID: instanceID,
		ConfigVersion: configVersion, Config: fullConfig,
	})
	if err != nil {
		return contract.PluginValidateResultV1{}, ErrManagementUnavailable
	}
	return result, nil
}

func mergeValidationConfigPatch(current, patch json.RawMessage) (json.RawMessage, error) {
	var base, changes map[string]json.RawMessage
	if contract.DecodeStrictJSON(current, &base) != nil || base == nil || contract.DecodeStrictJSON(patch, &changes) != nil || changes == nil {
		return nil, ErrManagementInvalid
	}
	for key, value := range changes {
		base[key] = value
	}
	merged, err := json.Marshal(base)
	if err != nil {
		return nil, ErrManagementInvalid
	}
	return merged, nil
}

var (
	ErrManagementInvalid                 = errors.New("notification management request is invalid")
	ErrManagementConflict                = errors.New("notification management request conflicts with current state")
	ErrManagementNotFound                = errors.New("notification resource was not found")
	ErrIdentityChangeRequiresNewInstance = errors.New("notification identity change requires a new instance")
)

func (s *ManagementService) CreateInstance(ctx context.Context, data *authn.Data, bearer, idempotencyKey, requestDigest string, request contract.InstanceCreate) (contract.InstanceView, error) {
	return contract.InstanceView{}, ErrIdentityForbidden
}

func (s *ManagementService) createInstanceInTenant(ctx context.Context, tenantID, idempotencyKey, requestDigest string, request contract.InstanceCreate) (contract.InstanceView, error) {
	var err error
	if !validManagementRequest(idempotencyKey, requestDigest) || request.PluginRegistrationID == "" || !validInstanceName(request.Name) || !validChannel(request.Channel) {
		return contract.InstanceView{}, ErrManagementInvalid
	}
	if s.privacy.Check() != nil {
		return contract.InstanceView{}, ErrPrivacyGuardClosed
	}
	plugin, err := s.plugins.GetPlugin(ctx, tenantID, request.PluginRegistrationID)
	if err != nil {
		return contract.InstanceView{}, mapManagementError(err)
	}
	if !plugin.Enabled || !containsChannel(plugin.Manifest.Channels, request.Channel) {
		return contract.InstanceView{}, ErrManagementInvalid
	}
	_, secrets, safeConfig, err := splitAndValidateConfig(plugin.Manifest, request.Config)
	if err != nil {
		return contract.InstanceView{}, ErrManagementInvalid
	}
	request.Config = safeConfig
	view, err := s.instances.CreateInstance(ctx, tenantID, idempotencyKey, requestDigest, request, secrets)
	if err != nil {
		return contract.InstanceView{}, mapManagementError(err)
	}
	return view, nil
}

func (s *ManagementService) GetInstance(ctx context.Context, data *authn.Data, bearer, instanceID string) (contract.InstanceView, error) {
	principal, err := s.verifiedPrincipal(ctx, data, bearer)
	if err != nil {
		return contract.InstanceView{}, ErrIdentityUnauthorized
	}
	if err := requireUserCapability(principal, "read"); err != nil {
		return contract.InstanceView{}, err
	}
	if instanceID == "" {
		return contract.InstanceView{}, ErrManagementNotFound
	}
	view, err := s.instances.GetInstance(ctx, principal.TenantID, instanceID)
	if err != nil {
		return contract.InstanceView{}, mapManagementError(err)
	}
	return view, nil
}

func (s *ManagementService) ListInstances(ctx context.Context, data *authn.Data, bearer string) ([]contract.InstanceView, error) {
	principal, err := s.verifiedPrincipal(ctx, data, bearer)
	if err != nil {
		return nil, ErrIdentityUnauthorized
	}
	if err := requireUserCapability(principal, "read"); err != nil {
		return nil, err
	}
	views, err := s.instances.ListInstances(ctx, principal.TenantID)
	if err != nil {
		return nil, mapManagementError(err)
	}
	return views, nil
}

func (s *ManagementService) UpdateInstance(ctx context.Context, data *authn.Data, bearer, instanceID, idempotencyKey, requestDigest string, update contract.InstanceUpdate) (contract.InstanceView, error) {
	return contract.InstanceView{}, ErrIdentityForbidden
}

func (s *ManagementService) updateInstanceInTenant(ctx context.Context, tenantID, instanceID, idempotencyKey, requestDigest string, update contract.InstanceUpdate) (contract.InstanceView, error) {
	var err error
	if instanceID == "" || update.ExpectedVersion < 1 || !validManagementRequest(idempotencyKey, requestDigest) || update.Name != nil && !validInstanceName(*update.Name) {
		return contract.InstanceView{}, ErrManagementInvalid
	}
	if s.replays != nil {
		if replay, found, replayErr := s.replays.FindInstanceReplay(ctx, tenantID, instanceID, "instance.update", idempotencyKey, requestDigest); replayErr != nil {
			return contract.InstanceView{}, mapManagementError(replayErr)
		} else if found {
			return replay, nil
		}
	}
	if s.privacy.Check() != nil {
		return contract.InstanceView{}, ErrPrivacyGuardClosed
	}
	current, err := s.instances.GetInstance(ctx, tenantID, instanceID)
	if err != nil {
		return contract.InstanceView{}, mapManagementError(err)
	}
	plugin, err := s.plugins.GetPlugin(ctx, tenantID, current.PluginRegistrationID)
	if err != nil {
		return contract.InstanceView{}, mapManagementError(err)
	}
	if update.ConfigPatch != nil {
		if err := validateConfigPatch(plugin.Manifest, update.ConfigPatch); err != nil {
			return contract.InstanceView{}, ErrManagementInvalid
		}
	}
	if update.Secrets != nil {
		if err := validateSecretPatch(plugin.Manifest, *update.Secrets); err != nil {
			return contract.InstanceView{}, ErrManagementInvalid
		}
	}
	if update.ConfigPatch != nil || update.Secrets != nil {
		if s.validation == nil {
			return contract.InstanceView{}, ErrManagementUnavailable
		}
		currentConfig, err := s.validation.ResolveInstanceConfig(ctx, tenantID, instanceID, current.ConfigVersion)
		if err != nil {
			return contract.InstanceView{}, mapManagementError(err)
		}
		changed, err := instanceIdentityUpdateChanges(plugin.Manifest.IdentityFields, currentConfig, update)
		if err != nil {
			return contract.InstanceView{}, ErrManagementInvalid
		}
		if changed {
			return contract.InstanceView{}, ErrIdentityChangeRequiresNewInstance
		}
	}
	view, err := s.instances.UpdateInstance(ctx, tenantID, instanceID, idempotencyKey, requestDigest, update)
	if err != nil {
		return contract.InstanceView{}, mapManagementError(err)
	}
	return view, nil
}

func splitAndValidateConfig(manifest contract.PluginManifestSummary, raw json.RawMessage) (map[string]json.RawMessage, map[string]string, json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil, nil, nil, ErrManagementInvalid
	}
	schema, err := CompileNotificationSchema(manifest.ConfigSchema)
	if err != nil || ValidateNotificationSchema(schema, raw) != nil {
		return nil, nil, nil, ErrManagementInvalid
	}
	var config map[string]json.RawMessage
	if contract.DecodeStrictJSON(raw, &config) != nil || config == nil {
		return nil, nil, nil, ErrManagementInvalid
	}
	secretFields := make(map[string]bool, len(manifest.SecretFields))
	secrets := make(map[string]string, len(manifest.SecretFields))
	for _, field := range manifest.SecretFields {
		secretFields[field] = true
		value, exists := config[field]
		if !exists {
			continue
		}
		var secret string
		if json.Unmarshal(value, &secret) != nil || strings.TrimSpace(secret) == "" || len(secret) > 16<<10 {
			return nil, nil, nil, ErrManagementInvalid
		}
		secrets[field] = secret
		delete(config, field)
	}
	safeConfig, err := json.Marshal(config)
	if err != nil {
		return nil, nil, nil, ErrManagementInvalid
	}
	return config, secrets, safeConfig, nil
}

func validateConfigPatch(manifest contract.PluginManifestSummary, raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return ErrManagementInvalid
	}
	var patch map[string]json.RawMessage
	if contract.DecodeStrictJSON(raw, &patch) != nil || patch == nil {
		return ErrManagementInvalid
	}
	secretFields := make(map[string]bool, len(manifest.SecretFields))
	for _, field := range manifest.SecretFields {
		secretFields[field] = true
	}
	for key := range patch {
		if secretFields[key] {
			return ErrManagementInvalid
		}
	}
	partial, err := compilePartialConfigSchema(manifest.ConfigSchema)
	if err != nil {
		return ErrManagementInvalid
	}
	return ValidateNotificationSchema(partial, raw)
}

func validateSecretPatch(manifest contract.PluginManifestSummary, patch contract.SecretPatch) error {
	if len(patch.Set)+len(patch.Clear) == 0 || len(patch.Set)+len(patch.Clear) > 64 {
		return ErrManagementInvalid
	}
	for key, value := range patch.Set {
		if !containsString(manifest.SecretFields, key) || strings.TrimSpace(value) == "" || len(value) > 16<<10 {
			return ErrManagementInvalid
		}
	}
	var schema map[string]json.RawMessage
	if json.Unmarshal(manifest.ConfigSchema, &schema) != nil {
		return ErrManagementInvalid
	}
	var properties map[string]json.RawMessage
	if json.Unmarshal(schema["properties"], &properties) != nil {
		return ErrManagementInvalid
	}
	for field, value := range patch.Set {
		property, ok := properties[field]
		if !ok {
			return ErrManagementInvalid
		}
		wrapped, err := json.Marshal(map[string]json.RawMessage{"type": json.RawMessage(`"object"`), "properties": mustJSON(map[string]json.RawMessage{"value": property}), "required": json.RawMessage(`[` + `"value"` + `]`), "additionalProperties": json.RawMessage(`false`)})
		if err != nil {
			return ErrManagementInvalid
		}
		propertySchema, err := CompileNotificationSchema(wrapped)
		if err != nil {
			return ErrManagementInvalid
		}
		valueJSON, _ := json.Marshal(map[string]string{"value": value})
		if ValidateNotificationSchema(propertySchema, valueJSON) != nil {
			return ErrManagementInvalid
		}
	}
	var required []string
	_ = json.Unmarshal(schema["required"], &required)
	for _, key := range patch.Clear {
		if !containsString(manifest.SecretFields, key) || containsString(required, key) {
			return ErrManagementInvalid
		}
	}
	seen := make(map[string]bool)
	for _, key := range patch.Clear {
		if seen[key] || patch.Set[key] != "" {
			return ErrManagementInvalid
		}
		seen[key] = true
	}
	return nil
}

func compilePartialConfigSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || root == nil {
		return nil, ErrNotificationSchema
	}
	// ConfigPatch is a partial object. Preserve local property constraints and
	// reject unknown fields, while omitting whole-object presence constraints.
	for _, key := range []string{"required", "dependentRequired", "dependentSchemas", "if", "then", "else", "oneOf", "anyOf", "allOf", "not", "contains", "minProperties", "maxProperties"} {
		delete(root, key)
	}
	partial, err := json.Marshal(root)
	if err != nil {
		return nil, ErrNotificationSchema
	}
	return CompileNotificationSchema(partial)
}

func mustJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func validManagementRequest(key, digest string) bool {
	if len(key) < 8 || len(key) > 128 || strings.TrimSpace(key) != key || len(digest) != 64 {
		return false
	}
	for _, r := range digest {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func validInstanceName(value string) bool {
	return strings.TrimSpace(value) == value && len(value) > 0 && len(value) <= 200 && !strings.ContainsAny(value, "\r\n\x00")
}

func validChannel(value contract.Channel) bool {
	switch value {
	case contract.ChannelEmail, contract.ChannelSMS, contract.ChannelVoice, contract.ChannelIM, contract.ChannelWebhook:
		return true
	default:
		return false
	}
}

func containsChannel(values []contract.Channel, target contract.Channel) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func mapManagementError(err error) error {
	var storeError *StoreError
	if errors.As(err, &storeError) {
		switch storeError.Kind {
		case StoreErrorNotFound:
			return ErrManagementNotFound
		case StoreErrorConflict, StoreErrorVersionConflict:
			return ErrManagementConflict
		case StoreErrorInvalid:
			return ErrManagementInvalid
		}
	}
	return ErrManagementUnavailable
}

var ErrManagementUnavailable = errors.New("notification management is temporarily unavailable")
