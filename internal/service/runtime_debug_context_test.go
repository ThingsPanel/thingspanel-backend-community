package service

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"project/pkg/global"
)

func TestRuntimeDebugRedisViewIsBoundedAndDoesNotMutatePublicClient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	release := make(chan struct{})
	defer close(release)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		<-release
	}()
	source := redis.NewClient(&redis.Options{Addr: listener.Addr().String(), DialTimeout: 100 * time.Millisecond, PoolTimeout: 100 * time.Millisecond, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, MaxRetries: -1})
	defer source.Close()
	old := global.REDIS
	global.REDIS = source
	t.Cleanup(func() { global.REDIS = old })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	scoped, err := RuntimeDebugContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client := deviceDebugRedis(scoped)
	if client == source || client.Options().ReadTimeout != time.Second || client.Options().WriteTimeout != time.Second {
		t.Fatal("missing capability-specific timeout view")
	}
	if source.Options().ReadTimeout != 30*time.Second || source.Options().ContextTimeoutEnabled {
		t.Fatal("changed public Redis client options")
	}
	done := make(chan error, 1)
	go func() { done <- client.Ping(scoped).Err() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("unresponsive Redis unexpectedly succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Redis read retained the runtime fence indefinitely")
	}
}

func TestRuntimeDebugRedisRejectsUnboundedPoolPolicy(t *testing.T) {
	old := global.REDIS
	t.Cleanup(func() { global.REDIS = old })
	for _, opts := range []*redis.Options{
		{Addr: "127.0.0.1:1", PoolTimeout: -1},
		{Addr: "127.0.0.1:1", DialTimeout: -1},
		{Addr: "127.0.0.1:1", MaxRetries: 3},
	} {
		client := redis.NewClient(opts)
		global.REDIS = client
		if _, err := RuntimeDebugContext(context.Background()); err == nil {
			client.Close()
			t.Fatal("unbounded Redis client admitted into runtime fence")
		}
		client.Close()
	}
}
