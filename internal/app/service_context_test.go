package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type contextualStartup struct {
	entered chan struct{}
	stopped bool
}

func (s *contextualStartup) Name() string { return "contextual" }
func (s *contextualStartup) Start() error { return errors.New("context path bypassed") }
func (s *contextualStartup) StartContext(ctx context.Context) error {
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}
func (s *contextualStartup) Stop() error { s.stopped = true; return nil }
func TestServiceManagerCancelsContextAwareStartup(t *testing.T) {
	m := NewServiceManager()
	s := &contextualStartup{entered: make(chan struct{})}
	m.RegisterService(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.StartAllContext(ctx) }()
	<-s.entered
	cancel()
	select {
	case e := <-done:
		if e == nil || !s.stopped {
			t.Fatalf("cancel cleanup e=%v stopped=%v", e, s.stopped)
		}
	case <-time.After(time.Second):
		t.Fatal("startup cancellation blocked")
	}
}
