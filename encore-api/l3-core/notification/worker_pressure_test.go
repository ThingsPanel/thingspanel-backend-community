package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

// TestWorkerPressureBoundsCallsAndServesNewTenantsBehindBacklog exercises an
// isolated PostgreSQL queue under a multi-tenant backlog. It measures the
// worker's configured send bound and a test-owned database/sql pool cap; it
// does not change production limits or contact a real provider.
func TestWorkerPressureBoundsCallsAndServesNewTenantsBehindBacklog(t *testing.T) {
	store, db := newMigratedTestStore(t)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	var phase atomic.Int32
	var active, maxActive atomic.Int32
	var calls atomic.Int32
	firstRelease := make(chan struct{})
	burstRelease := make(chan struct{})
	firstEntered := make(chan string, 1)
	burstEntered := make(chan string, 8)
	var tenantBQueuedAt atomic.Int64
	var tenantBStartedAfterQueue atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request contract.PluginSendRequestV1
		if r.Method != http.MethodPost || r.URL.Path != "/v1/send" || r.Header.Get("Authorization") != "Bearer fixture-worker-auth" || json.NewDecoder(r.Body).Decode(&request) != nil || request.TenantID == "" || request.DeliveryID == "" {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		calls.Add(1)
		current := active.Add(1)
		for previous := maxActive.Load(); current > previous && !maxActive.CompareAndSwap(previous, current); previous = maxActive.Load() {
		}
		defer active.Add(-1)
		switch phase.Load() {
		case 0:
			select {
			case firstEntered <- request.TenantID:
			default:
			}
			<-firstRelease
		case 1:
			if request.TenantID == "pressure-tenant-b" {
				if queuedAt := tenantBQueuedAt.Load(); queuedAt > 0 {
					tenantBStartedAfterQueue.Store(time.Since(time.Unix(0, queuedAt)).Nanoseconds())
				}
			}
			burstEntered <- request.TenantID
			<-burstRelease
		default:
			// A small deterministic response delay keeps the rest of the run under
			// sustained but bounded pressure without slowing the test materially.
			time.Sleep(2 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contract.PluginSendResultV1{DeliveryID: request.DeliveryID, Outcome: "accepted", ReceiptExpected: false})
	}))
	defer server.Close()

	tenants := []string{"pressure-tenant-a", "pressure-tenant-b", "pressure-tenant-c", "pressure-tenant-d"}
	instances := make(map[string]string, len(tenants))
	for _, tenant := range tenants {
		instances[tenant] = createWorkerFixtureInstance(t, store, tenant, server.URL)
	}
	backlog := submitWorkerFixtures(t, store, tenants[0], instances[tenants[0]], 60)

	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal("construct fixture-only plugin client")
	}
	sender := &InstancePluginSender{Client: client, Runtimes: store}
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: sender, Concurrency: 4, BatchSize: 4, PluginTimeout: 5 * time.Second, FinishTimeout: time.Second, LeaseFor: 7 * time.Second})
	if err != nil {
		t.Fatal("construct bounded pressure worker")
	}
	if _, err := NewWorker(WorkerOptions{Store: store, Sender: sender, Concurrency: 33, BatchSize: 1, PluginTimeout: time.Second, FinishTimeout: time.Second, LeaseFor: 3 * time.Second}); !errors.Is(err, ErrWorkerInvalidConfig) {
		t.Fatalf("worker over its documented 32-send limit should fail configuration explicitly: err=%v", err)
	}

	started := time.Now()
	firstCycle := make(chan error, 1)
	go func() {
		cycle, err := worker.RunOnce(context.Background())
		if err == nil && (cycle.Claimed != 1 || cycle.Finished != 1) {
			err = fmt.Errorf("initial backlog cycle claimed=%d finished=%d", cycle.Claimed, cycle.Finished)
		}
		firstCycle <- err
	}()
	select {
	case tenant := <-firstEntered:
		if tenant != tenants[0] {
			close(firstRelease)
			t.Fatalf("initial request did not come from the backlogged tenant: %s", tenant)
		}
	case <-time.After(5 * time.Second):
		close(firstRelease)
		t.Fatal("worker did not reach the mock HTTP endpoint for initial backlog")
	}

	// New tenant queues arrive while the first old-tenant send is in flight.
	// The source backlog is intentionally large; each new tenant gets its own
	// enabled instance, matching the production per-instance serialization rule.
	newQueues := make(map[string]contract.AcceptedData, 3)
	for _, tenant := range tenants[1:] {
		newQueues[tenant] = submitWorkerFixtures(t, store, tenant, instances[tenant], 4)
	}
	tenantBQueuedAt.Store(time.Now().UnixNano())
	close(firstRelease)
	if err := <-firstCycle; err != nil {
		t.Fatal(err)
	}

	phase.Store(1)
	burstCycle := make(chan error, 1)
	go func() {
		cycle, err := worker.RunOnce(context.Background())
		if err == nil && (cycle.Claimed != 4 || cycle.Finished != 4) {
			err = fmt.Errorf("fair pressure cycle claimed=%d finished=%d", cycle.Claimed, cycle.Finished)
		}
		burstCycle <- err
	}()
	servedInBurst := make(map[string]bool, 4)
	for i := 0; i < 4; i++ {
		select {
		case tenant := <-burstEntered:
			servedInBurst[tenant] = true
		case <-time.After(5 * time.Second):
			close(burstRelease)
			t.Fatalf("pressure batch did not dispatch four tenant claims: seen=%v", servedInBurst)
		}
	}
	if len(servedInBurst) != 4 {
		close(burstRelease)
		t.Fatalf("fair batch did not include backlogged and newly queued tenants: %v", servedInBurst)
	}
	for _, tenant := range tenants {
		if !servedInBurst[tenant] {
			close(burstRelease)
			t.Fatalf("tenant %s was skipped in the first post-arrival batch: %v", tenant, servedInBurst)
		}
	}
	if maxActive.Load() != 4 {
		close(burstRelease)
		t.Fatalf("worker concurrency did not remain at the configured four-call bound: max=%d", maxActive.Load())
	}
	close(burstRelease)
	if err := <-burstCycle; err != nil {
		t.Fatal(err)
	}
	phase.Store(2)
	newTenantLatency := time.Duration(tenantBStartedAfterQueue.Load())
	if newTenantLatency <= 0 || newTenantLatency > 5*time.Second {
		t.Fatalf("new tenant B was not served promptly after enqueue: latency=%s", newTenantLatency)
	}

	total := len(backlog.DeliveryIDs)
	for _, accepted := range newQueues {
		total += len(accepted.DeliveryIDs)
	}
	for cycles := 0; worker.Stats().Finished < uint64(total); cycles++ {
		if cycles >= total+2 {
			t.Fatalf("worker stopped draining within bounded cycles: stats=%+v total=%d", worker.Stats(), total)
		}
		if _, err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("drain pressure queues: %v", err)
		}
	}
	if calls.Load() != int32(total) {
		t.Fatalf("provider mock call count does not match durable deliveries: calls=%d total=%d", calls.Load(), total)
	}
	for tenant, accepted := range map[string]contract.AcceptedData{
		tenants[0]: backlog,
		tenants[1]: newQueues[tenants[1]],
		tenants[2]: newQueues[tenants[2]],
		tenants[3]: newQueues[tenants[3]],
	} {
		for _, deliveryID := range accepted.DeliveryIDs {
			var status string
			if err := db.QueryRow(`SELECT dispatch_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, deliveryID).Scan(&status); err != nil || status != "accepted" {
				t.Fatalf("pressure run lost or stranded a delivery: tenant=%s status=%q err=%v", tenant, status, err)
			}
		}
	}
	poolStats := db.Stats()
	if poolStats.MaxOpenConnections != 4 || poolStats.OpenConnections > 4 || poolStats.InUse != 0 {
		t.Fatalf("test-owned SQL pool exceeded its explicit cap or did not quiesce: max=%d open=%d in_use=%d", poolStats.MaxOpenConnections, poolStats.OpenConnections, poolStats.InUse)
	}
	duration := time.Since(started)
	throughput := float64(total) / duration.Seconds()
	t.Logf("worker pressure baseline: deliveries=%d elapsed=%s throughput=%.1f/s new_tenant_b_first_http=%s max_provider_calls=%d sql_pool_max=%d sql_pool_observed_max_open=%d sql_wait_count=%d", total, duration.Round(time.Millisecond), throughput, newTenantLatency.Round(time.Millisecond), maxActive.Load(), poolStats.MaxOpenConnections, poolStats.OpenConnections, poolStats.WaitCount)
}
