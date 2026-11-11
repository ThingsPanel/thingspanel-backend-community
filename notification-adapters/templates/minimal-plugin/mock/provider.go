package mock

import (
	"context"
	"sync/atomic"

	"plugin.local/example-notification/sdk"
)

type Provider struct {
	ManifestValue sdk.Manifest
	ValidateValue sdk.ValidateResult
	SendValue     sdk.SendResult
	SendError     error
	HealthError   error
	Calls         atomic.Int64
}

func (p *Provider) Manifest() sdk.Manifest { return p.ManifestValue }
func (p *Provider) Validate(context.Context, sdk.ValidateRequest) sdk.ValidateResult {
	return p.ValidateValue
}
func (p *Provider) Send(_ context.Context, req sdk.SendRequest) (sdk.SendResult, error) {
	p.Calls.Add(1)
	result := p.SendValue
	if result.DeliveryID == "" {
		result.DeliveryID = req.DeliveryID
	}
	return result, p.SendError
}
func (p *Provider) Health(context.Context) error { return p.HealthError }

func New() *Provider {
	return &Provider{
		ManifestValue: sdk.Manifest{
			APIVersion: sdk.APIVersion, PluginID: "mock.notification", Name: "Notification Mock", PluginVersion: "1.0.0",
			Channels: []string{"email", "sms", "voice", "im", "webhook"}, ContentModes: []string{"text", "template"},
			ConfigSchema:    map[string]any{"type": "object", "additionalProperties": true},
			RecipientSchema: map[string]any{"type": "object", "additionalProperties": true},
			SecretFields:    []string{}, Capabilities: sdk.Capabilities{DeliveryReceipts: false},
		},
		ValidateValue: sdk.ValidateResult{Valid: true, Errors: []sdk.FieldError{}},
		SendValue:     sdk.SendResult{Outcome: "accepted", ReceiptExpected: false},
	}
}
