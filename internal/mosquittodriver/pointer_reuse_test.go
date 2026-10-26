package mosquittodriver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func waitForAuthWorkers(t *testing.T, d *Driver, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		got := d.activeAuth
		d.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	d.mu.Lock()
	got := d.activeAuth
	d.mu.Unlock()
	t.Fatalf("active auth workers = %d, want %d", got, want)
}

func TestPointerReuseCancelsOldRequestAndDiscardsLateAllow(t *testing.T) {
	oldStarted := make(chan struct{})
	oldCanceled := make(chan struct{})
	releaseOld := make(chan struct{})
	var oldOnce = make(chan struct{}, 1)
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WireClientID string `json:"wire_client_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if req.WireClientID == "old-client" {
			oldOnce <- struct{}{}
			close(oldStarted)
			<-r.Context().Done()
			close(oldCanceled)
			<-releaseOld // Simulate an owner that finishes late after cancellation.
			_ = json.NewEncoder(w).Encode(testGrant())
			return
		}
		_ = json.NewEncoder(w).Encode(testGrant())
	}, 4)

	const ptr = uintptr(0x1234)
	if !d.Begin(ptr, "old-client", "u-old", "secret", "peer", 4) {
		t.Fatal("first Begin rejected")
	}
	select {
	case <-oldStarted:
	case <-time.After(time.Second):
		t.Fatal("old request did not reach owner")
	}
	if !d.Begin(ptr, "new-client", "u-new", "secret", "peer", 4) {
		t.Fatal("pointer reuse rejected")
	}
	select {
	case <-oldCanceled:
	case <-time.After(time.Second):
		t.Fatal("pointer reuse did not cancel old owner request")
	}

	c := poll(t, d)
	if !c.Allow || c.ID != "new-client" || c.Pointer != ptr {
		t.Fatalf("wrong completion after reuse: %+v", c)
	}
	d.Connected(ptr)
	close(releaseOld)
	waitForAuthWorkers(t, d, 0)
	if c := d.Poll(); c != nil {
		t.Fatalf("late old allow produced completion: %+v", c)
	}
	if !d.Authorize(ptr, 2, "devices/telemetry", 1, 1, false) {
		t.Fatal("late old result displaced new session")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.sessions) != 1 || len(d.pending) != 0 {
		t.Fatalf("unexpected maps after reuse: sessions=%d pending=%d", len(d.sessions), len(d.pending))
	}
}

func TestPointerReuseHighFrequencyStaysBounded(t *testing.T) {
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(testGrant())
	}, 2)
	const ptr = uintptr(0x5678)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("reuse-%04d", i)
		if !d.Begin(ptr, id, "u", "p", "peer", 4) {
			t.Fatalf("Begin %d rejected", i)
		}
		c := poll(t, d)
		if !c.Allow || c.ID != id || c.Pointer != ptr {
			t.Fatalf("iteration %d got wrong completion: %+v", i, c)
		}
		d.mu.Lock()
		pending, sessions, active := len(d.pending), len(d.sessions), d.activeAuth
		d.mu.Unlock()
		if pending != 0 || sessions > 1 || active > 1 {
			t.Fatalf("iteration %d leaked state: pending=%d sessions=%d active=%d", i, pending, sessions, active)
		}
	}
	waitForAuthWorkers(t, d, 0)
	if !d.Authorize(ptr, 2, "devices/telemetry", 1, 1, false) {
		t.Fatal("final session lost authorization")
	}
}

func TestPointerReuseCannotLeaveOldPendingClientID(t *testing.T) {
	oldStarted := make(chan struct{})
	release := make(chan struct{})
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WireClientID string `json:"wire_client_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.WireClientID == "old" {
			close(oldStarted)
			<-release
		}
		_ = json.NewEncoder(w).Encode(testGrant())
	}, 2)
	const ptr = uintptr(0x9abc)
	if !d.Begin(ptr, "old", "u", "p", "peer", 4) {
		t.Fatal("old Begin rejected")
	}
	select {
	case <-oldStarted:
	case <-time.After(time.Second):
		t.Fatal("old request not started")
	}
	if !d.Begin(ptr, "new", "u", "p", "peer", 4) {
		t.Fatal("new Begin rejected")
	}
	d.mu.Lock()
	_, stalePending := d.pending["old"]
	_, currentPending := d.pending["new"]
	d.mu.Unlock()
	if stalePending || !currentPending {
		t.Fatalf("pending map not rebound: stale=%v current=%v", stalePending, currentPending)
	}
	if c := poll(t, d); c == nil || c.ID != "new" || !c.Allow {
		t.Fatalf("new completion missing: %+v", c)
	}
	close(release)
	waitForAuthWorkers(t, d, 0)
	if c := d.Poll(); c != nil {
		t.Fatalf("old request completed after pointer reuse: %+v", c)
	}
	if strings.Contains(d.Stats(), "pending=1") {
		t.Fatal("stale pending ID retained")
	}
}
