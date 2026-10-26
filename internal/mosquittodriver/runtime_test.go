package mosquittodriver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	bc "project/internal/brokercontract"
)

func runtimeConfig(url string) Config {
	return Config{OwnerURL: url, Token: strings.Repeat("r", 32), Cell: "community", Instance: "node-runtime", MaxPending: 4, Timeout: time.Second, RuntimeFencing: true}
}

func runtimeResponse(t *testing.T, w http.ResponseWriter, resp bc.RuntimeSyncResponse) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Errorf("encode runtime response: %v", err)
	}
}

func TestRuntimeFenceStagesOnWorkerAndInstallsOnTickBeforeAck(t *testing.T) {
	type request struct {
		BootID   string               `json:"boot_id"`
		Cell     string               `json:"broker_cell"`
		Instance string               `json:"broker_instance"`
		Applied  []bc.RuntimeFenceAck `json:"applied"`
	}
	var calls atomic.Int32
	var authEpoch atomic.Uint64
	authEpoch.Store(1)
	bootID := ""
	stageSeen := make(chan request, 1)
	ackSeen := make(chan request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/authenticate" {
			g := testGrant()
			g.RouteEpoch = authEpoch.Load()
			g.Policy.Subscribe = append(g.Policy.Subscribe, "devices/telemetry")
			_ = json.NewEncoder(w).Encode(g)
			return
		}
		if r.URL.Path != "/v1/runtime/sync" || r.Method != http.MethodPost {
			t.Errorf("unexpected runtime request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var got request
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode runtime request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if got.BootID == "" || got.Cell != "community" || got.Instance != "node-runtime" {
			t.Errorf("runtime registration missing scope/boot identity: %+v", got)
		}
		if bootID == "" {
			bootID = got.BootID
		} else if got.BootID != bootID {
			t.Errorf("broker boot identity changed during one process lifetime: %q != %q", got.BootID, bootID)
		}
		switch calls.Add(1) {
		case 1:
			runtimeResponse(t, w, bc.RuntimeSyncResponse{Fences: []bc.RuntimeFence{}})
		case 2:
			stageSeen <- got
			runtimeResponse(t, w, bc.RuntimeSyncResponse{Fences: []bc.RuntimeFence{{ChangeID: "change-1", TenantID: "tenant-a", DeviceID: "device-a", DeniedEpoch: 1, NeedsAck: true}}})
		default:
			ackSeen <- got
			runtimeResponse(t, w, bc.RuntimeSyncResponse{Fences: []bc.RuntimeFence{}})
		}
	}))
	defer srv.Close()
	d, err := New(runtimeConfig(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.Begin(1, "wire-old", "u", "p", "peer", 4) || !poll(t, d).Allow {
		t.Fatal("initial old-epoch authentication failed")
	}
	d.Connected(1)

	d.Tick()
	select {
	case req := <-stageSeen:
		if len(req.Applied) != 0 {
			t.Fatalf("worker sent an ACK before it installed this fence: %+v", req.Applied)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime worker did not request fence")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		staged := d.runtimePending != nil
		installed := len(d.runtimeFences) != 0
		d.mu.Unlock()
		if staged {
			if installed {
				t.Fatal("HTTP worker installed a fence off the broker main thread")
			}
			break
		}
		time.Sleep(time.Millisecond)
	}
	d.mu.Lock()
	if d.runtimePending == nil || len(d.runtimeFences) != 0 {
		d.mu.Unlock()
		t.Fatal("response was not staged separately from active runtime snapshot")
	}
	d.mu.Unlock()

	d.Tick() // broker-thread installation; a following HTTP poll can now ACK it.
	select {
	case req := <-ackSeen:
		if len(req.Applied) != 1 || req.Applied[0].ChangeID != "change-1" || len(req.Applied[0].EvidenceSHA256) != 64 {
			t.Fatalf("installed fence ACK missing/invalid: %+v", req.Applied)
		}
	case <-time.After(time.Second):
		t.Fatal("installed fence was not acknowledged by the next control request")
	}

	for name, check := range map[string]func() bool{
		"old epoch write":                     func() bool { return d.Authorize(1, 2, "devices/telemetry", 8, 1, false) },
		"old epoch subscribe":                 func() bool { return d.Authorize(1, 4, "devices/command/device-a/+", 0, 1, false) },
		"old epoch outgoing ordinary payload": func() bool { _, _, e := d.Outgoing(1, "devices/telemetry", []byte("{}"), 1, false); return e == nil },
	} {
		if check() {
			t.Errorf("%s remained authorized after fence install", name)
		}
	}

	// The owner gives the same logical device a higher current route epoch.
	// This verifies new generation admission without reviving the old session.
	authEpoch.Store(2)
	if !d.Begin(2, "wire-new", "u", "p", "peer", 4) || !poll(t, d).Allow {
		t.Fatal("same-scope higher epoch denied after fence")
	}
	d.Connected(2)
	if !d.Authorize(2, 2, "devices/telemetry", 1, 1, false) || !d.Authorize(2, 4, "devices/command/device-a/+", 0, 1, false) {
		t.Fatal("higher-epoch device could not publish/subscribe in scope")
	}
	if _, stripped, err := d.Outgoing(2, "devices/telemetry", []byte("plain"), 1, false); err != nil || stripped {
		t.Fatalf("higher-epoch ordinary delivery failed: stripped=%v err=%v", stripped, err)
	}
}

func TestRuntimeFenceLeaseAndNewEpochAdmission(t *testing.T) {
	var epoch atomic.Uint64
	epoch.Store(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runtime/sync":
			runtimeResponse(t, w, bc.RuntimeSyncResponse{Fences: []bc.RuntimeFence{{ChangeID: "fence", TenantID: "tenant-a", DeviceID: "device-a", DeniedEpoch: 1}}})
		case "/v1/authenticate":
			g := testGrant()
			g.RouteEpoch = epoch.Load()
			g.Policy.Subscribe = append(g.Policy.Subscribe, "devices/telemetry")
			_ = json.NewEncoder(w).Encode(g)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	d, err := New(runtimeConfig(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.Begin(11, "wire-old", "u", "p", "peer", 4) || poll(t, d).Allow {
		t.Fatal("old route epoch authenticated after runtime registration")
	}
	if d.Authorize(11, 2, "devices/telemetry", 1, 1, false) {
		t.Fatal("old epoch write allowed")
	}

	epoch.Store(2)
	if !d.Begin(12, "wire-new", "u", "p", "peer", 4) || !poll(t, d).Allow {
		t.Fatal("same-scope higher epoch denied")
	}
	d.Connected(12)
	if !d.Authorize(12, 2, "devices/telemetry", 1, 1, false) || !d.Authorize(12, 4, "devices/command/device-a/+", 0, 1, false) {
		t.Fatal("higher-epoch device could not use scoped publish/subscribe")
	}
	if _, stripped, err := d.Outgoing(12, "devices/telemetry", []byte("ok"), 1, false); err != nil || stripped {
		t.Fatalf("higher epoch ordinary delivery denied: stripped=%v err=%v", stripped, err)
	}

	d.mu.Lock()
	d.runtimeValidUntil = time.Now().Add(-time.Second)
	d.mu.Unlock()
	for _, access := range []int{2, 4, 1} {
		if d.Authorize(12, access, "devices/telemetry", 1, 1, false) {
			t.Errorf("access=%d survived expired control lease", access)
		}
	}
	if _, _, err := d.Outgoing(12, "devices/telemetry", []byte("ok"), 1, false); err == nil {
		t.Fatal("ordinary outgoing payload survived expired control lease")
	}
}

func TestRuntimePendingDeadlineDoesNotExtendOnStalledTick(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runtime/sync" {
			runtimeResponse(t, w, bc.RuntimeSyncResponse{Fences: []bc.RuntimeFence{}})
			return
		}
		g := testGrant()
		_ = json.NewEncoder(w).Encode(g)
	}))
	defer srv.Close()
	d, err := New(runtimeConfig(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.Begin(1, "wire", "u", "p", "peer", 4) || !poll(t, d).Allow {
		t.Fatal("auth")
	}
	d.Connected(1)
	d.mu.Lock()
	d.runtimePending = &bc.RuntimeSyncResponse{Fences: []bc.RuntimeFence{}}
	d.runtimePendingDeadline = time.Now().Add(-time.Second)
	d.mu.Unlock()
	d.Tick()
	d.mu.Lock()
	lease := d.runtimeValidUntil
	d.mu.Unlock()
	if time.Now().Before(lease) {
		t.Fatalf("stalled main-thread install refreshed expired snapshot lease: %s", lease)
	}
	if d.Authorize(1, 2, "devices/telemetry", 1, 1, false) {
		t.Fatal("expired staged control snapshot left write authorization active")
	}
}

func TestRuntimeControlRejectsOwnerFailuresAndBoundsAcks(t *testing.T) {
	goodFence := bc.RuntimeFence{ChangeID: "c", TenantID: "tenant-a", DeviceID: "device-a", DeniedEpoch: 1, NeedsAck: true}
	cases := []struct {
		name       string
		httpStatus int
		body       func() []byte
		redirect   bool
	}{
		{name: "owner failure", httpStatus: http.StatusServiceUnavailable},
		{name: "redirect", httpStatus: http.StatusFound, redirect: true},
		{name: "malformed", body: func() []byte { return []byte(`{"fences":[`) }},
		{name: "duplicate json", body: func() []byte { return []byte(`{"fences":[],"fences":[]}`) }},
		{name: "unknown field", body: func() []byte { return []byte(`{"fences":[],"extra":true}`) }},
		{name: "overflow", body: func() []byte { return []byte(`{"overflow":true,"fences":[]}`) }},
		{name: "malformed fence", body: func() []byte {
			return []byte(`{"fences":[{"change_id":"c","tenant_id":"","device_id":"d","denied_epoch":1}]}`)
		}},
		{name: "too many fences", body: func() []byte {
			fences := make([]bc.RuntimeFence, bc.MaxRuntimeFences+1)
			for i := range fences {
				fences[i] = goodFence
				fences[i].ChangeID = fmt.Sprintf("c-%03d", i)
			}
			b, _ := json.Marshal(bc.RuntimeSyncResponse{Fences: fences})
			return b
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.redirect {
					http.Redirect(w, r, "/elsewhere", tc.httpStatus)
					return
				}
				if tc.httpStatus != 0 {
					w.WriteHeader(tc.httpStatus)
					return
				}
				_, _ = w.Write(tc.body())
			}))
			defer srv.Close()
			if d, err := New(runtimeConfig(srv.URL)); err == nil {
				d.Close()
				t.Fatal("invalid runtime control response allowed driver startup")
			}
		})
	}

	// The full accepted fence limit remains bounded even when every item needs
	// an acknowledgement and is installed in one broker-thread Tick.
	fences := make([]bc.RuntimeFence, bc.MaxRuntimeFences)
	for i := range fences {
		fences[i] = bc.RuntimeFence{ChangeID: fmt.Sprintf("f-%03d", i), TenantID: "tenant-a", DeviceID: fmt.Sprintf("d-%03d", i), DeniedEpoch: 1, NeedsAck: true}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeResponse(t, w, bc.RuntimeSyncResponse{Fences: fences})
	}))
	defer srv.Close()
	d, err := New(runtimeConfig(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.runtimeAcks) != bc.MaxRuntimeFences || len(d.runtimeFences) != bc.MaxRuntimeFences {
		t.Fatalf("runtime state exceeded/underfilled cap: acks=%d fences=%d", len(d.runtimeAcks), len(d.runtimeFences))
	}
	for _, ack := range d.runtimeAcks {
		if len(ack.EvidenceSHA256) != 64 {
			t.Fatal("runtime ACK evidence not fixed-size SHA256")
		}
	}
}
