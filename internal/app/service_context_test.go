package app

import (
	"context"
	"errors"
	"reflect"
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

func TestStartupCancellationStopsEarlierProducers(t *testing.T) {
	var calls []string
	m := NewServiceManager()
	blocked := &contextualStartup{entered: make(chan struct{})}
	m.RegisterService(&lifecycleFixture{"producer", &calls, false})
	m.RegisterService(blocked)
	m.RegisterService(&lifecycleFixture{"http", &calls, false})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.StartAllContext(ctx) }()
	<-blocked.entered
	cancel()
	select {
	case err := <-done:
		if err == nil || !blocked.stopped {
			t.Fatalf("canceled service was not cleaned up: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("startup cancellation blocked")
	}
	want := []string{"start:producer", "stop:producer"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected lifecycle %v", calls)
	}
	waited := make(chan struct{})
	go func() { m.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("canceled startup leaked wait group")
	}
	m.StopAll()
	if !reflect.DeepEqual(calls, want) {
		t.Fatal("shutdown repeated stopped components")
	}
}
