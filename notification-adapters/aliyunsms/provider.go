// Package aliyunsms implements domestic Alibaba Cloud SMS using the fixed
// Dysmsapi 2017-05-25 endpoint and ACS3-HMAC-SHA256 request signing.
package aliyunsms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"encore.app/notification-adapters/sdk"
)

const (
	pluginID    = "thingspanel.aliyunsms"
	apiEndpoint = "https://dysmsapi.aliyuncs.com/"
	apiHost     = "dysmsapi.aliyuncs.com"
	apiVersion  = "2017-05-25"
	maxBody     = 64 << 10
)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Provider struct {
	client Doer
	now    func() time.Time
	newID  func() (string, error)
	ledger SendLedger
	url    string // injectable only by package tests; never read from instance config
	host   string
}

func New() *Provider { return newProvider(newHTTPClient(), time.Now, randomNonce, apiEndpoint) }

// NewWithSendLedger wires the process-shared encrypted send ledger. The
// receipt capability remains disabled until the queue consumer and core sink
// are configured alongside this same ledger handle.
func NewWithSendLedger(ledger SendLedger) (*Provider, error) {
	if ledger == nil {
		return nil, errors.New("shared encrypted SMS send ledger is required")
	}
	p := New()
	p.ledger = ledger
	return p, nil
}

func newProvider(client Doer, now func() time.Time, newID func() (string, error), endpoint string) *Provider {
	if now == nil {
		now = time.Now
	}
	if newID == nil {
		newID = randomNonce
	}
	u, _ := url.Parse(endpoint)
	host := apiHost
	if u != nil && u.Host != "" {
		host = u.Host
	}
	return &Provider{client: client, now: now, newID: newID, url: endpoint, host: host}
}

func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
		DisableKeepAlives:     true, // provider calls are non-idempotent and must never be transport-retried
	}
	return &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (p *Provider) Manifest() sdk.Manifest {
	return sdk.Manifest{
		APIVersion: sdk.APIVersion, PluginID: pluginID, Name: "Alibaba Cloud domestic SMS",
		PluginVersion: "1.0.1", Channels: []string{"sms"}, ContentModes: []string{"template"},
		ConfigSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"access_key_id", "access_key_secret", "region", "sign_name", "template_allowlist"},
			"properties": map[string]any{
				"access_key_id":      map[string]any{"type": "string", "minLength": 1},
				"access_key_secret":  map[string]any{"type": "string", "minLength": 1},
				"security_token":     map[string]any{"type": "string"},
				"region":             map[string]any{"type": "string", "pattern": "^[a-z0-9-]{3,32}$"},
				"sign_name":          map[string]any{"type": "string", "minLength": 1},
				"template_allowlist": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string", "minLength": 1}},
			},
		},
		RecipientSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "address"}, "properties": map[string]any{"kind": map[string]any{"const": "phone"}, "address": map[string]any{"type": "string", "minLength": 5, "maxLength": 24}}},
		SecretFields:    []string{"access_key_id", "access_key_secret", "security_token"},
		IdentityFields:  []string{"access_key_id", "region", "sign_name"},
		// A durable trusted queue consumer and notification-core sink are not
		// wired in this module, so this adapter cannot promise delivery receipts.
		Capabilities: sdk.Capabilities{DeliveryReceipts: false},
	}
}

func (p *Provider) Validate(_ context.Context, req sdk.ValidateRequest) sdk.ValidateResult {
	var errs []sdk.FieldError
	if !onlyKeys(req.Config, "access_key_id", "access_key_secret", "security_token", "region", "sign_name", "template_allowlist") {
		errs = append(errs, sdk.FieldError{Field: "config", Code: "unknown_field", Message: "Configuration contains unsupported fields."})
	}
	for _, key := range []string{"access_key_id", "access_key_secret", "region", "sign_name"} {
		value, ok := req.Config[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			errs = append(errs, sdk.FieldError{Field: key, Code: "required", Message: "This configuration field is required."})
		}
	}
	if token, exists := req.Config["security_token"]; exists {
		if value, ok := token.(string); !ok || strings.TrimSpace(value) == "" {
			errs = append(errs, sdk.FieldError{Field: "security_token", Code: "invalid", Message: "The optional security token must be a non-empty string when set."})
		}
	}
	region, _ := req.Config["region"].(string)
	if region != "" && !validRegion(region) {
		errs = append(errs, sdk.FieldError{Field: "region", Code: "invalid", Message: "Use a valid Alibaba Cloud region ID."})
	}
	allowlist, ok := req.Config["template_allowlist"].([]any)
	if !ok || len(allowlist) == 0 {
		errs = append(errs, sdk.FieldError{Field: "template_allowlist", Code: "required", Message: "At least one approved template code is required."})
	} else {
		seen := map[string]bool{}
		for _, item := range allowlist {
			value, ok := item.(string)
			if !ok || strings.TrimSpace(value) == "" || len(value) > 128 || seen[value] {
				errs = append(errs, sdk.FieldError{Field: "template_allowlist", Code: "invalid", Message: "Template codes must be unique non-empty strings."})
				break
			}
			seen[value] = true
		}
	}
	return sdk.ValidateResult{Valid: len(errs) == 0, Errors: errs}
}

