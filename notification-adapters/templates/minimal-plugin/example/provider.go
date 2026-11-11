package example

import (
	"context"
	"strings"

	"plugin.local/example-notification/sdk"
)

// Provider demonstrates the plugin contract without contacting an external
// notification service. Send always returns a definitive local rejection.
type Provider struct{}

func (Provider) Manifest() sdk.Manifest {
	return sdk.Manifest{
		APIVersion: sdk.APIVersion, PluginID: "example.local.notification", Name: "Local Example",
		PluginVersion: "0.1.0", Channels: []string{"email"}, ContentModes: []string{"text"},
		ConfigSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"required":   []string{"label"},
			"properties": map[string]any{"label": map[string]any{"type": "string", "minLength": 1}},
		},
		RecipientSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"kind", "address"},
			"properties": map[string]any{
				"kind":    map[string]any{"const": "email"},
				"address": map[string]any{"type": "string", "format": "email"},
			},
		},
		SecretFields: []string{}, Capabilities: sdk.Capabilities{DeliveryReceipts: false},
	}
}

func (Provider) Validate(_ context.Context, req sdk.ValidateRequest) sdk.ValidateResult {
	label, ok := req.Config["label"].(string)
	if ok && strings.TrimSpace(label) != "" && len(req.Config) == 1 {
		return sdk.ValidateResult{Valid: true, Errors: []sdk.FieldError{}}
	}
	return sdk.ValidateResult{Valid: false, Errors: []sdk.FieldError{{Field: "label", Code: "required", Message: "A non-empty label is required."}}}
}

func (Provider) Send(_ context.Context, req sdk.SendRequest) (sdk.SendResult, error) {
	return sdk.SendResult{
		DeliveryID:      req.DeliveryID,
		Outcome:         "rejected",
		ReceiptExpected: false,
		Error: &sdk.DeliveryError{
			Code: "example_no_external_send", Message: "The example never contacts an external service.", Retryable: false,
		},
	}, nil
}

func (Provider) Health(context.Context) error { return nil }
