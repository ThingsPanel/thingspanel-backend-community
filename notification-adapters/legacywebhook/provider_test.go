package legacywebhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"encore.app/notification-adapters/sdk"
)

func TestLegacyJSONFixtureAndSignature(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		got, _ := io.ReadAll(r.Body)
		want := []byte(`{"amp":"A&B","name":"<cold>"}`)
		if string(got) != string(want) {
			t.Errorf("payload=%q want=%q", got, want)
		}
		mac := hmac.New(sha256.New, []byte("fixture-secret"))
		_, _ = mac.Write(want)
		if got := r.Header.Get("X-Signature-256"); got != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			t.Errorf("signature mismatch: %q", got)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type=%q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	p := newProvider(server.Client(), []string{server.URL})
	result, err := p.Send(context.Background(), webhookRequest(server.URL+"/alerts", `{"name":"<cold>","amp":"A&B"}`))
	if err != nil || result.Outcome != "accepted" || result.ReceiptExpected || calls.Load() != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestEveryNon2xxIsUnknownAndNeverRetried(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusBadRequest, http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(status) }))
			defer server.Close()
			p := newProvider(noRedirectClient(server), []string{server.URL})
			res, err := p.Send(context.Background(), webhookRequest(server.URL+"/", `{"ok":true}`))
			if err != nil || res.Outcome != "unknown" || res.Error == nil || res.Error.Retryable || calls.Load() != 1 {
				t.Fatalf("result=%+v err=%v calls=%d", res, err, calls.Load())
			}
		})
	}
}

func TestRedirectNotFollowedAndOriginIsFixed(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	var sourceCalls atomic.Int32
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	p := newProvider(noRedirectClient(source), []string{source.URL})
	res, _ := p.Send(context.Background(), webhookRequest(source.URL+"/", `{"ok":true}`))
	if res.Outcome != "unknown" || sourceCalls.Load() != 1 || targetCalls.Load() != 0 {
		t.Fatalf("result=%+v source=%d target=%d", res, sourceCalls.Load(), targetCalls.Load())
	}
	bad := webhookRequest(target.URL+"/", `{"ok":true}`)
	res, _ = p.Send(context.Background(), bad)
	if res.Outcome != "rejected" || sourceCalls.Load() != 1 {
		t.Fatalf("unapproved origin reached network: %+v", res)
	}
}

func TestConnectionLossAfterRequestIsUnknownAndNeverRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	p := newProvider(noRedirectClient(server), []string{server.URL})
	res, err := p.Send(context.Background(), webhookRequest(server.URL+"/", `{"side_effect_possible":true}`))
	if err != nil || res.Outcome != "unknown" || res.Error == nil || res.Error.Retryable || calls.Load() != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", res, err, calls.Load())
	}
}

func TestInvalidAndOversizedPayloadFailBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	p := newProvider(server.Client(), []string{server.URL})
	for _, payload := range []string{`[]`, strings.Repeat("x", maxPayload+1)} {
		res, _ := p.Send(context.Background(), webhookRequest(server.URL+"/", payload))
		if res.Outcome != "rejected" {
			t.Fatalf("unexpected result: %+v", res)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid payload reached network %d times", calls.Load())
	}
}

func TestProductionConstructorRejectsLoopbackAndPrivateOrigin(t *testing.T) {
	if _, err := New([]string{"https://127.0.0.1"}); err == nil {
		t.Fatal("loopback origin accepted")
	}
}

func webhookRequest(url, payload string) sdk.SendRequest {
	return sdk.SendRequest{DeliveryID: "d-legacy", Channel: "webhook", Recipient: sdk.Recipient{Kind: "webhook", Address: "default"}, Content: sdk.Content{Kind: "text", Text: payload}, Config: map[string]any{"payload_url": url, "secret": "fixture-secret"}}
}
func noRedirectClient(s *httptest.Server) *http.Client {
	c := s.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}
