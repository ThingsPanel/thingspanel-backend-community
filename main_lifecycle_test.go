package main

import (
	"context"
	"testing"
	"time"

	"project/internal/app"
)

type shutdownProbeService struct{ stopped chan struct{} }

func (s *shutdownProbeService) Name() string { return "shutdown probe" }
func (s *shutdownProbeService) Start() error { return nil }
func (s *shutdownProbeService) Stop() error  { close(s.stopped); return nil }

func TestTerminationSignalStopsApplicationWithoutWaitingFirst(t *testing.T) {
	application, err := app.NewApplication()
	if err != nil {
		t.Fatal("construct application")
	}
	probe := &shutdownProbeService{stopped: make(chan struct{})}
	application.RegisterService(probe)
	if err := application.Start(); err != nil {
		t.Fatal("start application")
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		waitForApplicationShutdown(application, runCtx.Done())
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("termination signal did not complete bounded application shutdown")
	}
	select {
	case <-probe.stopped:
	default:
		t.Fatal("application shutdown returned before stopping its service")
	}
}
