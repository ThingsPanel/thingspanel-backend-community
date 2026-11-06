package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSourceBridgeAcquiresManagedLeasesAndRefreshes(t *testing.T) {
	const identity = "test-installation-identity-not-a-real-secret"
	leases, sends := 0, 0
	scopes := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tenant  string `json:"tenantId"`
			Purpose string `json:"purpose"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("bad request")
		}
		if r.URL.Path == "/internal/notification/source-credentials" {
			if r.Header.Get("Authorization") != "Bearer "+identity || r.Header.Get("X-Notification-Deployment") != "deployment-a" {
				t.Error("machine identity lost")
			}
			leases++
			scopes[body.Tenant+"/"+body.Purpose]++
			json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": sourceCredentialLease{Credential: strings.Repeat("x", 32) + body.Tenant + body.Purpose, TenantID: body.Tenant, DeploymentID: "deployment-a", Purpose: body.Purpose, ExpiresAt: time.Now().Add(15 * time.Minute)}})
			return
		}
		sends++
		purpose := "source"
		if r.URL.Path == sourceProjectPath {
			purpose = "projection"
		}
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32)+body.Tenant+purpose {
			t.Error("wrong credential scope")
		}
		if r.Header.Get("Idempotency-Key") != "stable-event-0001" {
			t.Error("idempotency key changed")
		}
		if sends == 1 {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	bridge, err := NewSourceBridge(SourceBridgeConfig{Enabled: true, BaseURL: server.URL, DeploymentID: "deployment-a", ServiceIdentity: identity}, testCompatibilityChecker{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ tenant, path string }{{"a", sourceEventPath}, {"a", sourceProjectPath}, {"b", sourceEventPath}} {
		response, _, err := bridge.postJSON(context.Background(), tc.path, "stable-event-0001", []byte(`{"tenantId":"`+tc.tenant+`"}`))
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("managed bridge failed: %v", err)
		}
	}
	if sends != 4 || leases != 4 || scopes["a/source"] != 2 || scopes["a/projection"] != 1 || scopes["b/source"] != 1 {
		t.Fatalf("scope or refresh mismatch: sends=%d leases=%d scopes=%v", sends, leases, scopes)
	}
}
