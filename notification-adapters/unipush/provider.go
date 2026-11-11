// Package unipush delivers tenant-scoped member notifications through UniPush.
// Device bindings are revalidated immediately before each provider request.
package unipush

import (
	"bytes"
	"context"
	"encoding/json"
	"encore.app/notification-adapters/sdk"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Provider struct {
	endpoint   string
	client     *http.Client
	directory  *Directory
	cloudToken string
}

// allowHTTP is an explicit operator opt-in for an existing HTTP deployment.
// It does not accept endpoint overrides from tenant configuration.
func New(endpoint string, allowHTTP bool) (*Provider, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(allowHTTP && u.Scheme == "http")) {
		return nil, errors.New("invalid UniPush endpoint")
	}
	return &Provider{endpoint: endpoint, client: &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{Proxy: nil, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (*Provider) Manifest() sdk.Manifest {
	return sdk.Manifest{APIVersion: sdk.APIVersion, PluginID: "thingspanel.unipush", Name: "App 通知（UniPush，多设备）", PluginVersion: "2.0.0", Channels: []string{"im"}, ContentModes: []string{"text", "template"}, ConfigSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}, RecipientSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "address"}, "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"chat_id", "user_id"}}, "address": map[string]any{"type": "string", "minLength": 1, "maxLength": 256, "title": "Device binding ID (user_id) / diagnostic CID (chat_id)"}}}, SecretFields: []string{}, Capabilities: sdk.Capabilities{DeliveryReceipts: false}}
}
func (*Provider) Validate(_ context.Context, r sdk.ValidateRequest) sdk.ValidateResult {
	if len(r.Config) != 0 {
		return sdk.ValidateResult{Valid: false, Errors: []sdk.FieldError{{Field: "config", Code: "invalid_config", Message: "The App endpoint is configured by the operator."}}}
	}
	return sdk.ValidateResult{Valid: true, Errors: []sdk.FieldError{}}
}
func (*Provider) Health(context.Context) error { return nil }
func result(id, outcome, code string) sdk.SendResult {
	return sdk.SendResult{DeliveryID: id, Outcome: outcome, ReceiptExpected: false, Error: &sdk.DeliveryError{Code: code, Message: "App push was not confirmed accepted.", Retryable: false}}
}

// UniPush requires title <20 and content <50 characters. Count UTF-16 units
// conservatively so supplementary characters also fit JavaScript/provider limits.
// Only the displayed preview is shortened; routing and alarm IDs stay intact.
func pushPreview(s string, limit int) string {
	s = strings.TrimSpace(s)
	units := 0
	for _, r := range s {
		units++
		if r > 0xffff {
			units++
		}
	}
	if units <= limit {
		return s
	}
	units = 0
	for i, r := range s {
		width := 1
		if r > 0xffff {
			width = 2
		}
		if units+width > limit-1 {
			return s[:i] + "…"
		}
		units += width
	}
	return s
}
func (p *Provider) Send(ctx context.Context, r sdk.SendRequest) (sdk.SendResult, error) {

	cid := r.Recipient.Address
	title, text := r.Content.Title, r.Content.Text
	alarmID := ""
	alarmKind := "history"
	if r.Content.Kind == "template" {
		if r.Content.Template == nil || r.Content.Template.ID != "device_alarm" {
			return result(r.DeliveryID, "rejected", "invalid_template"), nil
		}
		if kind, ok := r.Content.Template.Params["alarmKind"]; ok {
			var valid bool
			alarmKind, valid = kind.(string)
			if !valid || (alarmKind != "history" && alarmKind != "info") {
				return result(r.DeliveryID, "rejected", "invalid_alarm_kind"), nil
			}
		}
		title, _ = r.Content.Template.Params["title"].(string)
		text, _ = r.Content.Template.Params["text"].(string)
		alarmID, _ = r.Content.Template.Params["alarmId"].(string)
		if !validPart(alarmID) {
			return result(r.DeliveryID, "rejected", "invalid_alarm_id"), nil
		}
	} else if r.Content.Kind != "text" {
		return result(r.DeliveryID, "rejected", "invalid_content"), nil
	}
	if r.Channel != "im" || strings.TrimSpace(text) == "" || len(text) > 65536 || len(title) > 512 || len(r.Config) != 0 || !validPart(cid) {
		return result(r.DeliveryID, "rejected", "invalid_request"), nil
	}
	user, tenant := "", r.TenantID
	if r.Recipient.Kind == "user_id" {
		d, err := p.directory.resolve(ctx, tenant, cid)
		if err != nil {
			return result(r.DeliveryID, "rejected", "device_binding_unavailable"), nil
		}
		cid, user = d.CID, d.UserID
	} else if r.Recipient.Kind != "chat_id" {
		return result(r.DeliveryID, "rejected", "invalid_request"), nil
	}
	title = pushPreview(title, 19)
	if title == "" {
		title = "设备告警"
	}
	text = pushPreview(text, 49)
	body, _ := json.Marshal(map[string]string{"push_clientid": cid, "title": title, "content": text, "alarm_id": alarmID, "alarm_kind": alarmKind, "tenant_id": tenant, "user_id": user, "message_id": r.DeliveryID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return result(r.DeliveryID, "rejected", "invalid_request"), nil
	}
	req.Header.Set("Content-Type", "application/json")
	if p.cloudToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.cloudToken)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return result(r.DeliveryID, "unknown", "provider_unavailable"), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result(r.DeliveryID, "unknown", "provider_http_error"), nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return result(r.DeliveryID, "unknown", "invalid_response"), nil
	}
	var reply struct {
		Code    *int            `json:"code"`
		ErrCode json.RawMessage `json:"errCode"`
		Data    struct {
			ErrCode json.RawMessage `json:"errCode"`
			Data    struct {
				TaskID string `json:"taskId"`
			} `json:"data"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &reply) != nil {
		return result(r.DeliveryID, "unknown", "invalid_response"), nil
	}
	if (len(reply.ErrCode) > 0 && string(reply.ErrCode) != "0") || (len(reply.Data.ErrCode) > 0 && string(reply.Data.ErrCode) != "0") || (reply.Code != nil && *reply.Code != 200) {
		return result(r.DeliveryID, "rejected", "provider_rejected"), nil
	}
	// Bare code=200 from an old wrapper is insufficient evidence of acceptance.
	if string(reply.ErrCode) != "0" && !(reply.Code != nil && *reply.Code == 200 && string(reply.Data.ErrCode) == "0") {
		return result(r.DeliveryID, "unknown", "invalid_response"), nil
	}
	return sdk.SendResult{DeliveryID: r.DeliveryID, Outcome: "accepted", ReceiptExpected: false, ProviderMessageID: reply.Data.Data.TaskID}, nil
}
