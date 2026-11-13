package mosquittodriver

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// Exercises the real driver's cached grant and owner-refresh HTTP path. No fake
// disconnect is claimed: denied refresh revokes all delivery/WRITE permissions.
func TestCredentialRevocationRefreshInvalidatesConnectedSession(t *testing.T) {
	var revoked atomic.Bool
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if revoked.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(testGrant())
	}, 2)
	if !d.Begin(1, "client-revoke", "old-token", "", "127.0.0.1", 4) || !poll(t, d).Allow {
		t.Fatal("initial authentication failed")
	}
	d.Connected(1)
	if !d.Authorize(1, 2, "devices/telemetry", 10, 1, false) {
		t.Fatal("initial grant missing")
	}
	revoked.Store(true)
	d.mu.Lock()
	d.sessions[1].refreshAt = time.Now().Add(-time.Second)
	d.mu.Unlock()
	d.Tick()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		done := !d.sessions[1].refreshing
		d.mu.Unlock()
		if done {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if d.Authorize(1, 2, "devices/telemetry", 10, 1, false) || d.Authorize(1, 1, "devices/command/device-a/id", 10, 1, false) || d.Authorize(1, 4, "devices/command/device-a/+", 0, 1, false) {
		t.Fatal("revoked cached grant still authorizes publish, delivery or subscribe")
	}
	if !d.Begin(2, "client-reconnect", "old-token", "", "127.0.0.1", 4) || poll(t, d).Allow {
		t.Fatal("revoked credential reconnected")
	}
}
