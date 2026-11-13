package diagnostics

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

func TestLogicalGatewayBatchHasOneDeadlineAndBoundedCommitQueue(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			go func() { defer conn.Close(); <-stop }()
		}
	}()
	r := redis.NewClient(&redis.Options{Addr: listener.Addr().String(), ReadTimeout: 3 * time.Second})
	defer r.Close()
	m := NewMetrics(r)
	defer m.logicalClient.Close()
	log := logrus.New()
	log.SetOutput(io.Discard)
	c := &Collector{metrics: m, initialized: true, config: DefaultConfig(), logger: log}
	updates := make([]LogicalUpdate, 1000)
	for i := range updates {
		updates[i] = LogicalUpdate{Message: &LogicalMessage{TenantID: "tenant", DeviceID: fmt.Sprint(i), ID: "receipt", Family: "attribute"}, State: LogicalCommitted}
	}
	start := time.Now()
	c.RecordLogicalBatch(context.Background(), updates)
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("batch multiplied deadlines: %v", elapsed)
	}
	start = time.Now()
	for i := 0; i < 5000; i++ {
		c.enqueueLogical(updates[i%1000])
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("commits waited on Redis: %v", elapsed)
	}
	if len(c.logicalBuffer) != 2000 {
		t.Fatalf("unbounded queue: %d", len(c.logicalBuffer))
	}
	start = time.Now()
	c.flushLogical()
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("queue flush multiplied deadlines: %v", elapsed)
	}
	if len(c.logicalBuffer) != 0 {
		t.Fatal("failed observations retained without bound")
	}
}

func logicalFixture(t *testing.T) (*Metrics, string) {
	t.Helper()
	addr := os.Getenv("TEST_DIAGNOSTICS_REDIS_ADDR")
	if addr == "" {
		t.Skip("isolated Redis required")
	}
	r := redis.NewClient(&redis.Options{Addr: addr})
	if err := r.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	m := NewMetrics(r)
	device := "qa-q04-" + uuid.NewString()
	t.Cleanup(func() {
		m.logicalClient.Close()
		keys := append(m.logicalKeys(device), m.getStatsKey(device), m.getFailuresKey(device))
		cleanup := redis.NewClient(&redis.Options{Addr: addr})
		defer cleanup.Close()
		cleanup.Del(context.Background(), keys...)
		r.Close()
	})
	return m, device
}

func TestRedisLogicalOutcomesDoNotInferSuccessOrDoubleCount(t *testing.T) {
	m, device := logicalFixture(t)
	ctx := context.Background()
	if err := m.redisClient.HSet(ctx, m.getStatsKey(device), "uplink_total", 1000, "storage_failed", 0, "downlink_total", 2, "downlink_failed", 1).Err(); err != nil {
		t.Fatal(err)
	}
	if err := m.ObserveLogical(ctx, "tenant", device, "receipt-1", "event", LogicalPending, ""); err != nil {
		t.Fatal(err)
	}
	stats, err := m.GetStats(device)
	if err != nil || stats.UplinkTotal != 1 || stats.UplinkSuccess != 0 || stats.StorageTotal != 0 {
		t.Fatalf("pending/legacy fake success: %+v %v", stats, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := m.ObserveLogical(ctx, "tenant", device, "receipt-1", "event", LogicalCommitted, ""); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	for i := 0; i < 3; i++ {
		if err = m.ObserveLogical(ctx, "tenant", device, "receipt-2", "attribute", LogicalRejected, "上报类型不符合物模型"); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.ObserveLogical(ctx, "tenant", device, "receipt-3", "telemetry", LogicalPending, ""); err != nil {
		t.Fatal(err)
	}
	c := &Collector{metrics: m, initialized: true, config: DefaultConfig(), logger: logrus.New()}
	got, err := c.GetDiagnostics(device)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.Uplink.Total != 3 || got.Stats.Uplink.Success != 1 || got.Stats.Storage.Total != 1 || got.Stats.Storage.Success != 1 || got.Stats.Downlink.Total != 2 || got.Stats.Downlink.Success != 1 || len(got.RecentFailures) != 1 {
		t.Fatalf("wrong logical outcomes: %+v failures%d", got.Stats, len(got.RecentFailures))
	}
	if err = m.ObserveLogical(ctx, "tenant", device, "receipt-3", "telemetry", LogicalStorageFailed, "存储提交失败"); err != nil {
		t.Fatal(err)
	}
	stats, err = m.GetStats(device)
	if err != nil || stats.UplinkTotal != 3 || stats.UplinkFailed != 2 || stats.UplinkSuccess != 1 || stats.StorageTotal != 2 || stats.StorageSuccess != 1 {
		t.Fatalf("failed commit counted as success: %+v %v", stats, err)
	}
	// Failure to read Redis must propagate; a zero/default success is not data.
	m.redisClient.Close()
	if _, err = c.GetDiagnostics(device); err == nil {
		t.Fatal("Redis failure fabricated diagnostic response")
	}
}

func TestRedisLogicalObservationRetentionIsBounded(t *testing.T) {
	m, device := logicalFixture(t)
	ctx := context.Background()
	for i := 0; i < 4100; i++ {
		if err := m.ObserveLogical(ctx, "tenant", device, fmt.Sprint(i), "telemetry", LogicalCommitted, ""); err != nil {
			t.Fatal(err)
		}
	}
	keys := m.logicalKeys(device)
	if n := m.redisClient.HLen(ctx, keys[1]).Val(); n != 4096 {
		t.Fatalf("unbounded hash: %d", n)
	}
	if n := m.redisClient.ZCard(ctx, keys[2]).Val(); n != 4096 {
		t.Fatalf("unbounded order: %d", n)
	}
	if ttl := m.redisClient.TTL(ctx, keys[0]).Val(); ttl <= 0 {
		t.Fatal("unbounded retention time")
	}
	stats, err := m.GetStats(device)
	if err != nil || stats.UplinkTotal != 4100 || stats.UplinkSuccess != 4100 {
		t.Fatalf("retention changed totals: %+v %v", stats, err)
	}
}
