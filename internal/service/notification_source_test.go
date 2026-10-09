package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"project/internal/dal"
	"project/internal/model"
)

func testSourceBridge(t *testing.T, server *httptest.Server) *SourceBridge {
	t.Helper()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("parse test URL")
	}
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &SourceBridge{config: SourceBridgeConfig{Enabled: true, DeploymentID: "deployment-test", BearerToken: "source-test-token"}, base: base, client: client}
}

func testOutboxRecord(t *testing.T) dal.SourceOutboxRecord {
	t.Helper()
	occurred := time.Date(2026, 10, 9, 1, 2, 3, 0, time.UTC)
	action := "action-test-0001"
	event := sourceEvent{SchemaVersion: "1.0", SourceDeploymentID: "deployment-test", SourceEventID: "event-test-0001", ActionID: action, TenantID: "tenant-test", NotificationGroupID: "encore-group", GroupRevision: 4, OccurredAt: occurred.Format(time.RFC3339Nano), ExpiresAt: occurred.Add(24 * time.Hour).Format(time.RFC3339Nano), Payload: sourceEventPayload{Subject: "subject", Text: "body", Variables: map[string]json.RawMessage{}, LegacyAlertJSON: map[string]json.RawMessage{}}}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal("marshal source event")
	}
	digest := sha256.Sum256(body)
	return dal.SourceOutboxRecord{ID: "outbox-id", SourceDeploymentID: event.SourceDeploymentID, TenantID: event.TenantID, SourceEventID: event.SourceEventID, SourceActionID: event.ActionID, LegacyGroupID: "legacy-group", NotificationGroupID: event.NotificationGroupID, GroupRevision: event.GroupRevision, IdempotencyKey: sourceKey(event.SourceEventID, event.ActionID), RequestBody: body, BodySHA256: hex.EncodeToString(digest[:]), OccurredAt: occurred, ExpiresAt: occurred.Add(24 * time.Hour)}
}

func TestSourceRelayRetryUsesFrozenBodyAndKeyAfterLostAck(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	var keys []string
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		requests++
		current := requests
		mu.Unlock()
		if current == 1 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("test server does not support hijacking")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Error("hijack response")
				return
			}
			_, _ = conn.Write([]byte("HTTP/1.1 202 Accepted\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"code\":200"))
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"code":200,"message":"accepted","requestId":"request-1","data":{"accepted":true,"notificationId":"n-1","deliveryIds":[],"intakeStatus":"blocked"}}`))
	}))
	defer server.Close()
	bridge := testSourceBridge(t, server)
	defer bridge.Close()
	record := testOutboxRecord(t)
	first := bridge.Deliver(context.Background(), record)
	if first.Accepted || first.StatusCode != 0 {
		t.Fatalf("lost ack must request retry, got %+v", first)
	}
	second := bridge.Deliver(context.Background(), record)
	if !second.Accepted || second.StatusCode != http.StatusAccepted {
		t.Fatalf("durable 202 was not accepted: %+v", second)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || string(bodies[0]) != string(record.RequestBody) || string(bodies[1]) != string(record.RequestBody) || string(bodies[0]) != string(bodies[1]) {
		t.Fatalf("relay changed frozen event body: %q", bodies)
	}
	if len(keys) != 2 || keys[0] != record.IdempotencyKey || keys[1] != record.IdempotencyKey {
		t.Fatalf("relay changed idempotency key: %v", keys)
	}
}

func TestSourceRelayAcceptsOnlySchemaValid202AndDoesNotFollowRedirects(t *testing.T) {
	t.Run("200 is not a durable acknowledgement", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":200,"data":{"accepted":true}}`))
		}))
		defer server.Close()
		bridge := testSourceBridge(t, server)
		defer bridge.Close()
		result := bridge.Deliver(context.Background(), testOutboxRecord(t))
		if result.Accepted || result.StatusCode != http.StatusOK {
			t.Fatalf("unexpected acceptance: %+v", result)
		}
	})
	t.Run("redirect is not followed", func(t *testing.T) {
		targetCalled := false
		target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targetCalled = true; w.WriteHeader(http.StatusAccepted) }))
		defer target.Close()
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		}))
		defer server.Close()
		bridge := testSourceBridge(t, server)
		defer bridge.Close()
		result := bridge.Deliver(context.Background(), testOutboxRecord(t))
		if result.Accepted || result.StatusCode != http.StatusTemporaryRedirect || targetCalled {
			t.Fatalf("redirect handling result=%+v targetCalled=%v", result, targetCalled)
		}
	})
}

func TestSourceRelayRejectsTamperedOutboxBeforeNetwork(t *testing.T) {
	called := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusAccepted) }))
	defer server.Close()
	bridge := testSourceBridge(t, server)
	defer bridge.Close()
	record := testOutboxRecord(t)
	record.RequestBody = append(record.RequestBody, ' ')
	result := bridge.Deliver(context.Background(), record)
	if called || result.StatusCode != http.StatusUnprocessableEntity || result.SafeCode != "invalid_source_event" {
		t.Fatalf("tampered record was not blocked: %+v called=%v", result, called)
	}
}

