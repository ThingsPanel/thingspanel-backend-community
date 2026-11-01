package sdk

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 64 << 10

type Plugin interface {
	Manifest() Manifest
	Validate(context.Context, ValidateRequest) ValidateResult
	Send(context.Context, SendRequest) (SendResult, error)
	Health(context.Context) error
}

type HandlerOptions struct {
	MaxRequestBytes int64
	Now             func() time.Time
	// AllowedUIOrigins enables credential-free browser access only for exact
	// configured origins. The default is to emit no CORS allow headers.
	AllowedUIOrigins []string
}

func NewHandler(token string, plugin Plugin, options HandlerOptions) (http.Handler, error) {
	if token == "" || plugin == nil {
		return nil, errors.New("plugin handler requires token and provider")
	}
	if options.MaxRequestBytes <= 0 {
		options.MaxRequestBytes = DefaultMaxRequestBytes
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	origins := make(map[string]struct{}, len(options.AllowedUIOrigins))
	for _, origin := range options.AllowedUIOrigins {
		if !validOrigin(origin) {
			return nil, errors.New("plugin handler has an invalid CORS origin")
		}
		origins[origin] = struct{}{}
	}
	h := &handler{tokenHash: sha256.Sum256([]byte(token)), plugin: plugin, opts: options}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/manifest", h.manifest)
	mux.HandleFunc("/healthz", h.health)
	mux.HandleFunc("/v1/validate", h.validate)
	mux.HandleFunc("/v1/send", h.send)
	h.allowedOrigins = origins
	return h.cors(mux), nil
}

type handler struct {
	tokenHash      [32]byte
	plugin         Plugin
	opts           HandlerOptions
	allowedOrigins map[string]struct{}
}

func (h *handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Origin")
		if _, allowed := h.allowedOrigins[origin]; !allowed {
			if r.Method == http.MethodOptions {
				h.write(w, http.StatusForbidden, ProtocolError{Code: "cors_origin_denied", Message: "Origin is not allowed."})
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		if r.Method == http.MethodOptions {
			if !validPreflight(r) {
				h.write(w, http.StatusForbidden, ProtocolError{Code: "cors_preflight_denied", Message: "CORS preflight is not allowed."})
				return
			}
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validOrigin(value string) bool {
	u, err := http.NewRequest(http.MethodGet, value, nil)
	if err != nil || u.URL == nil || (u.URL.Scheme != "http" && u.URL.Scheme != "https") || u.URL.Host == "" || u.URL.User != nil || u.URL.Path != "" || u.URL.RawQuery != "" || u.URL.Fragment != "" {
		return false
	}
	return u.URL.String() == value
}

func validPreflight(r *http.Request) bool {
	if r.URL.Path != "/v1/manifest" && r.URL.Path != "/healthz" && r.URL.Path != "/v1/validate" && r.URL.Path != "/v1/send" {
		return false
	}
	method := r.Header.Get("Access-Control-Request-Method")
	if method != http.MethodGet && method != http.MethodPost {
		return false
	}
	if (r.URL.Path == "/v1/manifest" || r.URL.Path == "/healthz") && method != http.MethodGet {
		return false
	}
	if (r.URL.Path == "/v1/validate" || r.URL.Path == "/v1/send") && method != http.MethodPost {
		return false
	}
	for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
		header = strings.TrimSpace(strings.ToLower(header))
		if header != "" && header != "authorization" && header != "content-type" {
			return false
		}
	}
	return true
}

func (h *handler) authorized(r *http.Request) bool {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") || len(value) <= len("Bearer ") {
		return false
	}
	candidate := sha256.Sum256([]byte(strings.TrimPrefix(value, "Bearer ")))
	return subtle.ConstantTimeCompare(candidate[:], h.tokenHash[:]) == 1
}

func (h *handler) guard(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		h.write(w, http.StatusMethodNotAllowed, ProtocolError{Code: "method_not_allowed", Message: "Method not allowed."})
		return false
	}
	if !h.authorized(r) {
		h.write(w, http.StatusUnauthorized, ProtocolError{Code: "unauthorized", Message: "Unauthorized."})
		return false
	}
	return true
}

func (h *handler) manifest(w http.ResponseWriter, r *http.Request) {
	if !h.guard(w, r, http.MethodGet) {
		return
	}
	h.write(w, http.StatusOK, h.plugin.Manifest())
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if !h.guard(w, r, http.MethodGet) {
		return
	}
	if err := h.plugin.Health(r.Context()); err != nil {
		h.write(w, http.StatusServiceUnavailable, ProtocolError{Code: "unavailable", Message: "Plugin unavailable."})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *handler) validate(w http.ResponseWriter, r *http.Request) {
	if !h.guard(w, r, http.MethodPost) {
		return
	}
	var req ValidateRequest
	obj, err := decodeStrict(r.Body, h.opts.MaxRequestBytes, &req)
	if err != nil || !validateValidateKeys(obj) || req.APIVersion != APIVersion || req.TenantID == "" || req.InstanceID == "" || req.ConfigVersion < 1 {
		h.write(w, http.StatusBadRequest, ProtocolError{Code: "bad_request", Message: "Invalid plugin request."})
		return
	}
	result := h.plugin.Validate(r.Context(), req)
	if result.Errors == nil {
		result.Errors = []FieldError{}
	}
	if result.Valid != (len(result.Errors) == 0) {
		h.write(w, http.StatusInternalServerError, ProtocolError{Code: "invalid_plugin_response", Message: "Plugin returned an invalid response."})
		return
	}
	h.write(w, http.StatusOK, result)
}

func (h *handler) send(w http.ResponseWriter, r *http.Request) {
	if !h.guard(w, r, http.MethodPost) {
		return
	}
	var req SendRequest
	obj, err := decodeStrict(r.Body, h.opts.MaxRequestBytes, &req)
	if err != nil || !validateSendKeys(obj) || req.APIVersion != APIVersion || req.DeliveryID == "" || req.AttemptID == "" || req.TenantID == "" || req.InstanceID == "" || req.ConfigVersion < 1 || req.Channel == "" || req.Recipient.Kind == "" || req.Recipient.Address == "" || !req.ExpiresAt.After(h.opts.Now()) {
		h.write(w, http.StatusBadRequest, ProtocolError{Code: "bad_request", Message: "Invalid plugin request."})
		return
	}
	result, callErr := h.plugin.Send(r.Context(), req)
	if callErr != nil {
		result = unknownResult(req.DeliveryID, h.plugin.Manifest().Capabilities.DeliveryReceipts, "submission_uncertain", "Submission outcome is unknown; automatic retry is disabled.")
	}
	if !validSendResult(result) || result.DeliveryID != req.DeliveryID {
		result = unknownResult(req.DeliveryID, h.plugin.Manifest().Capabilities.DeliveryReceipts, "invalid_provider_response", "Submission outcome is unknown; automatic retry is disabled.")
	}
	h.write(w, http.StatusOK, result)
}

func unknownResult(deliveryID string, receiptExpected bool, code, message string) SendResult {
	return SendResult{DeliveryID: deliveryID, Outcome: "unknown", ReceiptExpected: receiptExpected, Error: &DeliveryError{Code: code, Message: message, Retryable: false}}
}

func validSendResult(result SendResult) bool {
	if result.DeliveryID == "" {
		return false
	}
	switch result.Outcome {
	case "accepted":
		return result.Error == nil && (!result.ReceiptExpected || result.ProviderMessageID != "")
	case "rejected":
		return result.Error != nil && !result.ReceiptExpected && result.ProviderMessageID == "" && (result.Error.RetryAfterSeconds == nil || result.Error.Retryable)
	case "unknown":
		return result.Error != nil && !result.Error.Retryable && result.ProviderMessageID == ""
	default:
		return false
	}
}

func (h *handler) write(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err == nil {
		body = append(body, '\n') // manifestDigest hashes these exact response bytes
	}
	if err != nil || len(body) > maxResponseBytes {
		status = http.StatusInternalServerError
		body = []byte(`{"code":"invalid_plugin_response","message":"Plugin returned an invalid response."}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = io.Copy(w, strings.NewReader(string(body)))
}
