package devicemetadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeLifecycleRequiresExactOwnerReceipt(t *testing.T) {
	item := runtimeOutboxItem{EventID: "event", DeviceID: "device", TenantID: "tenant", Generation: 3, Kind: operationKindDeviceRenumber}
	for _, mode := range []string{"ok", "wrong-event", "wrong-device", "wrong-generation", "denied", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/automation/runtime-devices/lifecycle" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) || r.Header.Get("X-Automation-Tenant") != "tenant" || r.Header.Get("X-Automation-Deployment") != "dep" {
					t.Error("missing owner identity")
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["deviceNumber"] != "new" {
					t.Error("wrong payload")
				}
				if mode == "denied" {
					w.WriteHeader(403)
					return
				}
				if mode == "redirect" {
					w.Header().Set("Location", "/other")
					w.WriteHeader(302)
					return
				}
				data := map[string]any{"deviceId": item.DeviceID, "eventId": item.EventID, "generation": item.Generation}
				if mode == "wrong-event" {
					data["eventId"] = "other"
				}
				if mode == "wrong-device" {
					data["deviceId"] = "other"
				}
				if mode == "wrong-generation" {
					data["generation"] = 2
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": data})
			}))
			defer server.Close()
			t.Setenv("TP_COMMUNITY_BASE_URL", server.URL)
			t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", strings.Repeat("x", 32))
			t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "dep")
			err := applyNativeRuntimeLifecycle(context.Background(), item, "new")
			if (err == nil) != (mode == "ok") {
				t.Fatalf("receipt mode %s err=%v", mode, err)
			}
		})
	}
}
