package devicemetadata

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"encore.dev/rlog"
)

// Encore runs cron jobs only on Encore Cloud. A local `encore run` or a
// self-hosted build never fires them, so the long-export worker, the expired
// download purge and the runtime outbox dispatcher would sit idle. Setting
// TP_BACKGROUND_WORKERS=on starts the same three jobs from inside the service
// process. They are safe next to the cron jobs: exports are claimed with
// FOR UPDATE SKIP LOCKED under a lease, the purge is idempotent, and the
// dispatcher is documented as at-least-once.

const backgroundWorkersEnv = "TP_BACKGROUND_WORKERS"

var (
	backgroundMu     sync.Mutex
	backgroundCancel context.CancelFunc
)

type backgroundJob struct {
	name  string
	every time.Duration
	run   func(context.Context) error
}

func backgroundJobs() []backgroundJob {
	return []backgroundJob{
		{"process-long-reading-exports", 15 * time.Second, func(ctx context.Context) error { return ProcessLongReadingExports(ctx) }},
		{"dispatch-device-runtime-outbox", time.Minute, func(ctx context.Context) error { return DispatchDeviceRuntimeOutbox(ctx) }},
		{"purge-expired-metadata-downloads", time.Hour, func(ctx context.Context) error { return PurgeExpiredMetadataDownloads(ctx) }},
	}
}

// startBackgroundWorkers is a no-op unless the environment opts in, and starts
// at most one set of workers per process.
func startBackgroundWorkers() {
	if strings.TrimSpace(os.Getenv(backgroundWorkersEnv)) != "on" {
		return
	}
	backgroundMu.Lock()
	defer backgroundMu.Unlock()
	if backgroundCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	backgroundCancel = cancel
	for _, job := range backgroundJobs() {
		go runBackgroundJob(ctx, job)
	}
	rlog.Info("device metadata background workers started")
}

func stopBackgroundWorkers() {
	backgroundMu.Lock()
	defer backgroundMu.Unlock()
	if backgroundCancel != nil {
		backgroundCancel()
		backgroundCancel = nil
	}
}

func runBackgroundJob(ctx context.Context, job backgroundJob) {
	ticker := time.NewTicker(job.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						rlog.Error("background job panicked", "job", job.name, "panic", r)
					}
				}()
				if err := job.run(ctx); err != nil && ctx.Err() == nil {
					rlog.Error("background job failed", "job", job.name, "err", err)
				}
			}()
		}
	}
}

// Shutdown stops the in-process workers when Encore drains the service.
func (s *Service) Shutdown(force context.Context) {
	stopBackgroundWorkers()
}
