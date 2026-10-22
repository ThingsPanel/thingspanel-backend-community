package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"project/pkg/global"
)

// This is an opt-in fault experiment against an isolated approved PostgreSQL
// schema and a TLS mock. The mock commits the first request as a durable
// acceptance, then drops only its HTTP response to model an ACK lost in transit.
func TestSourceRelayDurableAckLostThenRestartReplaysFrozenIntent(t *testing.T) {
	openSourceCompatPGFixture(t)

	record := testOutboxRecord(t)
	record.ID = uuid.NewString()
	record.State = "pending"
	record.Attempts = 0
	record.NextAttemptAt = time.Now().UTC().Add(-time.Second)
	record.CreatedAt = time.Now().UTC()
	record.UpdatedAt = record.CreatedAt
	if err := global.DB.Create(&record).Error; err != nil {
		t.Fatal("could not seed frozen source outbox record")
	}

	type acceptedIntent struct {
		body         []byte
		notification string
		delivery     string
	}
	var mu sync.Mutex
	accepted := map[string]acceptedIntent{}
	var attempts int
	var droppedAck bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != sourceEventPath {
			t.Errorf("unexpected relay endpoint %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer source-test-token" {
			t.Error("relay request did not use source-purpose authentication")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error("could not read source event body")
		}
		key := r.Header.Get("Idempotency-Key")
		if key != record.IdempotencyKey || string(body) != string(record.RequestBody) {
			t.Error("retry changed the frozen request key or body")
		}

		mu.Lock()
		attempts++
		intent, exists := accepted[key]
		if !exists {
			intent = acceptedIntent{body: append([]byte(nil), body...), notification: "notification-stable", delivery: "delivery-stable"}
			accepted[key] = intent // Simulated Encore transaction committed before ACK.
		}
		shouldDrop := !droppedAck
		if shouldDrop {
			droppedAck = true
		}
		mu.Unlock()
		if shouldDrop {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("TLS test server cannot hijack response")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Error("could not inject response loss")
				return
			}
			partial := `{"code":200,"message":"accepted","requestId":"request-stable","data":{"accepted":true`
			_, _ = fmt.Fprintf(conn, "HTTP/1.1 202 Accepted\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(partial)+80, partial)
			_ = conn.Close()
			return
		}
		response, _ := json.Marshal(sourceEnvelope[sourceAccepted]{
			Code: 200, Message: "accepted", RequestID: "request-stable",
			Data: sourceAccepted{Accepted: true, NotificationID: intent.notification, DeliveryIDs: []string{intent.delivery}, IntakeStatus: "ready"},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write(response)
	}))
	defer server.Close()

	bridge := testSourceBridge(t, server)
	if err := bridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("first relay pass failed")
	}
	var state string
	var count int
	if err := global.DB.Raw(`SELECT state, attempts FROM notification_source_outbox WHERE id = ?`, record.ID).Row().Scan(&state, &count); err != nil || state != "retry_wait" || count != 1 {
		t.Fatalf("lost ACK was not retained for retry: state=%q attempts=%d", state, count)
	}
	if err := global.DB.Exec(`UPDATE notification_source_outbox SET next_attempt_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Second), record.ID).Error; err != nil {
		t.Fatal("could not make retry immediately eligible")
	}
	bridge.Close()

	// A new client/bridge instance models process restart. The database remains
	// the source of truth for the frozen event, lease, and idempotency key.
	restarted := testSourceBridge(t, server)
	defer restarted.Close()
	if err := restarted.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("restarted relay pass failed")
	}
	if err := global.DB.Raw(`SELECT state, attempts FROM notification_source_outbox WHERE id = ?`, record.ID).Row().Scan(&state, &count); err != nil || state != "handed_off" || count != 2 {
		t.Fatalf("restarted relay did not persist durable handoff: state=%q attempts=%d", state, count)
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts != 2 || len(accepted) != 1 {
		t.Fatalf("expected two HTTP attempts but one durable accepted intent; attempts=%d accepted=%d", attempts, len(accepted))
	}
	intent := accepted[record.IdempotencyKey]
	digest := sha256.Sum256(record.RequestBody)
	if string(intent.body) != string(record.RequestBody) || hex.EncodeToString(digest[:]) != record.BodySHA256 || intent.notification != "notification-stable" || intent.delivery != "delivery-stable" {
		t.Fatal("durable acceptance did not retain the original event identity")
	}
}
