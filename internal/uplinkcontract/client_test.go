package uplinkcontract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"project/internal/brokersemantic"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "fixture-service-token-at-least-32-characters"
const testModel = `{"name":"fixture","fields":[{"kind":"TELEMETRY","identifier":"temperature","nameI18n":{"default":"T"},"access":{"read":true},"valueType":{"kind":"NUMBER"}}]}`

func answer(w http.ResponseWriter, pins Pins) {
	h := sha256.Sum256([]byte(testModel))
	_ = json.NewEncoder(w).Encode(map[string]any{"schema_version": 1, "tenant_id": pins.TenantID, "model_id": pins.ModelID, "snapshot_id": pins.SnapshotID, "revision": pins.Revision, "contract_sha256": hex.EncodeToString(h[:]), "model": json.RawMessage(testModel)})
}
func fixtureClient(t *testing.T, handle http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handle)
	t.Cleanup(s.Close)
	c, err := NewClient(Config{s.URL, testToken, "deployment"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}
func requestPins(t *testing.T, r *http.Request) Pins {
	t.Helper()
	if r.Method != "POST" || r.URL.Path != "/internal/thingmodel/uplink-contract" || r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("X-Automation-Deployment") != "deployment" {
		t.Error("untrusted request identity")
	}
	var p Pins
	if json.NewDecoder(r.Body).Decode(&p) != nil {
		t.Error("bad request")
	}
	p.TenantID = r.Header.Get("X-Automation-Tenant")
	return p
}
func TestUplinkContractColdSingleflightAndWarmZeroHTTP(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		p := requestPins(t, r)
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		answer(w, p)
	})
	pins := Pins{"tenant", "model", "snapshot", 1}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := c.Contract(context.Background(), pins)
			if e != nil || v.Validate("telemetry", []byte(`{"temperature":0}`)) != nil {
				t.Errorf("contract err %v", e)
			}
		}()
	}
	<-entered
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("cold HTTP calls=%d", calls.Load())
	}
	for i := 0; i < 1000; i++ {
		v, e := c.Contract(context.Background(), pins)
		if e != nil || v.Validate("telemetry", []byte(`{"temperature":12.5}`)) != nil {
			t.Fatal("warm validation failed")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("warm path made HTTP request")
	}
}
func TestUplinkContractRejectsWrongPinsAndDoesNotCacheFailure(t *testing.T) {
	var calls atomic.Int32
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		p := requestPins(t, r)
		calls.Add(1)
		p.TenantID = "foreign"
		answer(w, p)
	})
	for i := 0; i < 2; i++ {
		if _, e := c.Contract(context.Background(), Pins{"tenant", "model", "snapshot", 1}); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("failed owner response cached")
	}
}
func TestUplinkContractCachePinsAndBoundedLRU(t *testing.T) {
	var calls atomic.Int32
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); answer(w, requestPins(t, r)) })
	c.cache = newContractCache(c.load, 2, 64<<10, 4)
	a := Pins{"a", "m", "s", 1}
	b := Pins{"b", "m", "s", 1}
	v2 := Pins{"a", "m", "s2", 2}
	for _, p := range []Pins{a, b, a, v2, b} {
		if _, e := c.Contract(context.Background(), p); e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("cache keys/eviction calls=%d", calls.Load())
	}
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()
	if len(c.cache.entries) > 2 || c.cache.bytes > c.cache.maxBytes {
		t.Fatal("unbounded cache")
	}
}
func TestUplinkContractCancelledWaiterDoesNotPoisonOtherWaiters(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		p := requestPins(t, r)
		close(entered)
		<-release
		answer(w, p)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	pins := Pins{"a", "m", "s", 1}
	go func() { _, e := c.Contract(ctx, pins); done <- e }()
	<-entered
	cancel()
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	close(release)
	if _, e := c.Contract(context.Background(), pins); e != nil {
		t.Fatal(e)
	}
}
func TestUplinkContractColdLoadBoundAndNoExternalLocks(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var active, peak atomic.Int32
	c := newContractCache(func(ctx context.Context, p Pins) (*brokersemantic.UplinkContract, error) {
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		defer active.Add(-1)
		entered <- struct{}{}
		select {
		case <-release:
			return brokersemantic.CompileUplinkContract(json.RawMessage(testModel))
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, 2, 64<<10, 2)
	var wg sync.WaitGroup
	for _, tenant := range []string{"a", "b"} {
		wg.Add(1)
		go func(v string) { defer wg.Done(); _, _ = c.get(context.Background(), Pins{v, "m", "s", 1}) }(tenant)
	}
	<-entered
	<-entered
	started := time.Now()
	if _, e := c.get(context.Background(), Pins{"c", "m", "s", 1}); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("cold load saturation queued instead of backpressure")
	}
	// The slow loader does not hold the cache mutex; SQL locking is separately
	// tested at the later prepare/commit integration boundary.
	c.mu.Lock()
	c.mu.Unlock()
	close(release)
	wg.Wait()
	if peak.Load() != 2 {
		t.Fatal("unexpected cold concurrency")
	}
}
