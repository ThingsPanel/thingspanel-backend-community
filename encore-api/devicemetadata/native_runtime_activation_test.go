package devicemetadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeActivationRequiresBoundOwnerFacts(t *testing.T) {
	for _, mode := range []string{"ok", "device", "event", "inactive", "missing-time", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Runtime-Confirm-Only") != "true" || r.Header.Get("X-Automation-Tenant") != "tenant" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
					t.Error("unbound request")
				}
				var input map[string]string
				_ = json.NewDecoder(r.Body).Decode(&input)
				if input["credentialId"] != "credential" || input["eventId"] != "event" || input["deviceId"] != "device" || input["voucher"] != "" {
					t.Error("unexpected confirmation body")
				}
				if mode == "unavailable" {
					w.WriteHeader(503)
					return
				}
				data := map[string]any{"deviceId": "device", "eventId": "event", "activationState": "active", "activatedAt": "2026-11-01T00:00:00Z"}
				switch mode {
				case "device":
					data["deviceId"] = "other"
				case "event":
					data["eventId"] = "other"
				case "inactive":
					data["activationState"] = "inactive"
				case "missing-time":
					delete(data, "activatedAt")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": data})
			}))
			defer server.Close()
			t.Setenv("TP_COMMUNITY_BASE_URL", server.URL)
			t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", strings.Repeat("x", 32))
			t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "dep")
			_, err := requestRuntimeActivationConfirmation(context.Background(), "tenant", "device", "event", credentialChangeDesired{Change: "activation", CredentialID: "credential"})
			if (err == nil) != (mode == "ok") {
				t.Fatalf("mode %s: %v", mode, err)
			}
		})
	}
}
