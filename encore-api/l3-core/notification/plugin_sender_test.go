package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func pluginSenderTestRequest() contract.PluginSendRequestV1 {
	text := "fixture message"
	return contract.PluginSendRequestV1{APIVersion: "1.0", DeliveryID: "delivery-1", AttemptID: "attempt-1", TenantID: "tenant-1", InstanceID: "instance-1", ConfigVersion: 1, Channel: "sms", Recipient: contract.Recipient{Kind: "phone", Address: "+8613800000000"}, Content: contract.PluginContentV1{Kind: "text", Text: &text}, Config: json.RawMessage(`{"secret":"fixture-only"}`), ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
}

func TestPluginSenderAmbiguousResponsesNeverRetry(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		body   string
	}{
		{"non-protocol-202", 202, `{"delivery_id":"delivery-1","outcome":"accepted","receipt_expected":false}`},
		{"non-protocol-429", 429, `{"delivery_id":"delivery-1","outcome":"rejected","receipt_expected":false,"error":{"code":"rate_limit","message":"rejected","retryable":true}}`},
		{"non-protocol-503", 503, `{}`},
		{"missing-required-boolean", 200, `{"delivery_id":"delivery-1","outcome":"accepted"}`},
		{"wrong-delivery", 200, `{"delivery_id":"other","outcome":"accepted","receipt_expected":false}`},
		{"malformed-json", 200, `{"unfinished":`},
		{"unknown-cannot-retry", 200, `{"delivery_id":"delivery-1","outcome":"unknown","receipt_expected":false,"error":{"code":"timeout","message":"echo-secret","retryable":true}}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/send" || r.Header.Get("Idempotency-Key") != "" || r.Header.Get("Authorization") != "Bearer fixture-plugin-token" {
					t.Error("unexpected send route/headers")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(scenario.status)
				_, _ = w.Write([]byte(scenario.body))
			}))
			defer server.Close()
			client, _ := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: time.Second})
			result := client.SendInstance(context.Background(), PluginRuntime{Origin: server.URL, AuthSecret: "fixture-plugin-token"}, pluginSenderTestRequest())
			if result.Outcome != "unknown" || result.Error == nil || result.Error.Retryable || calls.Load() != 1 {
				t.Fatalf("ambiguity authorized resend: %+v calls=%d", result, calls.Load())
			}
		})
	}
}

func TestPluginSenderDisconnectAfterSideEffectIsOneCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()
	client, _ := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: time.Second})
	result := client.SendInstance(context.Background(), PluginRuntime{Origin: server.URL, AuthSecret: "fixture-plugin-token"}, pluginSenderTestRequest())
	if result.Outcome != "unknown" || calls.Load() != 1 {
		t.Fatalf("disconnected side effect repeated: %+v count=%d", result, calls.Load())
	}
}

func TestPluginSenderOnlyExplicitProtocolRejectionCanRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delivery_id":"delivery-1","outcome":"rejected","receipt_expected":false,"error":{"code":"rate_limit","message":"fixture-only-secret","retryable":true,"retry_after_seconds":2}}`))
	}))
	defer server.Close()
	client, _ := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true})
	result := client.SendInstance(context.Background(), PluginRuntime{Origin: server.URL, AuthSecret: "fixture-plugin-token"}, pluginSenderTestRequest())
	if result.Outcome != "rejected" || result.Error == nil || !result.Error.Retryable || result.Error.RetryAfterSeconds == nil || *result.Error.RetryAfterSeconds != 2 || result.Error.Message == "fixture-only-secret" {
		t.Fatalf("explicit rejection mapping/sanitization invalid: %+v", result)
	}
}

func TestPluginSenderRedirectAndExpiredIntentDoNotSend(t *testing.T) {
	var redirected, submitted atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer sink.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		submitted.Add(1)
		http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, _ := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true})
	runtime := PluginRuntime{Origin: server.URL, AuthSecret: "fixture-plugin-token"}
	result := client.SendInstance(context.Background(), runtime, pluginSenderTestRequest())
	if result.Outcome != "unknown" || redirected.Load() != 0 || submitted.Load() != 1 {
		t.Fatal("redirect followed or non-unknown result")
	}
	expired := pluginSenderTestRequest()
	expired.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	_ = client.SendInstance(context.Background(), runtime, expired)
	if submitted.Load() != 1 {
		t.Fatal("expired request reached plugin")
	}
}
