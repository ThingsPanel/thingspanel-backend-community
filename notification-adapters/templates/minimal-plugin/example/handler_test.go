package example

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"plugin.local/example-notification/sdk"
)

func TestHTTPContractRoutesToLocalOnlyProvider(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	handler, err := sdk.NewHandler("local-fixture-token", Provider{}, sdk.HandlerOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal("handler initialization failed")
	}
	body := `{"api_version":"1.0","delivery_id":"fixture-1","attempt_id":"attempt-1","tenant_id":"tenant-1","instance_id":"instance-1","config_version":1,"channel":"email","recipient":{"kind":"email","address":"operator@example.test"},"content":{"kind":"text","text":"fixture only"},"config":{"label":"fixture"},"expires_at":"2030-01-02T00:00:00Z"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer local-fixture-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	var result sdk.SendResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.DeliveryID != "fixture-1" || result.Outcome != "rejected" || result.Error == nil || result.Error.Code != "example_no_external_send" {
		t.Fatalf("unexpected local-only result: %+v", result)
	}
	if strings.Contains(response.Body.String(), "operator@example.test") || strings.Contains(response.Body.String(), "fixture only") {
		t.Fatal("response leaked recipient or content")
	}
}
