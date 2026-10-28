package notification

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"encore.app/l3-core/notification/contract"
)

type groupValidationReader interface {
	GetInstance(context.Context, string, string) (contract.InstanceView, error)
	GetPlugin(context.Context, string, string) (contract.PluginView, error)
}

type groupRecipientSource struct {
	Kind         string              `json:"kind"`
	Recipient    *contract.Recipient `json:"recipient,omitempty"`
	UserID       string              `json:"userId,omitempty"`
	ContactField string              `json:"contactField,omitempty"`
}

type groupContentBinding struct {
	Kind          string             `json:"kind"`
	Title         *string            `json:"title,omitempty"`
	Text          *string            `json:"text,omitempty"`
	TemplateID    *string            `json:"templateId,omitempty"`
	Locale        *string            `json:"locale,omitempty"`
	ParamsMapping *map[string]string `json:"paramsMapping,omitempty"`
}

var notificationVariablePath = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*$`)

func validateGroupCreate(ctx context.Context, reader groupValidationReader, tenant string, request contract.GroupCreate) []contract.FieldError {
	return validateGroupBindings(ctx, reader, tenant, request.Name, request.Enabled, request.Bindings)
}

func validateGroupUpdate(ctx context.Context, reader groupValidationReader, tenant string, request contract.GroupUpdate) []contract.FieldError {
	fields := validateGroupBindings(ctx, reader, tenant, request.Name, request.Enabled, request.Bindings)
	if request.ExpectedVersion < 1 {
		fields = append(fields, contract.FieldError{Field: "expectedVersion", Reason: "must_be_positive"})
	}
	return fields
}

func validateGroupBindings(ctx context.Context, reader groupValidationReader, tenant, name string, enabled bool, bindings []contract.GroupBinding) []contract.FieldError {
	fields := make([]contract.FieldError, 0)
	if reader == nil || tenant == "" {
		return []contract.FieldError{{Field: "bindings", Reason: "notification_unavailable"}}
	}
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 256 {
		fields = append(fields, contract.FieldError{Field: "name", Reason: "invalid_group_name"})
	}
	if enabled && len(bindings) == 0 {
		fields = append(fields, contract.FieldError{Field: "bindings", Reason: "enabled_group_requires_binding"})
	}
	seen := make(map[string]struct{}, len(bindings))
	for index, binding := range bindings {
		prefix := "bindings[" + itoa(index) + "]"
		if binding.BindingID == "" || len(binding.BindingID) > 128 {
			fields = append(fields, contract.FieldError{Field: prefix + ".bindingId", Reason: "invalid_binding_id"})
		} else if _, duplicate := seen[binding.BindingID]; duplicate {
			fields = append(fields, contract.FieldError{Field: prefix + ".bindingId", Reason: "duplicate_binding_id"})
		} else {
			seen[binding.BindingID] = struct{}{}
		}
		if binding.InstanceID == "" || len(binding.InstanceID) > 256 {
			fields = append(fields, contract.FieldError{Field: prefix + ".instanceId", Reason: "invalid_instance_id"})
			continue
		}
		instance, err := reader.GetInstance(ctx, tenant, binding.InstanceID)
		if err != nil || instance.ID != binding.InstanceID {
			fields = append(fields, contract.FieldError{Field: prefix + ".instanceId", Reason: "instance_not_found"})
			continue
		}
		if enabled && !instance.Enabled {
			fields = append(fields, contract.FieldError{Field: prefix + ".instanceId", Reason: "instance_disabled"})
			continue
		}
		plugin, err := reader.GetPlugin(ctx, tenant, instance.PluginRegistrationID)
		if err != nil || plugin.ID != instance.PluginRegistrationID {
			fields = append(fields, contract.FieldError{Field: prefix + ".instanceId", Reason: "plugin_unavailable"})
			continue
		}
		if enabled && !plugin.Enabled {
			fields = append(fields, contract.FieldError{Field: prefix + ".instanceId", Reason: "plugin_disabled"})
			continue
		}
		if !containsChannel(plugin.Manifest.Channels, instance.Channel) {
			fields = append(fields, contract.FieldError{Field: prefix + ".instanceId", Reason: "channel_unsupported"})
			continue
		}
		if err := validateGroupRecipient(binding.RecipientSource, instance.Channel, plugin.Manifest.RecipientSchema, plugin.Manifest.PluginID); err != nil {
			fields = append(fields, contract.FieldError{Field: prefix + ".recipientSource", Reason: groupValidationReason(err)})
		}
		if err := validateGroupContent(binding.ContentBinding, plugin.Manifest.ContentModes); err != nil {
			fields = append(fields, contract.FieldError{Field: prefix + ".contentBinding", Reason: groupValidationReason(err)})
		}
	}
	return fields
}

func validateGroupRecipient(raw json.RawMessage, channel contract.Channel, schemaRaw json.RawMessage, pluginIDs ...string) error {
	var source groupRecipientSource
	if contract.DecodeStrictJSON(raw, &source) != nil {
		return ErrManagementInvalid
	}
	switch source.Kind {
	case "literal":
		if source.Recipient == nil || source.UserID != "" || source.ContactField != "" {
			return ErrManagementInvalid
		}
		if strings.TrimSpace(source.Recipient.Address) == "" || !recipientMatchesChannel(source.Recipient.Kind, channel) {
			return ErrManagementInvalid
		}
		encoded, err := json.Marshal(source.Recipient)
		if err != nil {
			return ErrManagementInvalid
		}
		schema, err := CompileNotificationSchema(schemaRaw)
		if err != nil || ValidateNotificationSchema(schema, encoded) != nil {
			return ErrManagementInvalid
		}
		return nil
	case "member":
		if source.Recipient != nil || source.UserID == "" || len(source.UserID) > 256 {
			return ErrManagementInvalid
		}
		// appDevice explicitly expands to session-bound device IDs. Legacy
		// applicationUserId must never be mistaken for a provider recipient.
		switch {
		case channel == contract.ChannelIM && source.ContactField == "appDevice" && len(pluginIDs) == 1 && pluginIDs[0] == "thingspanel.unipush":
			schema, err := CompileNotificationSchema(schemaRaw)
			if err != nil || ValidateNotificationSchema(schema, []byte(`{"kind":"user_id","address":"device-binding"}`)) != nil {
				return ErrManagementInvalid
			}
			return nil
		case (channel == contract.ChannelEmail && source.ContactField == "email"),
			(channel == contract.ChannelSMS || channel == contract.ChannelVoice) && source.ContactField == "phone":
			return nil
		default:
			return ErrManagementInvalid
		}
	default:
		return ErrManagementInvalid
	}
}

func validateGroupContent(raw json.RawMessage, contentModes []string) error {
	var content groupContentBinding
	if contract.DecodeStrictJSON(raw, &content) != nil || !containsString(contentModes, content.Kind) {
		return ErrManagementInvalid
	}
	switch content.Kind {
	case "text":
		if content.Title == nil || content.Text == nil || strings.TrimSpace(*content.Text) == "" ||
			content.TemplateID != nil || content.Locale != nil || content.ParamsMapping != nil ||
			len(*content.Title) > 64<<10 || len(*content.Text) > 64<<10 ||
			!validNotificationTextTemplate(*content.Title) || !validNotificationTextTemplate(*content.Text) {
			return ErrManagementInvalid
		}
		return nil
	case "template":
		if content.Title != nil || content.Text != nil || content.TemplateID == nil || strings.TrimSpace(*content.TemplateID) == "" ||
			content.Locale == nil || len(*content.Locale) > 128 || content.ParamsMapping == nil || len(*content.ParamsMapping) > 100 {
			return ErrManagementInvalid
		}
		for key, path := range *content.ParamsMapping {
			if strings.TrimSpace(key) == "" || len(key) > 128 || !validNotificationVariablePath(path) {
				return ErrManagementInvalid
			}
		}
		return nil
	default:
		return ErrManagementInvalid
	}
}

func validNotificationTextTemplate(value string) bool {
	indices := placeholder.FindAllStringSubmatchIndex(value, -1)
	last := 0
	for _, index := range indices {
		if strings.Contains(value[last:index[0]], "{{") || strings.Contains(value[last:index[0]], "}}") ||
			!validNotificationVariablePath(value[index[2]:index[3]]) {
			return false
		}
		last = index[1]
	}
	return !strings.Contains(value[last:], "{{") && !strings.Contains(value[last:], "}}")
}

func validNotificationVariablePath(path string) bool {
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "payload.")
	path = strings.TrimPrefix(path, "variables.")
	return notificationVariablePath.MatchString(path)
}

func recipientMatchesChannel(kind string, channel contract.Channel) bool {
	switch channel {
	case contract.ChannelEmail:
		return kind == "email"
	case contract.ChannelSMS, contract.ChannelVoice:
		return kind == "phone"
	case contract.ChannelIM:
		return kind == "chat_id" || kind == "user_id"
	case contract.ChannelWebhook:
		return kind == "webhook"
	default:
		return false
	}
}

func groupValidationReason(err error) string {
	if err == nil {
		return ""
	}
	return "binding_incompatible_or_invalid"
}
