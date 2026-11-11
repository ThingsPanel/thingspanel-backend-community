// Package legacywebhook adapts ThingsPanel's historical JSON/HMAC webhook.
package legacywebhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"encore.app/notification-adapters/sdk"
)

const (
	pluginID   = "thingspanel.legacywebhook"
	maxPayload = 1 << 20
	timeout    = 10 * time.Second
)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Provider struct {
	client  Doer
	origins map[string]struct{}
}

// New pins DNS answers for operator-approved HTTPS origins at startup. The
// configured URL remains a secret config value, but it cannot select a host
// outside this exact origin allowlist.
func New(allowedOrigins []string) (*Provider, error) {
	origins := make(map[string]struct{}, len(allowedOrigins))
	pinned := map[string][]net.IPAddr{}
	for _, raw := range allowedOrigins {
		u, err := parseOrigin(raw)
		if err != nil {
			return nil, errors.New("invalid webhook origin allowlist")
		}
		key := originKey(u)
		lookupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		ips, err := net.DefaultResolver.LookupIPAddr(lookupCtx, u.Hostname())
		cancel()
		if err != nil || len(ips) == 0 {
			return nil, errors.New("webhook origin DNS resolution failed")
		}
		for _, ip := range ips {
			if !publicUnicast(ip.IP) {
				return nil, errors.New("webhook origin resolves to a prohibited address")
			}
		}
		origins[key] = struct{}{}
		pinned[u.Host] = append([]net.IPAddr(nil), ips...)
	}
	if len(origins) == 0 {
		return nil, errors.New("webhook origin allowlist is empty")
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			ips, ok := pinned[address]
			if !ok {
				return nil, errors.New("webhook destination is not pinned")
			}
			host, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				return nil, errors.New("invalid webhook destination")
			}
			_ = host // The HTTP transport still verifies TLS against the URL hostname.
			var last error
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		},
		TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		DisableKeepAlives: true, MaxIdleConns: 0,
	}
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Provider{client: client, origins: origins}, nil
}

// newProvider is only used with explicitly injected local httptest clients.
func newProvider(client Doer, allowedOrigins []string) *Provider {
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, raw := range allowedOrigins {
		if u, err := parseOrigin(raw); err == nil {
			origins[originKey(u)] = struct{}{}
		}
	}
	return &Provider{client: client, origins: origins}
}

func (p *Provider) Manifest() sdk.Manifest {
	return sdk.Manifest{APIVersion: sdk.APIVersion, PluginID: pluginID, Name: "ThingsPanel legacy signed webhook", PluginVersion: "1.0.1",
		Channels: []string{"webhook"}, ContentModes: []string{"text"},
		ConfigSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"payload_url", "secret"}, "properties": map[string]any{
			"payload_url": map[string]any{"type": "string", "format": "uri", "description": "HTTPS endpoint under an operator-approved origin; stored as a secret."},
			"secret":      map[string]any{"type": "string", "minLength": 1},
		}},
		RecipientSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "address"}, "properties": map[string]any{"kind": map[string]any{"const": "webhook"}, "address": map[string]any{"const": "default"}}},
		SecretFields:    []string{"payload_url", "secret"}, IdentityFields: []string{"payload_url"}, Capabilities: sdk.Capabilities{DeliveryReceipts: false}}
}

func (p *Provider) Validate(_ context.Context, req sdk.ValidateRequest) sdk.ValidateResult {
	var errs []sdk.FieldError
	if !onlyKeys(req.Config, "payload_url", "secret") {
		errs = append(errs, field("config", "unknown_field", "Configuration contains unsupported fields."))
	}
	target, ok := req.Config["payload_url"].(string)
	if !ok || p.allowedURL(target) != nil {
		errs = append(errs, field("payload_url", "origin_not_allowed", "Use an HTTPS endpoint under an operator-approved origin."))
	}
	secret, ok := req.Config["secret"].(string)
	if !ok || secret == "" {
		errs = append(errs, field("secret", "required", "A signing secret is required."))
	}
	return sdk.ValidateResult{Valid: len(errs) == 0, Errors: errs}
}

func (p *Provider) Health(context.Context) error {
	if p.client == nil || len(p.origins) == 0 {
		return errors.New("provider is not configured")
	}
	return nil
}

