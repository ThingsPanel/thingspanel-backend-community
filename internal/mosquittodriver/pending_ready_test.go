package mosquittodriver

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestReadyButUncompletedAuthStillConsumesPendingCapacity(t *testing.T) {
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(testGrant()) }, 1)
	if !d.Begin(1, "ready-a", "u", "p", "peer", 4) {
		t.Fatal("first auth rejected")
	}
	deadline := time.Now().Add(time.Second)
	for {
		d.mu.Lock()
		ready := d.sessions[1] != nil && d.sessions[1].ready
		active := d.activeAuth
		d.mu.Unlock()
		if ready && active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owner did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if d.Begin(2, "ready-b", "u", "p", "peer", 4) {
		t.Fatal("uncompleted callback bypassed pending bound")
	}
	if c := poll(t, d); !c.Allow || c.ID != "ready-a" {
		t.Fatal("wrong completion")
	}
	if !d.Begin(2, "ready-b", "u", "p", "peer", 4) {
		t.Fatal("completion did not release pending capacity")
	}
	if !poll(t, d).Allow {
		t.Fatal("second auth denied after capacity release")
	}
}