func TestSourceEventPayloadEnforcesFrozenSchemaBounds(t *testing.T) {
	record := testOutboxRecord(t)
	var event sourceEvent
	if err := json.Unmarshal(record.RequestBody, &event); err != nil {
		t.Fatal("decode fixture source event")
	}
	if !validateSourceEvent(event) {
		t.Fatal("valid source event rejected")
	}
	event.Payload.Subject = strings.Repeat("s", 513)
	if validateSourceEvent(event) {
		t.Fatal("oversized subject accepted")
	}
	event.Payload.Subject = "subject"
	event.Payload.Text = strings.Repeat("t", 65537)
	if validateSourceEvent(event) {
		t.Fatal("oversized source text accepted")
	}
	event.Payload.Text = "text"
	deep := "null"
	for i := 0; i < 65; i++ {
		deep = `{"x":` + deep + `}`
	}
	event.Payload.LegacyAlertJSON = map[string]json.RawMessage{"nested": json.RawMessage(deep)}
	if validateSourceEvent(event) {
		t.Fatal("excessively nested legacy JSON accepted")
	}
}

func TestNewSourceBridgeRejectsUnsafeEndpointAndMissingCompatibilityCheck(t *testing.T) {
	baseConfig := SourceBridgeConfig{Enabled: true, BaseURL: "http://127.0.0.1:1234", DeploymentID: "deployment", BearerToken: "token"}
	if _, err := NewSourceBridge(baseConfig, testCompatibilityChecker{}); err == nil {
		t.Fatal("http endpoint accepted")
	}
	baseConfig.BaseURL = "https://user:pass@example.invalid"
	if _, err := NewSourceBridge(baseConfig, testCompatibilityChecker{}); err == nil {
		t.Fatal("userinfo endpoint accepted")
	}
	baseConfig.BaseURL = "https://example.invalid"
	if _, err := NewSourceBridge(baseConfig, nil); err == nil {
		t.Fatal("missing compatibility checker accepted")
	}
}

func TestSourceGroupCompatibilityRejectsWholeLegacyFanoutTypes(t *testing.T) {
	for _, kind := range []string{"APP", "EMAIL,APP", "EMAIL,VOICE", "EMAIL,,WEBHOOK", "EMAIL,EMAIL", "UNKNOWN"} {
		if sourceGroupTypesSupported(kind) {
			t.Errorf("unsupported whole-group type accepted: %q", kind)
		}
	}
	for _, kind := range []string{"EMAIL", "SME_CODE", "MEMBER", "WEBHOOK", "EMAIL,MEMBER"} {
		if !sourceGroupTypesSupported(kind) {
			t.Errorf("known type rejected before full compatibility check: %q", kind)
		}
	}
}

func TestSourceProjectionRequiresExactServerConfirmedTuple(t *testing.T) {
	request := SourceGroupProjectionRequest{SourceDeploymentID: "deployment-test", TenantID: "tenant-test", LegacyGroupID: "legacy-1", NotificationGroupID: "encore-1", GroupRevision: 3}
	key := "projection-replay-0001"
	t.Run("exact durable projection", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != sourceProjectPath {
				t.Errorf("unexpected projection request %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer source-test-token" || r.Header.Get("Idempotency-Key") != key {
				t.Error("source identity or replay key missing")
			}
			var got SourceGroupProjectionRequest
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got != request {
				t.Errorf("projection request tuple mismatch: %+v", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","requestId":"req-1","data":{"sourceDeploymentId":"deployment-test","tenantId":"tenant-test","legacyGroupId":"legacy-1","notificationGroupId":"encore-1","groupRevision":3,"createdAt":"2026-10-09T00:00:00Z"}}`))
		}))
		defer server.Close()
		bridge := testSourceBridge(t, server)
		defer bridge.Close()
		projection, err := bridge.RegisterProjection(context.Background(), request, key)
		if err != nil || projection.GroupRevision != request.GroupRevision {
			t.Fatalf("projection confirmation=%+v err=%v", projection, err)
		}
	})
	t.Run("mismatched tuple", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","requestId":"req-2","data":{"sourceDeploymentId":"other-deployment","tenantId":"tenant-test","legacyGroupId":"legacy-1","notificationGroupId":"encore-1","groupRevision":3,"createdAt":"2026-10-09T00:00:00Z"}}`))
		}))
		defer server.Close()
		bridge := testSourceBridge(t, server)
		defer bridge.Close()
		if _, err := bridge.RegisterProjection(context.Background(), request, key); err == nil {
			t.Fatal("mismatched projection response accepted")
		}
	})
}

type testCompatibilityChecker struct{}

func (testCompatibilityChecker) CheckEncoreCompatibility(_ context.Context, _ *model.NotificationGroup, _ string) error {
	return nil
}
