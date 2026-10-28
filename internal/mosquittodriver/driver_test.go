package mosquittodriver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	bc "project/internal/brokercontract"
	"strings"
	"testing"
	"time"
)

func testGrant() bc.Grant {
	return bc.Grant{Allow: true, Principal: bc.Principal{TenantID: "tenant-a", DeviceID: "device-a", Kind: bc.PrincipalDevice}, Policy: bc.Policy{Publish: []string{"devices/telemetry", "devices/event/+"}, Subscribe: []string{"devices/command/device-a/+"}, MaxPayload: 1024, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}, ConnectionID: "connection-a", RouteEpoch: 1, PolicyVersion: "policy-1", MappingVersion: "standard-1", ExpiresAt: time.Now().Add(60 * time.Second)}
}
func newTestDriver(t *testing.T, fn http.HandlerFunc, limit int) *Driver {
	t.Helper()
	srv := httptest.NewServer(fn)
	t.Cleanup(srv.Close)
	d, e := New(Config{OwnerURL: srv.URL, Token: strings.Repeat("x", 32), Cell: "community", Instance: "node-a", MaxPending: limit, Timeout: 80 * time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(d.Close)
	return d
}
func poll(t *testing.T, d *Driver) *Completion {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c := d.Poll(); c != nil {
			return c
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("authentication never completed")
	return nil
}
func TestRawBytesAndAuthenticatedPrincipal(t *testing.T) {
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
			t.Error("missing service authentication")
		}
		json.NewEncoder(w).Encode(testGrant())
	}, 2)
	if !d.Begin(1, "client-a", "token", "", "127.0.0.1", 4) || !poll(t, d).Allow {
		t.Fatal("auth failed")
	}
	d.Connected(1)
	raw := []byte{0, 1, 2, 255}
	topic, body, retain, err := d.Rewrite(1, "devices/telemetry", raw, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	env, err := bc.ParseIngress(topic, body, bc.TrustedMetadata{BrokerCell: "community", BrokerInstance: "node-a"})
	if err != nil {
		t.Fatal(err)
	}
	if retain || !bytes.Equal(env.Values, raw) || env.DeviceID != "device-a" {
		t.Fatalf("altered transport: %+v", env)
	}
	if d.Authorize(1, 2, "$tp/ingress/v1/forged", 1, 1, false) || d.Authorize(1, 4, "devices/command/device-b/+", 0, 1, false) || d.Authorize(1, 1, "devices/command/device-b/id", 1, 1, false) {
		t.Fatal("cross scope allowed")
	}
	if !d.Authorize(1, 1, "devices/command/device-a/id", 1, 1, false) {
		t.Fatal("own delivery denied")
	}
	if _, _, _, err = d.Rewrite(1, "devices/telemetry", raw, 1, true); err == nil {
		t.Fatal("unapproved retain accepted")
	}
	d.Closed(1)
	if d.Authorize(1, 2, "devices/telemetry", 1, 1, false) {
		t.Fatal("closed connection grant survived")
	}
}
func TestDelayedDisconnectAndBoundedAuthentication(t *testing.T) {
	gate := make(chan struct{})
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		json.NewEncoder(w).Encode(testGrant())
	}, 1)
	if !d.Begin(1, "a", "u", "p", "peer", 4) {
		t.Fatal("first denied")
	}
	if d.Begin(2, "b", "u", "p", "peer", 4) || d.Begin(3, "a", "u", "p", "peer", 4) {
		t.Fatal("pending limit bypass")
	}
	d.Closed(1)
	close(gate)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		n := d.activeAuth
		d.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if c := d.Poll(); c != nil {
		t.Fatalf("late result revived socket: %+v", c)
	}
	if !d.Begin(1, "a", "u", "p", "peer", 4) || !poll(t, d).Allow {
		t.Fatal("pointer reuse did not bind fresh request")
	}
}
func TestOwnerFaultsDeny(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"500", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }},
		{"invalid", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) }},
		{"trailing", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(testGrant())
			w.Write([]byte("{}"))
		}},
		{"timeout", func(w http.ResponseWriter, r *http.Request) { time.Sleep(200 * time.Millisecond) }},
		{"bad_policy", func(w http.ResponseWriter, r *http.Request) {
			g := testGrant()
			g.Policy.MaxQoS = 3
			json.NewEncoder(w).Encode(g)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDriver(t, tc.handler, 2)
			if !d.Begin(1, "a", "u", "p", "peer", 4) {
				t.Fatal("begin")
			}
			if poll(t, d).Allow {
				t.Fatal("fault allowed")
			}
			if d.Authorize(1, 2, "devices/telemetry", 1, 1, false) {
				t.Fatal("fault retained grant")
			}
		})
	}
}
func TestInsecureRemoteOwnerRejected(t *testing.T) {
	for _, u := range []string{"http://example.com", "http://u:p@127.0.0.1", "file:///tmp/a"} {
		if _, e := New(Config{OwnerURL: u, Token: strings.Repeat("x", 32), Cell: "c", Instance: "i"}); e == nil {
			t.Fatalf("accepted %s", u)
		}
	}
}

func TestImmediateWillRetainsClosingIdentityWithoutReopeningAuthorization(t *testing.T) {
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(testGrant()) }, 2)
	if !d.Begin(1, "a", "u", "p", "peer", 4) || !poll(t, d).Allow {
		t.Fatal("auth")
	}
	d.Connected(1)
	_, online, e := d.Observation(1, true)
	if e != nil {
		t.Fatal(e)
	}
	var s bc.StateObservation
	if e = json.Unmarshal(online, &s); e != nil || s.EventSeq != 1 {
		t.Fatal("bad online observation")
	}
	d.Closed(1)
	if !d.IsClosed(1) || d.Authorize(1, 2, "devices/telemetry", 1, 1, false) {
		t.Fatal("ordinary access after close")
	}
	topic, body, _, e := d.RewriteWill(1, "devices/event/will", []byte(`{"method":"offline"}`), 1, false)
	if e != nil {
		t.Fatal(e)
	}
	env, e := bc.ParseIngress(topic, body, bc.TrustedMetadata{BrokerCell: "community", BrokerInstance: "node-a"})
	if e != nil || env.Metadata.ConnectionID != "connection-a" || env.DeviceID != "device-a" {
		t.Fatalf("wrong will principal %+v %v", env, e)
	}
	if _, _, _, e = d.RewriteWill(2, "devices/event/will", []byte("{}"), 1, false); e == nil {
		t.Fatal("unbound restored will allowed")
	}
}
