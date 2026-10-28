package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"project/internal/brokeraitools"
	"project/internal/brokercommands"
	"project/internal/brokersemantic"
	"project/internal/brokerstate"
	"project/internal/service"
)

func brokerAITestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterBrokerAIToolRoutes(router)
	return router
}

func brokerAITestRequest(router http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func assertNoStore(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control=%q, want no-store", got)
	}
}

func TestBrokerAIToolRoutesAreFixedPOSTOnly(t *testing.T) {
	router := brokerAITestRouter()
	want := []string{
		"/api/v1/broker-ai/delegations",
		"/api/v1/broker-ai/delegations/:id/revoke",
		"/api/v1/broker-ai/tools/devices.describe",
		"/api/v1/broker-ai/tools/telemetry.query",
		"/api/v1/broker-ai/tools/commands.preview",
		"/api/v1/broker-ai/tools/commands.getResult",
	}
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		if route.Method != http.MethodPost {
			t.Fatalf("unexpected method %s for %s", route.Method, route.Path)
		}
		registered[route.Path] = true
	}
	if len(registered) != len(want) {
		t.Fatalf("registered %d routes, want %d", len(registered), len(want))
	}
	for _, path := range want {
		if !registered[path] {
			t.Errorf("missing fixed route POST %s", path)
		}
	}
	for _, path := range []string{
		"/api/v1/broker-ai/tools/unknown",
		"/api/v1/broker-ai/tools/commands.submit",
	} {
		recorder := brokerAITestRequest(router, http.MethodPost, path, `{}`, nil)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("unknown tool %s status=%d, want 404", path, recorder.Code)
		}
	}
	if recorder := brokerAITestRequest(router, http.MethodGet, "/api/v1/broker-ai/tools/devices.describe", "", nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("GET tool status=%d, want 404", recorder.Code)
	}
}

func TestBrokerAIToolRejectsCredentialMixing(t *testing.T) {
	router := brokerAITestRouter()
	cases := []struct {
		name, path string
		headers    map[string]string
	}{
		{"issuer authorization header", "/api/v1/broker-ai/delegations", map[string]string{"x-token": "human-session", "Authorization": "Bearer opaque"}},
		{"issuer api key", "/api/v1/broker-ai/delegations", map[string]string{"x-token": "human-session", "x-api-key": "api-key"}},
		{"tool x-token", "/api/v1/broker-ai/tools/devices.describe", map[string]string{"Authorization": "Bearer opaque-token", "x-token": "human-session"}},
		{"tool api key", "/api/v1/broker-ai/tools/devices.describe", map[string]string{"Authorization": "Bearer opaque-token", "x-api-key": "api-key"}},
		{"tool missing bearer", "/api/v1/broker-ai/tools/devices.describe", map[string]string{"x-token": "human-session"}},
		{"tool malformed bearer", "/api/v1/broker-ai/tools/devices.describe", map[string]string{"Authorization": "Basic opaque-token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := brokerAITestRequest(router, http.MethodPost, tc.path, `{}`, tc.headers)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s, want 401", recorder.Code, recorder.Body.String())
			}
			assertNoStore(t, recorder)
			if strings.Contains(recorder.Body.String(), "opaque-token") || strings.Contains(recorder.Body.String(), "human-session") {
				t.Fatalf("credential echoed in error: %s", recorder.Body.String())
			}
		})
	}
}

