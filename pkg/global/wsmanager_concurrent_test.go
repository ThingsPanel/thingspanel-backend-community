package global

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
)

type wsManagerFakeRedis struct{ decrCalls atomic.Int64 }

func (*wsManagerFakeRedis) DialHook(redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("fixture has no network")
	}
}
func (h *wsManagerFakeRedis) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		if cmd.Name() != "decr" {
			return errors.New("unexpected fixture Redis command")
		}
		h.decrCalls.Add(1)
		counter, ok := cmd.(*redis.IntCmd)
		if !ok {
			return errors.New("unexpected Redis result type")
		}
		counter.SetVal(1)
		return nil
	}
}
func (*wsManagerFakeRedis) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(context.Context, []redis.Cmder) error { return errors.New("unexpected fixture Redis pipeline") }
}

func TestWSManagerPushUnsubscribeConcurrent(t *testing.T) {
	hook := &wsManagerFakeRedis{}
	client := redis.NewClient(&redis.Options{Addr: "fixture.invalid:1", MaxRetries: -1})
	client.AddHook(hook)
	defer client.Close()
	for i := 0; i < 100; i++ {
		ws := &WSClient{Send: make(chan []byte, 1)}
		manager := &WSManager{redisClient: client, deviceClients: map[string]map[string]*WSClient{"device": {"conn": ws}}}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for j := 0; j < 8; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for k := 0; k < 100; k++ {
					manager.PushToDevice("device", map[string]interface{}{"value": 1})
				}
			}()
		}
		var firstErr, secondErr error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; firstErr = manager.UnsubscribeDevice("device", "conn") }()
		go func() { defer wg.Done(); <-start; secondErr = manager.UnsubscribeDevice("device", "conn") }()
		close(start)
		wg.Wait()
		if firstErr != nil || secondErr != nil {
			t.Fatalf("unsubscribe errors %v %v", firstErr, secondErr)
		}
		if got := ws.TrySend([]byte("after")); got != WSSendClosed {
			t.Fatalf("send after unsubscribe=%v", got)
		}
		if got := manager.GetStats()["total_clients"]; got != 0 {
			t.Fatalf("remaining clients=%v", got)
		}
	}
	if got := hook.decrCalls.Load(); got != 100 {
		t.Fatalf("DECR calls=%d, want one per removed client", got)
	}
}
