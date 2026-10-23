package contract

import "encoding/json"

// Plugin protocol v1 intentionally retains snake_case wire names. It is a
// language-neutral HTTP contract and is not an Encore-generated API schema.
type PluginManifestV1 struct {
	APIVersion      string               `json:"api_version"`
	PluginID        string               `json:"plugin_id"`
	Name            string               `json:"name"`
	PluginVersion   string               `json:"plugin_version"`
	IdentityFields  []string             `json:"identity_fields,omitempty"`
	Channels        []Channel            `json:"channels"`
	ContentModes    []string             `json:"content_modes"`
	ConfigSchema    json.RawMessage      `json:"config_schema" strictjson:"dynamic-object"`
	RecipientSchema json.RawMessage      `json:"recipient_schema" strictjson:"dynamic-object"`
	SecretFields    []string             `json:"secret_fields"`
	Capabilities    PluginCapabilitiesV1 `json:"capabilities"`
}

type PluginCapabilitiesV1 struct {
	DeliveryReceipts bool `json:"delivery_receipts"`
}

type PluginValidateRequestV1 struct {
	APIVersion    string          `json:"api_version"`
	TenantID      string          `json:"tenant_id"`
	InstanceID    string          `json:"instance_id"`
	ConfigVersion int64           `json:"config_version"`
	Config        json.RawMessage `json:"config" encore:"sensitive" strictjson:"dynamic-object"`
}

type PluginValidateResultV1 struct {
	Valid  bool                 `json:"valid"`
	Errors []PluginFieldErrorV1 `json:"errors"`
}

type PluginFieldErrorV1 struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PluginSendRequestV1 struct {
	APIVersion    string          `json:"api_version"`
	DeliveryID    string          `json:"delivery_id"`
	AttemptID     string          `json:"attempt_id"`
	TenantID      string          `json:"tenant_id"`
	InstanceID    string          `json:"instance_id"`
	ConfigVersion int64           `json:"config_version"`
	Channel       Channel         `json:"channel"`
	Recipient     Recipient       `json:"recipient"`
	Content       PluginContentV1 `json:"content"`
	Config        json.RawMessage `json:"config" encore:"sensitive" strictjson:"dynamic-object"`
	ExpiresAt     string          `json:"expires_at"`
}

type PluginContentV1 struct {
	Kind     string            `json:"kind"`
	Title    *string           `json:"title,omitempty"`
	Text     *string           `json:"text,omitempty"`
	Template *PluginTemplateV1 `json:"template,omitempty"`
}

type PluginTemplateV1 struct {
	ID     string                     `json:"id"`
	Locale *string                    `json:"locale,omitempty"`
	Params map[string]json.RawMessage `json:"params" strictjson:"dynamic-values"`
}

type PluginSendResultV1 struct {
	DeliveryID        string                 `json:"delivery_id"`
	Outcome           string                 `json:"outcome"`
	ProviderMessageID *string                `json:"provider_message_id,omitempty"`
	ReceiptExpected   bool                   `json:"receipt_expected"`
	Error             *PluginDeliveryErrorV1 `json:"error,omitempty"`
}

type PluginDeliveryErrorV1 struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds *int   `json:"retry_after_seconds,omitempty"`
}

type PluginReceiptV1 struct {
	EventID           string  `json:"event_id"`
	ProviderMessageID string  `json:"provider_message_id"`
	DeliveryID        *string `json:"delivery_id,omitempty"`
	Status            string  `json:"status"`
	OccurredAt        string  `json:"occurred_at"`
	ProviderCode      *string `json:"provider_code,omitempty"`
	Message           *string `json:"message,omitempty"`
}

type PluginProtocolErrorV1 struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