func TestBrokerAIToolStrictBoundedBodies(t *testing.T) {
	router := brokerAITestRouter()
	issuerHeaders := map[string]string{"x-token": "human-session"}
	toolHeaders := map[string]string{"Authorization": "Bearer opaque-token"}
	cases := []struct {
		name, path, body string
		headers          map[string]string
	}{
		{"issuer unknown field", "/api/v1/broker-ai/delegations", `{"device_ids":["d"],"scopes":["devices.describe"],"extra":true}`, issuerHeaders},
		{"issuer duplicate field", "/api/v1/broker-ai/delegations", `{"device_ids":[],"device_ids":["d"],"scopes":["devices.describe"]}`, issuerHeaders},
		{"issuer trailing JSON", "/api/v1/broker-ai/delegations", `{"device_ids":[],"scopes":[]} {}`, issuerHeaders},
		{"tool unknown field", "/api/v1/broker-ai/tools/devices.describe", `{"device_id":"d","extra":true}`, toolHeaders},
		{"tool duplicate field", "/api/v1/broker-ai/tools/devices.describe", `{"device_id":"d","device_id":"other"}`, toolHeaders},
		{"revoke nonempty body", "/api/v1/broker-ai/delegations/grant-id/revoke", `{"token":"secret"}`, issuerHeaders},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := brokerAITestRequest(router, http.MethodPost, tc.path, tc.body, tc.headers)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s, want 400", recorder.Code, recorder.Body.String())
			}
			assertNoStore(t, recorder)
			if strings.Contains(recorder.Body.String(), "secret") || strings.Contains(recorder.Body.String(), "opaque-token") {
				t.Fatalf("sensitive input echoed: %s", recorder.Body.String())
			}
		})
	}

	oversizeIssuer := `{"device_ids":[],"scopes":[]}` + strings.Repeat(" ", maxBrokerAIToolIssuerBody)
	recorder := brokerAITestRequest(router, http.MethodPost, "/api/v1/broker-ai/delegations", oversizeIssuer, issuerHeaders)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversize issuer body status=%d, want 400", recorder.Code)
	}
	assertNoStore(t, recorder)

	oversizeTool := `{"device_id":"d"}` + strings.Repeat(" ", maxBrokerAIToolBody)
	recorder = brokerAITestRequest(router, http.MethodPost, "/api/v1/broker-ai/tools/devices.describe", oversizeTool, toolHeaders)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversize tool body status=%d, want 400", recorder.Code)
	}
	assertNoStore(t, recorder)
}

func TestBrokerAIToolRejectsMultipleCredentialHeaderValues(t *testing.T) {
	router := brokerAITestRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/broker-ai/tools/devices.describe", bytes.NewBufferString(`{}`))
	request.Header.Add("Authorization", "Bearer one")
	request.Header.Add("Authorization", "Bearer two")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", recorder.Code)
	}
	assertNoStore(t, recorder)
}

func TestBrokerAIToolErrorCategoriesAreFixed(t *testing.T) {
	cases := []struct {
		err      error
		status   int
		category string
	}{
		{brokeraitools.ErrInvalid, http.StatusBadRequest, "invalid_argument"},
		{service.ErrTelemetryEvidenceInvalid, http.StatusBadRequest, "invalid_argument"},
		{brokercommands.ErrRejected, http.StatusBadRequest, "invalid_argument"},
		{brokeraitools.ErrUnauthorized, http.StatusUnauthorized, "not_authorized"},
		{brokeraitools.ErrBudget, http.StatusTooManyRequests, "budget_exceeded"},
		{brokercommands.ErrCapacity, http.StatusTooManyRequests, "budget_exceeded"},
		{service.ErrBrokerSemanticUnsupported, http.StatusConflict, "unsupported_capability"},
		{brokersemantic.ErrCommandUnsupported, http.StatusConflict, "unsupported_capability"},
		{brokercommands.ErrNotFound, http.StatusNotFound, "not_found"},
		{brokercommands.ErrFenced, http.StatusConflict, "stale_state"},
		{brokerstate.ErrFenced, http.StatusConflict, "stale_state"},
		{brokercommands.ErrConflict, http.StatusConflict, "stale_state"},
		{errors.New("sensitive internal database detail"), http.StatusServiceUnavailable, "temporarily_unavailable"},
	}
	for _, tc := range cases {
		status, category := brokerAIToolStatus(tc.err)
		if status != tc.status || category != tc.category {
			t.Errorf("brokerAIToolStatus(%v)=(%d,%q), want (%d,%q)", tc.err, status, category, tc.status, tc.category)
		}
		if strings.Contains(category, "sensitive") {
			t.Fatalf("raw error category exposed internal detail %q", category)
		}
	}
	if _, category := brokerAIToolStatus(errors.New("db password=secret")); category != "temporarily_unavailable" {
		t.Fatalf("unexpected generic error category %q", category)
	}
}
