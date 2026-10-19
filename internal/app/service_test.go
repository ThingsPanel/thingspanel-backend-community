package app

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

type lifecycleFixture struct {
	name  string
	calls *[]string
	fail  bool
}

func (s *lifecycleFixture) Name() string { return s.name }
func (s *lifecycleFixture) Start() error {
	*s.calls = append(*s.calls, "start:"+s.name)
	if s.fail {
		return errors.New("fixture startup failure")
	}
	return nil
}
func (s *lifecycleFixture) Stop() error { *s.calls = append(*s.calls, "stop:"+s.name); return nil }

func TestStartupFailureStopsEarlierProducers(t *testing.T) {
	var calls []string
	m := NewServiceManager()
	for _, s := range []*lifecycleFixture{{"producer", &calls, false}, {"relay", &calls, true}, {"http", &calls, false}} {
		m.RegisterService(s)
	}
	if m.StartAll() == nil {
		t.Fatal("startup failure was hidden")
	}
	want := []string{"start:producer", "start:relay", "stop:relay", "stop:producer"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected lifecycle %v", calls)
	}
	done := make(chan struct{})
	go func() { m.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("failed startup leaked wait group")
	}
	m.StopAll()
	if !reflect.DeepEqual(calls, want) {
		t.Fatal("shutdown repeated stopped components")
	}
}
