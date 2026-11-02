package global

import (
	"sync"
	"testing"
)

func TestWSClientTrySendQueueState(t *testing.T) {
	c := &WSClient{Send: make(chan []byte, 1)}
	if got := c.TrySend([]byte("first")); got != WSSendQueued {
		t.Fatalf("first send=%v", got)
	}
	if got := c.TrySend([]byte("second")); got != WSSendFull {
		t.Fatalf("full send=%v", got)
	}
	if got := string(<-c.Send); got != "first" {
		t.Fatalf("queued data=%q", got)
	}
	c.closeSend()
	c.closeSend()
	if got := c.TrySend([]byte("third")); got != WSSendClosed {
		t.Fatalf("closed send=%v", got)
	}
	if _, ok := <-c.Send; ok {
		t.Fatal("closed queue remained open")
	}
}

func TestWSClientConcurrentSendAndClose(t *testing.T) {
	for attempt := 0; attempt < 100; attempt++ {
		c := &WSClient{Send: make(chan []byte, 1)}
		start := make(chan struct{})
		var workers sync.WaitGroup
		for i := 0; i < 8; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				for j := 0; j < 100; j++ {
					_ = c.TrySend([]byte("x"))
				}
			}()
		}
		workers.Add(1)
		go func() { defer workers.Done(); <-start; c.closeSend(); c.closeSend() }()
		close(start)
		workers.Wait()
		if got := c.TrySend([]byte("after")); got != WSSendClosed {
			t.Fatalf("attempt=%d send after close=%v", attempt, got)
		}
	}
}
