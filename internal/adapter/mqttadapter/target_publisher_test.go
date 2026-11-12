package mqttadapter

import (
	"context"
	"errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"sync"
	"testing"
	"time"
)

type targetFakeClient struct {
	mqtt.Client
	disconnected bool
	publishes    int
}

func TestTargetPublisherConcurrentSendsSerializeAndHonorCancellation(t *testing.T) {
	client := &targetFakeClient{}
	p := &targetPublisherPool{ctx: context.Background(), config: MQTTConfig{ClientID: "platform"}, entries: map[string]*targetPublisherEntry{}, limit: 1, now: time.Now}
	dials := 0
	p.connect = func(context.Context, MQTTConfig) (mqtt.Client, error) { dials++; return client, nil }
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- p.publish(context.Background(), "a", "topic", nil) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if dials != 1 || client.publishes != 12 {
		t.Fatal("concurrent sends were lost or duplicated")
	}
	if _, err := p.acquire(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.publish(ctx, "a", "topic", nil); err == nil {
		t.Fatal("busy wait ignored cancellation")
	}
	if client.publishes != 12 {
		t.Fatal("canceled waiter sent a message")
	}
	p.close()
}

func (c *targetFakeClient) IsConnectionOpen() bool { return !c.disconnected }
func (c *targetFakeClient) Disconnect(uint)        { c.disconnected = true }
func (c *targetFakeClient) Publish(string, byte, bool, interface{}) mqtt.Token {
	c.publishes++
	return completedToken{}
}

func TestTargetPublisherPoolBoundsReuseExpiryAndShutdown(t *testing.T) {
	now := time.Now()
	connections := []*targetFakeClient{}
	p := &targetPublisherPool{ctx: context.Background(), config: MQTTConfig{ClientID: "platform"}, entries: map[string]*targetPublisherEntry{}, limit: 2, now: func() time.Time { return now }}
	p.connect = func(_ context.Context, c MQTTConfig) (mqtt.Client, error) {
		if c.ClientID == "platform" {
			t.Fatal("target scope missing")
		}
		f := &targetFakeClient{}
		connections = append(connections, f)
		return f, nil
	}
	if e := p.publish(context.Background(), "a", "topic", []byte("x")); e != nil {
		t.Fatal(e)
	}
	if e := p.publish(context.Background(), "a", "topic", []byte("x")); e != nil {
		t.Fatal(e)
	}
	if len(connections) != 1 || connections[0].publishes != 2 {
		t.Fatal("connection not reused")
	}
	now = now.Add(targetPublisherLifetime)
	if e := p.publish(context.Background(), "a", "topic", []byte("x")); e != nil {
		t.Fatal(e)
	}
	if len(connections) != 2 || !connections[0].disconnected {
		t.Fatal("expired authorization reused")
	}
	a, e := p.acquire(context.Background(), "a")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.acquire(context.Background(), "a"); e == nil {
		t.Fatal("busy target shared")
	}
	_, e = p.acquire(context.Background(), "b")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.acquire(context.Background(), "c"); e == nil || len(connections) != 3 {
		t.Fatal("capacity not enforced")
	}
	a.busy = false
	if _, e = p.acquire(context.Background(), "c"); e != nil {
		t.Fatal(e)
	}
	if !a.client.(*targetFakeClient).disconnected || len(p.entries) != 2 {
		t.Fatal("idle eviction failed")
	}
	for _, entry := range p.entries {
		entry.busy = false
	}
	now = now.Add(targetPublisherIdle)
	p.reap()
	if len(p.entries) != 0 {
		t.Fatal("idle entries leaked")
	}
	p.close()
	if _, e = p.acquire(context.Background(), "d"); e == nil {
		t.Fatal("closed pool reopened")
	}
}

func TestTargetPublisherPoolFailsClosedOnDialAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	p := &targetPublisherPool{ctx: ctx, config: MQTTConfig{ClientID: "platform"}, entries: map[string]*targetPublisherEntry{}, limit: 1, now: time.Now}
	p.connect = func(context.Context, MQTTConfig) (mqtt.Client, error) {
		calls++
		return nil, errors.New("owner unavailable")
	}
	if e := p.publish(context.Background(), "a", "topic", nil); e == nil || len(p.entries) != 0 {
		t.Fatal("failed dial cached")
	}
	cancel()
	if e := p.publish(context.Background(), "a", "topic", nil); e == nil || calls != 1 {
		t.Fatal("shutdown dialed")
	}
}
