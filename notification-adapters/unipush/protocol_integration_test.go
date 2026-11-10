package unipush

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.app/notification-adapters/sdk"
)

// Exercises the same plugin v1 HTTP boundary used by the Encore worker; the
// directory and cloud endpoints remain fixtures, not live Community or DCloud.
func TestPluginHTTPMemberSendAndRevocation(t *testing.T) {
	cloudCalls := 0
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudCalls++
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["push_clientid"] != "cid" || body["tenant_id"] != "tenant" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 32) {
			t.Error("cloud request did not carry expected scoped data")
		}
		_, _ = w.Write([]byte(`{"errCode":0}`))
	}))
	defer cloud.Close()
	active := true
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !active {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("tenantId") != "tenant" || r.Header.Get("X-Notification-Deployment") != "deployment" {
			t.Error("directory scope missing")
		}
		_ = json.NewEncoder(w).Encode(device{ID: "binding", TenantID: "tenant", UserID: "member", AppID: "app", CID: "cid"})
	}))
	defer directory.Close()
	provider, err := New(cloud.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.ConfigureDirectory(directory.URL, strings.Repeat("d", 32), "deployment", "app", strings.Repeat("s", 32), true); err != nil {
		t.Fatal(err)
	}
	handler, err := sdk.NewHandler("test-plugin-token", provider, sdk.HandlerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	plugin := httptest.NewServer(handler)
	defer plugin.Close()
	request := sdk.SendRequest{APIVersion: "1.0", DeliveryID: "delivery", AttemptID: "attempt", TenantID: "tenant", InstanceID: "instance", ConfigVersion: 1, Channel: "im", Recipient: sdk.Recipient{Kind: "user_id", Address: "binding"}, Content: sdk.Content{Kind: "text", Text: "hello"}, Config: map[string]any{}, ExpiresAt: time.Now().Add(time.Hour)}
	body, _ := json.Marshal(request)
	call := func() sdk.SendResult {
		req, _ := http.NewRequest(http.MethodPost, plugin.URL+"/v1/send", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-plugin-token")
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var result sdk.SendResult
		if err := json.NewDecoder(res.Body).Decode(&result); err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("HTTP %d decode: %v", res.StatusCode, err)
		}
		return result
	}
	if result := call(); result.Outcome != "accepted" || cloudCalls != 1 {
		t.Fatalf("accepted=%+v calls=%d", result, cloudCalls)
	}
	active = false
	if result := call(); result.Outcome != "rejected" || cloudCalls != 1 {
		t.Fatalf("revoked=%+v calls=%d", result, cloudCalls)
	}
}
