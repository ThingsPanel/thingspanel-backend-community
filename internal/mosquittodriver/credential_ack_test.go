package mosquittodriver

import (
	"encoding/json"
	"net/http"
	bc "project/internal/brokercontract"
	"sync/atomic"
	"testing"
	"time"
)

func TestCredentialAckAfterLocalInvalidationAndRetry(t *testing.T) {
	var revoked atomic.Bool
	var attempts atomic.Int32
	received := make(chan bc.CredentialLeaseAck, 4)
	var d *Driver
	d = newTestDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/credential-leases/ack" {
			if d.Authorize(1, 2, "devices/telemetry", 1, 1, false) {
				t.Error("ACK before local WRITE revocation")
			}
			var ack bc.CredentialLeaseAck
			if json.NewDecoder(r.Body).Decode(&ack) != nil {
				t.Error("bad ACK")
			}
			received <- ack
			if attempts.Add(1) == 1 {
				w.WriteHeader(503)
			} else {
				w.WriteHeader(204)
			}
			return
		}
		if revoked.Load() {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(testGrant())
	}, 2)
	if !d.Begin(1, "client", "old-token", "", "127.0.0.1", 4) || !poll(t, d).Allow {
		t.Fatal("auth")
	}
	d.Connected(1)
	d.mu.Lock()
	issued := d.sessions[1].grant
	d.sessions[1].refreshAt = time.Now().Add(-time.Second)
	d.mu.Unlock()
	revoked.Store(true)
	d.Tick()
	select {
	case ack := <-received:
		if ack.ConnectionID != issued.ConnectionID || !ack.ExpiresAt.Equal(issued.ExpiresAt) || ack.BrokerInstance != "node-a" {
			t.Fatal("ACK did not identify exact lease")
		}
	case <-time.After(time.Second):
		t.Fatal("missing ACK")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		done := !d.sessions[1].credentialAckInFlight
		d.mu.Unlock()
		if done {
			break
		}
		time.Sleep(time.Millisecond)
	}
	d.mu.Lock()
	if !d.sessions[1].credentialAckPending {
		t.Error("failed ACK not retained")
	}
	d.sessions[1].credentialAckNext = time.Time{}
	d.mu.Unlock()
	d.Tick()
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("ACK not retried")
	}
}

func TestCredentialAckRetryDoesNotAcknowledgeReplacementGrant(t *testing.T) {
	received := make(chan bc.CredentialLeaseAck, 1)
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/credential-leases/ack" {
			var ack bc.CredentialLeaseAck
			json.NewDecoder(r.Body).Decode(&ack)
			received <- ack
			w.WriteHeader(204)
			return
		}
		json.NewEncoder(w).Encode(testGrant())
	}, 2)
	if !d.Begin(1, "client", "token", "", "127.0.0.1", 4) || !poll(t, d).Allow {
		t.Fatal("auth")
	}
	d.Connected(1)
	d.mu.Lock()
	old := d.sessions[1].grant
	d.sessions[1].credentialAck = bc.CredentialLeaseAck{BrokerCell: d.cfg.Cell, BrokerInstance: d.cfg.Instance, ConnectionID: old.ConnectionID, ExpiresAt: old.ExpiresAt}
	d.sessions[1].credentialAckPending = true
	// A refresh succeeded while the old invalidation ACK was waiting for retry.
	d.sessions[1].grant.ExpiresAt = old.ExpiresAt.Add(time.Second)
	d.startCredentialAckLocked(d.sessions[1])
	d.mu.Unlock()
	select {
	case ack := <-received:
		if !ack.ExpiresAt.Equal(old.ExpiresAt) {
			t.Fatal("ACK targets still-live replacement grant")
		}
	case <-time.After(time.Second):
		t.Fatal("missing ACK")
	}
	if !d.Authorize(1, 2, "devices/telemetry", 1, 1, false) {
		t.Fatal("replacement grant was revoked")
	}
}
