package diagnostics

import (
	"context"
	"net"
	"reflect"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

type captureDiagnosticsPipeline struct {
	batches [][]redis.Cmder
}

func (h *captureDiagnosticsPipeline) DialHook(next redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		panic("recording diagnostics must not open a network connection during this test")
	}
}

func (h *captureDiagnosticsPipeline) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return next
}

func (h *captureDiagnosticsPipeline) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(_ context.Context, cmds []redis.Cmder) error {
		h.batches = append(h.batches, cmds)
		return nil
	}
}

func TestCumulativeStatisticsCoalesceBeforeRedisFlush(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	hook := &captureDiagnosticsPipeline{}
	client.AddHook(hook)
	c := &Collector{metrics: NewMetrics(client), config: DefaultConfig(), initialized: true, logger: logrus.New(), counts: make(map[string]Stats)}

	c.RecordUplinkPoints("a", 3)
	c.RecordUplinkPoints("a", 4)
	c.RecordUplinkPoints("b", 2)
	c.RecordDownlinkPublish("a", nil)
	c.RecordDownlinkPublish("a", nil)
	if len(hook.batches) != 0 {
		t.Fatal("recording performed Redis I/O before flushing")
	}
	c.flush()
	c.flush()
	if len(hook.batches) != 1 {
		t.Fatalf("expected one pipeline, got %d", len(hook.batches))
	}
	actual := make(map[string]int64)
	for _, cmd := range hook.batches[0] {
		args := cmd.Args()
		if len(args) != 4 || args[0] != "hincrby" {
			t.Fatalf("unexpected counter command: %v", args)
		}
		key := args[1].(string) + "/" + args[2].(string)
		if _, ok := actual[key]; ok {
			t.Fatalf("counter was not coalesced: %v", args)
		}
		actual[key] = args[3].(int64)
	}
	want := map[string]int64{
		"device:a:diagnostics:stats/uplink_total":   7,
		"device:b:diagnostics:stats/uplink_total":   2,
		"device:a:diagnostics:stats/downlink_total": 2,
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("got %v, want %v", actual, want)
	}
}
