// Package dingtalk implements the fixed-host DingTalk custom group robot API.
package dingtalk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encore.app/notification-adapters/sdk"
)

const (
	pluginID = "thingspanel.dingtalk"
	apiURL   = "https://oapi.dingtalk.com/robot/send"
	maxBody  = 64 << 10
)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Provider struct {
	client Doer
	now    func() time.Time
	url    string // injectable only by package tests; never read from instance config
}

func New() *Provider { return newProvider(newHTTPClient(), time.Now, apiURL) }

func newProvider(client Doer, now func() time.Time, endpoint string) *Provider {
	if now == nil {
		now = time.Now
	}
	return &Provider{client: client, now: now, url: endpoint}
}

func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
		DisableKeepAlives:     true,
	}
	return &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (p *Provider) Manifest() sdk.Manifest {
	return sdk.Manifest{
		APIVersion: sdk.APIVersion, PluginID: pluginID, Name: "DingTalk custom group robot",
		PluginVersion: "1.0.1", Channels: []string{"im"}, ContentModes: []string{"text"},
		ConfigSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"webhook", "secret"},
			"properties": map[string]any{
				"webhook": map[string]any{"type": "string", "format": "uri", "description": "DingTalk custom robot webhook URL; stored as a secret."},
				"secret":  map[string]any{"type": "string", "minLength": 1},
			},
		},
		RecipientSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "address"}, "properties": map[string]any{"kind": map[string]any{"const": "chat_id"}, "address": map[string]any{"const": "default"}}},
		SecretFields:    []string{"webhook", "secret"}, Capabilities: sdk.Capabilities{DeliveryReceipts: false},
		IdentityFields: []string{"webhook"},
	}
}

func (p *Provider) Validate(_ context.Context, req sdk.ValidateRequest) sdk.ValidateResult {
	var errs []sdk.FieldError
	if !onlyConfigKeys(req.Config) {
		errs = append(errs, sdk.FieldError{Field: "config", Code: "unknown_field", Message: "Configuration contains unsupported fields."})
	}
	webhook, ok := stringField(req.Config, "webhook")
	if !ok || validateWebhook(webhook) != nil {
		errs = append(errs, sdk.FieldError{Field: "webhook", Code: "invalid_webhook", Message: "Use a DingTalk custom robot webhook on the official endpoint."})
	}
	secret, ok := stringField(req.Config, "secret")
	if !ok || strings.TrimSpace(secret) == "" || len(secret) > 4096 {
		errs = append(errs, sdk.FieldError{Field: "secret", Code: "required", Message: "A signing secret is required."})
	}
	return sdk.ValidateResult{Valid: len(errs) == 0, Errors: errs}
}

func (p *Provider) Health(context.Context) error { return nil }

func (p *Provider) Send(ctx context.Context, req sdk.SendRequest) (sdk.SendResult, error) {
	if req.Channel != "im" || req.Recipient.Kind != "chat_id" || req.Recipient.Address != "default" || req.Content.Kind != "text" || strings.TrimSpace(req.Content.Text) == "" {
		return rejected(req.DeliveryID, "unsupported_request", "DingTalk accepts text messages for the configured group only."), nil
	}
	if !onlyConfigKeys(req.Config) {
		return rejected(req.DeliveryID, "invalid_configuration", "The DingTalk configuration is invalid."), nil
	}
	webhook, ok := stringField(req.Config, "webhook")
	if !ok || validateWebhook(webhook) != nil {
		return rejected(req.DeliveryID, "invalid_configuration", "The DingTalk configuration is invalid."), nil
	}
	secret, ok := stringField(req.Config, "secret")
	if !ok || secret == "" {
		return rejected(req.DeliveryID, "invalid_configuration", "The DingTalk configuration is invalid."), nil
	}
	target, err := url.Parse(webhook)
	if err != nil {
		return rejected(req.DeliveryID, "invalid_configuration", "The DingTalk configuration is invalid."), nil
	}
	query := target.Query()
	accessToken := query.Get("access_token")
	if accessToken == "" {
		return rejected(req.DeliveryID, "invalid_configuration", "The DingTalk configuration is invalid."), nil
	}
	timestamp := fmt.Sprintf("%d", p.now().UnixMilli())
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, timestamp+"\n"+secret)
	// Let url.Values.Encode percent-encode the raw base64 value exactly once.
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	endpoint, err := url.Parse(p.url)
	if err != nil {
		return unknown(req.DeliveryID), nil
	}
	endpointQuery := endpoint.Query()
	endpointQuery.Set("access_token", accessToken)
	endpointQuery.Set("timestamp", timestamp)
	endpointQuery.Set("sign", sign)
	endpoint.RawQuery = endpointQuery.Encode()
	body, err := json.Marshal(map[string]any{"msgtype": "text", "text": map[string]string{"content": req.Content.Text}})
	if err != nil {
		return rejected(req.DeliveryID, "invalid_content", "The message content is invalid."), nil
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return rejected(req.DeliveryID, "invalid_request", "The request could not be created."), nil
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		// A timeout or connection error cannot establish whether DingTalk accepted the message.
		return unknown(req.DeliveryID), nil
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(data) > maxBody || resp.StatusCode != http.StatusOK {
		return unknown(req.DeliveryID), nil
	}
	var result struct {
		ErrCode *int64 `json:"errcode"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.ErrCode == nil {
		return unknown(req.DeliveryID), nil
	}
	if *result.ErrCode != 0 {
		// Only a documented explicit non-zero business result proves rejection; raw
		// provider messages and codes are intentionally kept out of plugin errors.
		return rejected(req.DeliveryID, "provider_rejected", "DingTalk rejected the message."), nil
	}
	return sdk.SendResult{DeliveryID: req.DeliveryID, Outcome: "accepted", ReceiptExpected: false}, nil
}

func validateWebhook(raw string) error {
	if len(raw) > 4096 {
		return errors.New("invalid webhook")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "oapi.dingtalk.com") || u.Port() != "" || u.User != nil || u.Fragment != "" || u.Path != "/robot/send" {
		return errors.New("invalid webhook")
	}
	q := u.Query()
	if len(q) != 1 || q.Get("access_token") == "" || len(q.Get("access_token")) > 2048 || len(q["access_token"]) != 1 {
		return errors.New("invalid webhook")
	}
	return nil
}

func stringField(fields map[string]any, key string) (string, bool) {
	value, ok := fields[key].(string)
	return value, ok
}

func onlyConfigKeys(values map[string]any) bool {
	for key := range values {
		if key != "webhook" && key != "secret" {
			return false
		}
	}
	return true
}

func rejected(id, code, message string) sdk.SendResult {
	return sdk.SendResult{DeliveryID: id, Outcome: "rejected", ReceiptExpected: false, Error: &sdk.DeliveryError{Code: code, Message: message}}
}

func unknown(id string) sdk.SendResult {
	return sdk.SendResult{DeliveryID: id, Outcome: "unknown", ReceiptExpected: false, Error: &sdk.DeliveryError{Code: "submission_uncertain", Message: "Submission outcome is unknown; automatic retry is disabled.", Retryable: false}}
}

var _ sdk.Plugin = (*Provider)(nil)
