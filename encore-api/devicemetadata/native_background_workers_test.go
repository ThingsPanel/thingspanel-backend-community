package devicemetadata

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundWorkersAreOptIn(t *testing.T) {
	t.Setenv(backgroundWorkersEnv, "")
	startBackgroundWorkers()
	if backgroundCancel != nil {
		t.Fatal("workers started without opting in")
	}
	jobs := backgroundJobs()
	if len(jobs) != 3 {
		t.Fatalf("expected the three cron jobs to be mirrored, got %d", len(jobs))
	}
}

func TestBackgroundJobRunsOnTickSurvivesPanicAndStops(t *testing.T) {
	var runs atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runBackgroundJob(ctx, backgroundJob{name: "t", every: 5 * time.Millisecond, run: func(context.Context) error {
			if runs.Add(1) == 1 {
				panic("boom")
			}
			return nil
		}})
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for runs.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("job ran only %d times", runs.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not stop after cancellation")
	}
}
