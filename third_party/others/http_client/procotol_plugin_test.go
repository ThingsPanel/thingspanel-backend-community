package http_client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotificationUsesCanonicalPluginEndpoint(t *testing.T) {
	t.Parallel()

	const (
		messageType = "1"
		message     = `{"service_access_id":"access-1"}`
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v1/plugin/notification" {
			t.Errorf("unexpected notification path: %s", r.URL.Path)
		}

		var req struct {
			MessageType string `json:"message_type"`
			Message     string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode notification request: %v", err)
		}
		if req.MessageType != messageType {
			t.Errorf("unexpected message_type: %s", req.MessageType)
		}
		if req.Message != message {
			t.Errorf("unexpected message: %s", req.Message)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")
	if _, err := Notification(messageType, message, host); err != nil {
		t.Fatalf("Notification returned error: %v", err)
	}
}
