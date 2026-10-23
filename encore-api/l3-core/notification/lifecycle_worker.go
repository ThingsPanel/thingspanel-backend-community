package notification

import "context"

// WorkerLifecycle exposes the worker through the service lifecycle without
// coupling the queue implementation to Encore's lifecycle package.
type WorkerLifecycle struct {
	Worker *Worker
}

func (l WorkerLifecycle) Start(ctx context.Context) error {
	if l.Worker == nil {
		return ErrWorkerInvalidConfig
	}
	return l.Worker.Start(ctx)
}

func (l WorkerLifecycle) Shutdown(ctx context.Context) error {
	if l.Worker == nil {
		return ErrWorkerInvalidConfig
	}
	return l.Worker.Stop(ctx)
}
