package sdk_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"plugin.local/example-notification/mock"
	"plugin.local/example-notification/sdk"
)

const token = "sentinel-plugin-token"

func testHandler(t *testing.T, provider *mock.Provider, max int64) http.Handler {
	t.Helper()
	h, err := sdk.NewHandler(token, provider, sdk.HandlerOptions{MaxRequestBytes: max, Now: func() time.Time { return time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func validSendBody() string {
	return `{"api_version":"1.0","delivery_id":"d-1","attempt_id":"a-1","tenant_id":"t-1","instance_id":"i-1","config_version":1,"channel":"im","recipient":{"kind":"webhook","address":"default"},"content":{"kind":"text","text":"hello"},"config":{},"expires_at":"2026-10-10T00:00:00Z"}`
}

func call(h http.Handler, method, path, body, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestHandlerAuthAndHealth(t *testing.T) {
	p := mock.New()
	h := testHandler(t, p, 1024)
	for _, tc := range []struct {
		auth string
		want int
	}{{"", 401}, {"Bearer wrong", 401}, {"Bearer " + token, 200}} {
		w := call(h, http.MethodGet, "/healthz", "", tc.auth)
		if w.Code != tc.want {
			t.Fatalf("auth=%q: got %d want %d", tc.auth, w.Code, tc.want)
		}
	}
	if strings.Contains(call(h, http.MethodGet, "/healthz", "", "").Body.String(), token) {
		t.Fatal("response leaked token")
	}
}

func TestSendStrictDecodingAndContractUnion(t *testing.T) {
	cases := []struct {
		name, body string
		want       int
	}{
		{"duplicate", strings.Replace(validSendBody(), `"delivery_id":"d-1"`, `"delivery_id":"d-1","delivery_id":"d-2"`, 1), 400},
		{"wrong case", strings.Replace(validSendBody(), `"delivery_id"`, `"Delivery_ID"`, 1), 400},
		{"explicit null", strings.Replace(validSendBody(), `"title"`, `"title"`, 1)[:0] + strings.Replace(validSendBody(), `"text":"hello"`, `"text":null`, 1), 400},
		{"trailing object", validSendBody() + ` {}`, 400},
		{"unknown nested recipient", strings.Replace(validSendBody(), `"address":"default"`, `"address":"default","extra":true`, 1), 400},
		{"valid", validSendBody(), 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := mock.New()
			h := testHandler(t, p, 4096)
			w := call(h, http.MethodPost, "/v1/send", tc.body, "Bearer "+token)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == 400 && p.Calls.Load() != 0 {
				t.Fatal("provider called for invalid request")
			}
		})
	}
}

func TestBodyLimit(t *testing.T) {
	p := mock.New()
	h := testHandler(t, p, 10)
	w := call(h, http.MethodPost, "/v1/send", validSendBody(), "Bearer "+token)
	if w.Code != 400 || p.Calls.Load() != 0 {
		t.Fatalf("status=%d calls=%d", w.Code, p.Calls.Load())
	}
}

func TestProviderErrorBecomesUnknownAndIsCalledOnce(t *testing.T) {
	p := mock.New()
	p.ManifestValue.Capabilities.DeliveryReceipts = true
	p.SendError = errors.New("transport error with private details")
	h := testHandler(t, p, 4096)
	w := call(h, http.MethodPost, "/v1/send", validSendBody(), "Bearer "+token)
	if w.Code != 200 {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if p.Calls.Load() != 1 {
		t.Fatalf("provider calls=%d", p.Calls.Load())
	}
	body := w.Body.String()
	for _, fragment := range []string{`"outcome":"unknown"`, `"receipt_expected":true`, `"retryable":false`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("missing %s in %s", fragment, body)
		}
	}
	if strings.Contains(body, "private details") || strings.Contains(body, token) {
		t.Fatal("provider error leaked")
	}
}

func TestInvalidProviderUnionFailsClosed(t *testing.T) {
	p := mock.New()
	p.SendValue = sdk.SendResult{Outcome: "accepted", ReceiptExpected: true}
	h := testHandler(t, p, 4096)
	w := call(h, http.MethodPost, "/v1/send", validSendBody(), "Bearer "+token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"outcome":"unknown"`) || !strings.Contains(w.Body.String(), `"retryable":false`) {
		t.Fatalf("not fail-closed: %d %s", w.Code, w.Body.String())
	}
}

func TestManifestNoSecretAndMethodGuard(t *testing.T) {
	p := mock.New()
	h := testHandler(t, p, 4096)
	w := call(h, http.MethodPost, "/v1/manifest", `{}`, "Bearer "+token)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", w.Code)
	}
	w = call(h, http.MethodGet, "/v1/manifest", "", "Bearer "+token)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"api_version":"1.0"`)) || !bytes.HasSuffix(w.Body.Bytes(), []byte("\n")) {
		t.Fatalf("bad manifest: %d %s", w.Code, w.Body.String())
	}
}

func TestCORSExactOriginAndPreflightDoesNotInvokeProvider(t *testing.T) {
	p := mock.New()
	h, err := sdk.NewHandler(token, p, sdk.HandlerOptions{AllowedUIOrigins: []string{"http://127.0.0.1:15002"}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodOptions, "/v1/manifest", nil)
	request.Header.Set("Origin", "http://127.0.0.1:15002")
	request.Header.Set("Access-Control-Request-Method", "GET")
	request.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:15002" || response.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("unexpected preflight: %d %v", response.Code, response.Header())
	}
	if p.Calls.Load() != 0 {
		t.Fatal("CORS preflight invoked provider")
	}
	if !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), "Origin") {
		t.Fatal("Vary: Origin is missing")
	}

	request = httptest.NewRequest(http.MethodOptions, "/v1/manifest", nil)
	request.Header.Set("Origin", "http://127.0.0.1:15002.evil")
	request.Header.Set("Access-Control-Request-Method", "GET")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("untrusted origin was allowed: %d %v", response.Code, response.Header())
	}
}

func TestCORSRejectsUnsupportedHeadersAndInvalidOrigin(t *testing.T) {
	if _, err := sdk.NewHandler(token, mock.New(), sdk.HandlerOptions{AllowedUIOrigins: []string{"https://trusted.example/path"}}); err == nil {
		t.Fatal("path-bearing origin should fail closed")
	}
	h, err := sdk.NewHandler(token, mock.New(), sdk.HandlerOptions{AllowedUIOrigins: []string{"https://trusted.example"}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodOptions, "/v1/send", nil)
	request.Header.Set("Origin", "https://trusted.example")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "authorization,x-forwarded-host")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unsupported header was accepted: %d", response.Code)
	}
}

func TestHealthFailureIsFixedAndDoesNotExposeCause(t *testing.T) {
	p := mock.New()
	p.HealthError = errors.New("secret=do-not-leak")
	w := call(testHandler(t, p, 4096), http.MethodGet, "/healthz", "", "Bearer "+token)
	if w.Code != 503 || strings.Contains(w.Body.String(), "do-not-leak") {
		t.Fatalf("leak/status: %d %s", w.Code, w.Body.String())
	}
}

var _ = context.Background