func (p *Provider) Health(context.Context) error { return nil }

func (p *Provider) Send(ctx context.Context, req sdk.SendRequest) (sdk.SendResult, error) {
	if req.Channel != "sms" || req.Recipient.Kind != "phone" || req.Content.Kind != "template" || req.Content.Template == nil {
		return rejected(req.DeliveryID, "unsupported_request", "Alibaba Cloud SMS accepts an approved domestic template only."), nil
	}
	if req.Content.Template.Locale != "" {
		return rejected(req.DeliveryID, "unsupported_locale", "This domestic SMS API does not accept a locale field."), nil
	}
	if err := validatePhone(req.Recipient.Address); err != nil {
		return rejected(req.DeliveryID, "invalid_recipient", "The recipient is invalid."), nil
	}
	config := req.Config
	if !onlyKeys(config, "access_key_id", "access_key_secret", "security_token", "region", "sign_name", "template_allowlist") {
		return rejected(req.DeliveryID, "invalid_configuration", "The Alibaba Cloud SMS configuration is invalid."), nil
	}
	accessKeyID, okID := config["access_key_id"].(string)
	accessKeySecret, okSecret := config["access_key_secret"].(string)
	region, okRegion := config["region"].(string)
	signName, okSign := config["sign_name"].(string)
	if !okID || accessKeyID == "" || !okSecret || accessKeySecret == "" || !okRegion || !validRegion(region) || !okSign || signName == "" {
		return rejected(req.DeliveryID, "invalid_configuration", "The Alibaba Cloud SMS configuration is invalid."), nil
	}
	if token, exists := config["security_token"]; exists {
		if value, ok := token.(string); !ok || value == "" {
			return rejected(req.DeliveryID, "invalid_configuration", "The Alibaba Cloud SMS configuration is invalid."), nil
		}
	}
	if !templateAllowed(config["template_allowlist"], req.Content.Template.ID) {
		return rejected(req.DeliveryID, "template_not_allowed", "The requested SMS template is not approved for this instance."), nil
	}
	if err := validateTemplateParams(req.Content.Template.Params); err != nil {
		return rejected(req.DeliveryID, "invalid_template_params", "Template parameters must be a JSON object with scalar values."), nil
	}
	templateJSON, err := json.Marshal(req.Content.Template.Params)
	if err != nil {
		return rejected(req.DeliveryID, "invalid_template_params", "Template parameters are invalid."), nil
	}
	nonce, err := p.newID()
	if err != nil {
		return unknown(req.DeliveryID), nil
	}
	params := url.Values{
		"PhoneNumbers":  {req.Recipient.Address},
		"SignName":      {signName},
		"TemplateCode":  {req.Content.Template.ID},
		"TemplateParam": {string(templateJSON)},
	}
	canonicalQuery := canonicalQueryString(params)
	endpoint, err := url.Parse(p.url)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host != p.host || endpoint.Path != "/" || endpoint.User != nil || endpoint.Fragment != "" {
		return unknown(req.DeliveryID), nil
	}
	endpoint.RawQuery = canonicalQuery
	date := p.now().UTC().Format("2006-01-02T15:04:05Z")
	contentHash := sha256Hex(nil)
	headers := map[string]string{
		"host":                  p.host,
		"x-acs-action":          "SendSms",
		"x-acs-content-sha256":  contentHash,
		"x-acs-date":            date,
		"x-acs-signature-nonce": nonce,
		"x-acs-version":         apiVersion,
	}
	securityToken, _ := config["security_token"].(string)
	if securityToken != "" {
		headers["x-acs-security-token"] = securityToken
	}
	authorization := signV3(http.MethodPost, "/", canonicalQuery, headers, contentHash, accessKeyID, accessKeySecret)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(nil))
	if err != nil {
		return rejected(req.DeliveryID, "invalid_request", "The request could not be created."), nil
	}
	for key, value := range headers {
		if key != "host" {
			request.Header.Set(key, value)
		}
	}
	request.Host = p.host
	request.Header.Set("Authorization", authorization)
	response, err := p.client.Do(request)
	if err != nil {
		// No automatic retry: a transport error may occur after the provider accepted the SMS.
		return unknown(req.DeliveryID), nil
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil || len(data) > maxBody {
		return unknown(req.DeliveryID), nil
	}
	var result apiResponse
	if err := json.Unmarshal(data, &result); err != nil || result.Code == "" {
		return unknown(req.DeliveryID), nil
	}
	if result.Code != "OK" {
		return classifyProviderRejection(req.DeliveryID, result.Code), nil
	}
	if response.StatusCode != http.StatusOK || result.BizID == "" {
		return unknown(req.DeliveryID), nil
	}
	if p.ledger != nil {
		if err := p.ledger.RecordAccepted(SendLedgerRecord{
			QueueNamespace: queueNamespace(accessKeyID, region), BizID: result.BizID,
			Phone: req.Recipient.Address, TenantID: req.TenantID, InstanceID: req.InstanceID,
			DeliveryID: req.DeliveryID, ConfigVersion: req.ConfigVersion, SentAt: p.now().UTC(),
		}); err != nil {
			// Alibaba has accepted the SMS. A failed local correlation write is
			// ambiguous and must never become retryable.
			return unknown(req.DeliveryID), nil
		}
	}
	return sdk.SendResult{DeliveryID: req.DeliveryID, Outcome: "accepted", ProviderMessageID: result.BizID, ReceiptExpected: false}, nil
}