func (p *Provider) Send(ctx context.Context, req sdk.SendRequest) (sdk.SendResult, error) {
	if req.Channel != "webhook" || req.Recipient.Kind != "webhook" || req.Recipient.Address != "default" || req.Content.Kind != "text" {
		return rejected(req.DeliveryID, "unsupported_request", "The legacy webhook accepts one configured default target and JSON text."), nil
	}
	if !onlyKeys(req.Config, "payload_url", "secret") {
		return rejected(req.DeliveryID, "invalid_configuration", "The webhook configuration is invalid."), nil
	}
	endpoint, ok := req.Config["payload_url"].(string)
	if !ok || p.allowedURL(endpoint) != nil {
		return rejected(req.DeliveryID, "origin_not_allowed", "The webhook endpoint is not allowed."), nil
	}
	secret, ok := req.Config["secret"].(string)
	if !ok || secret == "" {
		return rejected(req.DeliveryID, "invalid_configuration", "The webhook configuration is invalid."), nil
	}
	if len(req.Content.Text) > maxPayload {
		return rejected(req.DeliveryID, "payload_too_large", "The webhook JSON payload exceeds the size limit."), nil
	}
	body, err := normalizeLegacyJSON(req.Content.Text)
	if err != nil {
		return rejected(req.DeliveryID, "invalid_payload", "The webhook payload must be a JSON object."), nil
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return rejected(req.DeliveryID, "invalid_request", "The webhook request could not be created."), nil
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response, err := p.client.Do(request)
	if err != nil {
		return unknown(req.DeliveryID), nil
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return sdk.SendResult{DeliveryID: req.DeliveryID, Outcome: "accepted", ReceiptExpected: false}, nil
	}
	// The historical receiver may perform side effects before returning any
	// error status. No HTTP status is a safe basis for a second send.
	return unknown(req.DeliveryID), nil
}

func normalizeLegacyJSON(raw string) ([]byte, error) {
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil || value == nil {
		return nil, errors.New("invalid JSON object")
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(b.Bytes()), nil
}

func (p *Provider) allowedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Host == "" {
		return errors.New("invalid endpoint")
	}
	if _, ok := p.origins[originKey(u)]; !ok {
		return errors.New("origin is not allowed")
	}
	return nil
}

func parseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid origin")
	}
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "443")
	}
	if u.Hostname() == "" {
		return nil, errors.New("invalid host")
	}
	return u, nil
}
func originKey(u *url.URL) string {
	host := strings.ToLower(u.Host)
	if strings.HasSuffix(host, ":443") {
		host = strings.TrimSuffix(host, ":443")
	}
	return "https://" + host
}
func publicUnicast(ip net.IP) bool {
	parsed, err := netip.ParseAddr(ip.String())
	if err != nil || !parsed.IsGlobalUnicast() || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsMulticast() || parsed.IsUnspecified() {
		return false
	}
	if parsed.Is4() {
		for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4"} {
			if netip.MustParsePrefix(prefix).Contains(parsed) {
				return false
			}
		}
	}
	if parsed.Is6() {
		for _, prefix := range []string{"2001:db8::/32", "2001:10::/28"} {
			if netip.MustParsePrefix(prefix).Contains(parsed) {
				return false
			}
		}
	}
	return true
}
func onlyKeys(m map[string]any, allowed ...string) bool {
	set := map[string]bool{}
	for _, k := range allowed {
		set[k] = true
	}
	for k := range m {
		if !set[k] {
			return false
		}
	}
	return true
}
func field(name, code, message string) sdk.FieldError {
	return sdk.FieldError{Field: name, Code: code, Message: message}
}
func rejected(id, code, message string) sdk.SendResult {
	return sdk.SendResult{DeliveryID: id, Outcome: "rejected", Error: &sdk.DeliveryError{Code: code, Message: message, Retryable: false}}
}
func unknown(id string) sdk.SendResult {
	return sdk.SendResult{DeliveryID: id, Outcome: "unknown", Error: &sdk.DeliveryError{Code: "submission_uncertain", Message: "Submission outcome is unknown; automatic retry is disabled.", Retryable: false}}
}
