package communityclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAuthenticatedCommandForwardsCallerAndExecutionKeyOnlyToCommandEndpoints(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("x-token") != "caller" || r.Header.Get("x-api-key") != "" {
			t.Error("wrong caller")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid command")
		}
		if body["device_id"] != "own-device" || body["idempotency_key"] != "execution-id" {
			t.Error("lost device or execution identity")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"data":{"message_id":"message"}}`))
	}))
	defer ts.Close()
	c := &Client{baseURL: ts.URL, apiKey: "privileged-key", http: ts.Client()}
	r := httptest.NewRequest("POST", "/native-command", nil)
	r.Header.Set("Authorization", "Bearer caller")
	ctx := WithAuthenticatedCommandRequest(context.Background(), r)
	message, err := c.SubmitCommand(ctx, "own-device", "switch", `{"on":true}`, "execution-id")
	if err != nil || message != "message" {
		t.Fatalf("command confirmation failed: %v", err)
	}
	for _, path := range []string{"/api/v1/device", "/api/v1/login", "/api/v1/command/datas/pub?override=true"} {
		if _, err := c.doRaw(ctx, "POST", path, "", nil); !errors.Is(err, ErrRequestAuthentication) {
			t.Fatal("unrelated mutation allowed")
		}
	}
	if _, err := c.doRaw(ctx, "DELETE", "/api/v1/command/datas/pub", "", nil); !errors.Is(err, ErrRequestAuthentication) {
		t.Fatal("wrong method allowed")
	}
	empty := WithAuthenticatedCommandRequest(context.Background(), httptest.NewRequest("POST", "/", nil))
	if _, err := c.SubmitCommand(empty, "d", "i", "", "k"); !errors.Is(err, ErrRequestAuthentication) {
		t.Fatal("missing caller fell back")
	}
	if calls.Load() != 1 {
		t.Fatalf("unexpected upstream calls: %d", calls.Load())
	}
}