type apiResponse struct {
	Code      string `json:"Code"`
	BizID     string `json:"BizId"`
	RequestID string `json:"RequestId"`
}

func signV3(method, path, query string, headers map[string]string, payloadHash, accessKeyID, secret string) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		lower := strings.ToLower(key)
		if lower == "host" || lower == "content-type" || strings.HasPrefix(lower, "x-acs-") {
			keys = append(keys, lower)
		}
	}
	sort.Strings(keys)
	var canonicalHeaders strings.Builder
	for _, key := range keys {
		canonicalHeaders.WriteString(key)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.TrimSpace(headerValue(headers, key)))
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(keys, ";")
	canonicalRequest := strings.Join([]string{strings.ToUpper(method), path, query, canonicalHeaders.String(), signedHeaders, payloadHash}, "\n")
	canonicalHash := sha256Hex([]byte(canonicalRequest))
	stringToSign := "ACS3-HMAC-SHA256\n" + canonicalHash
	mac := hmacSHA256([]byte(secret), []byte(stringToSign))
	return fmt.Sprintf("ACS3-HMAC-SHA256 Credential=%s,SignedHeaders=%s,Signature=%s", accessKeyID, signedHeaders, hex.EncodeToString(mac))
}

func canonicalQueryString(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		items := append([]string(nil), values[key]...)
		sort.Strings(items)
		for _, value := range items {
			parts = append(parts, rfc3986(key)+"="+rfc3986(value))
		}
	}
	return strings.Join(parts, "&")
}

func rfc3986(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(value), "+", "%20"), "%7E", "~"), "*", "%2A")
}

func headerValue(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func hmacSHA256(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

func randomNonce() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

func validRegion(value string) bool {
	if len(value) < 3 || len(value) > 32 || !strings.HasPrefix(value, "cn-") {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

func validatePhone(value string) error {
	if len(value) < 5 || len(value) > 24 {
		return errors.New("invalid phone")
	}
	for i, r := range value {
		if r == '+' && i == 0 {
			continue
		}
		if r < '0' || r > '9' {
			return errors.New("invalid phone")
		}
	}
	return nil
}

func validateTemplateParams(params map[string]any) error {
	if params == nil {
		return errors.New("params required")
	}
	for _, value := range params {
		switch value.(type) {
		case string, bool, json.Number, float64, int, int64, uint64:
		default:
			return errors.New("non-scalar parameter")
		}
	}
	return nil
}

func templateAllowed(raw any, templateID string) bool {
	items, ok := raw.([]any)
	if !ok || templateID == "" {
		return false
	}
	for _, item := range items {
		if value, ok := item.(string); ok && value == templateID {
			return true
		}
	}
	return false
}

func onlyKeys(values map[string]any, allowed ...string) bool {
	set := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		set[key] = true
	}
	for key := range values {
		if !set[key] {
			return false
		}
	}
	return true
}

func classifyProviderRejection(id, providerCode string) sdk.SendResult {
	code := "provider_rejected"
	retryable := false
	if providerCode == "isv.BUSINESS_LIMIT_CONTROL" {
		code = "provider_rate_limited"
		retryable = true
	}
	return sdk.SendResult{DeliveryID: id, Outcome: "rejected", ReceiptExpected: false, Error: &sdk.DeliveryError{Code: code, Message: "Alibaba Cloud rejected the SMS request.", Retryable: retryable}}
}

func rejected(id, code, message string) sdk.SendResult {
	return sdk.SendResult{DeliveryID: id, Outcome: "rejected", ReceiptExpected: false, Error: &sdk.DeliveryError{Code: code, Message: message}}
}

func unknown(id string) sdk.SendResult {
	return sdk.SendResult{DeliveryID: id, Outcome: "unknown", ReceiptExpected: false, Error: &sdk.DeliveryError{Code: "submission_uncertain", Message: "Submission outcome is unknown; automatic retry is disabled.", Retryable: false}}
}

var _ sdk.Plugin = (*Provider)(nil)
