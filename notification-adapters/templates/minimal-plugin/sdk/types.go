package sdk

import (
	"encoding/json"
	"time"
)

const APIVersion = "1.0"

type Manifest struct {
	APIVersion      string         `json:"api_version"`
	PluginID        string         `json:"plugin_id"`
	Name            string         `json:"name"`
	PluginVersion   string         `json:"plugin_version"`
	Channels        []string       `json:"channels"`
	ContentModes    []string       `json:"content_modes"`
	ConfigSchema    map[string]any `json:"config_schema"`
	RecipientSchema map[string]any `json:"recipient_schema"`
	SecretFields    []string       `json:"secret_fields"`
	IdentityFields  []string       `json:"identity_fields,omitempty"`
	Capabilities    Capabilities   `json:"capabilities"`
}

type Capabilities struct {
	DeliveryReceipts bool `json:"delivery_receipts"`
}

type ValidateRequest struct {
	APIVersion    string         `json:"api_version"`
	TenantID      string         `json:"tenant_id"`
	InstanceID    string         `json:"instance_id"`
	ConfigVersion int64          `json:"config_version"`
	Config        map[string]any `json:"config"`
}

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidateResult struct {
	Valid  bool         `json:"valid"`
	Errors []FieldError `json:"errors"`
}

type Recipient struct {
	Kind    string `json:"kind"`
	Address string `json:"address"`
}

type Template struct {
	ID     string         `json:"id"`
	Locale string         `json:"locale,omitempty"`
	Params map[string]any `json:"params"`
}

type Content struct {
	Kind     string    `json:"kind"`
	Title    string    `json:"title,omitempty"`
	Text     string    `json:"text,omitempty"`
	Template *Template `json:"template,omitempty"`
}

type SendRequest struct {
	APIVersion    string         `json:"api_version"`
	DeliveryID    string         `json:"delivery_id"`
	AttemptID     string         `json:"attempt_id"`
	TenantID      string         `json:"tenant_id"`
	InstanceID    string         `json:"instance_id"`
	ConfigVersion int64          `json:"config_version"`
	Channel       string         `json:"channel"`
	Recipient     Recipient      `json:"recipient"`
	Content       Content        `json:"content"`
	Config        map[string]any `json:"config"`
	ExpiresAt     time.Time      `json:"expires_at"`
}

type DeliveryError struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds *int   `json:"retry_after_seconds,omitempty"`
}

type SendResult struct {
	DeliveryID        string         `json:"delivery_id"`
	Outcome           string         `json:"outcome"`
	ProviderMessageID string         `json:"provider_message_id,omitempty"`
	ReceiptExpected   bool           `json:"receipt_expected"`
	Error             *DeliveryError `json:"error,omitempty"`
}

type Receipt struct {
	EventID           string    `json:"event_id"`
	ProviderMessageID string    `json:"provider_message_id"`
	DeliveryID        string    `json:"delivery_id,omitempty"`
	Status            string    `json:"status"`
	OccurredAt        time.Time `json:"occurred_at"`
	ProviderCode      string    `json:"provider_code,omitempty"`
	Message           string    `json:"message,omitempty"`
}

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RawJSON is useful for preserving an opaque schema document while keeping the
// SDK's own envelope types independent of any validation library.
type RawJSON = json.RawMessage
