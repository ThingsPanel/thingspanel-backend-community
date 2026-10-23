package notification

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"encore.app/l3-core/notification/contract"
)

var (
	ErrWorkerInvalidConfig = errors.New("notification worker configuration is invalid")
	ErrWorkerStarted       = errors.New("notification worker is already started")
	ErrWorkerStopped       = errors.New("notification worker is stopped")
	ErrWorkerPersistResult = errors.New("notification worker could not persist a result")
)

type WorkerStore interface {
	ClaimDeliveriesFair(ctx context.Context, limit int, leaseFor time.Duration, afterTenant string) ([]DeliveryClaim, string, error)
	FinishDelivery(ctx context.Context, finish DeliveryFinish) error
	RecoverExpiredClaimsBounded(ctx context.Context, now time.Time, limit int) (int, error)
	ExpireQueued(ctx context.Context, now time.Time, limit int) (int, error)
	ReconcileReceipts(ctx context.Context, limit int) (int, error)
}

type WorkerOptions struct {
	Store          WorkerStore
	Sender         PluginSender
	Clock          func() time.Time
	PollInterval   time.Duration
	Concurrency    int
	BatchSize      int
	PluginTimeout  time.Duration
	FinishTimeout  time.Duration
	LeaseFor       time.Duration
	MaintenanceCap int
}

type WorkerCycle struct {
	RecoveredClaims   int
	ExpiredQueued     int
	ReconciledReceipt int
	Claimed           int
	Finished          int
	Unknown           int
}

type WorkerStats struct {
	Cycles             uint64
	CycleErrors        uint64
	Claims             uint64
	Finished           uint64
	UnknownResults     uint64
	RecoveredClaims    uint64
	ExpiredDeliveries  uint64
	ReconciledReceipts uint64
}

type workerCounters struct {
	cycles      atomic.Uint64
	cycleErrors atomic.Uint64
	claims      atomic.Uint64
	finished    atomic.Uint64
	unknown     atomic.Uint64
	recovered   atomic.Uint64
	expired     atomic.Uint64
	reconciled  atomic.Uint64
}

// Worker consumes committed notification attempts. Provider calls are
// bounded by worker concurrency; each persisted result uses an independent
// short context so shutdown cancellation cannot discard a known outcome.
type Worker struct {
	store          WorkerStore
	sender         PluginSender
	now            func() time.Time
	pollInterval   time.Duration
	concurrency    int
	batchSize      int
	pluginTimeout  time.Duration
	finishTimeout  time.Duration
	leaseFor       time.Duration
	maintenanceCap int

	cycleMu sync.Mutex
	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	done    chan struct{}
	cursor  string
	stats   workerCounters
}

func NewWorker(options WorkerOptions) (*Worker, error) {
	if options.Store == nil || options.Sender == nil {
		return nil, ErrWorkerInvalidConfig
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.PollInterval == 0 {
		options.PollInterval = time.Second
	}
	if options.Concurrency == 0 {
		options.Concurrency = 4
	}
	if options.BatchSize == 0 {
		options.BatchSize = options.Concurrency
	}
	if options.PluginTimeout == 0 {
		options.PluginTimeout = 8 * time.Second
	}
	if options.FinishTimeout == 0 {
		options.FinishTimeout = 3 * time.Second
	}
	if options.LeaseFor == 0 {
		options.LeaseFor = options.PluginTimeout + options.FinishTimeout + time.Second
	}
	if options.MaintenanceCap == 0 {
		options.MaintenanceCap = 100
	}
	if options.PollInterval < 10*time.Millisecond || options.PollInterval > time.Minute || options.Concurrency < 1 || options.Concurrency > 32 || options.BatchSize < 1 || options.BatchSize > options.Concurrency || options.PluginTimeout < 100*time.Millisecond || options.PluginTimeout > time.Minute || options.FinishTimeout < 100*time.Millisecond || options.FinishTimeout > 30*time.Second || options.LeaseFor < options.PluginTimeout+options.FinishTimeout || options.LeaseFor > 5*time.Minute || options.MaintenanceCap < 1 || options.MaintenanceCap > 1000 {
		return nil, ErrWorkerInvalidConfig
	}
	return &Worker{
		store: options.Store, sender: options.Sender, now: options.Clock,
		pollInterval: options.PollInterval, concurrency: options.Concurrency,
		batchSize: options.BatchSize, pluginTimeout: options.PluginTimeout,
		finishTimeout: options.FinishTimeout, leaseFor: options.LeaseFor,
		maintenanceCap: options.MaintenanceCap,
	}, nil
}

// Start launches one bounded polling loop. It is intended for service startup,
// never for a per-request handler.
func (w *Worker) Start(ctx context.Context) error {
	if w == nil || ctx == nil {
		return ErrWorkerInvalidConfig
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return ErrWorkerStopped
	}
	if w.started {
		return ErrWorkerStarted
	}
	loopCtx, cancel := context.WithCancel(ctx)
	w.started, w.cancel, w.done = true, cancel, make(chan struct{})
	go w.loop(loopCtx, w.done)
	return nil
}

// Stop prevents new claims and waits only until ctx ends. In-flight sends are
// canceled; their result is persisted as unknown because the external side
// effect may already have occurred.
func (w *Worker) Stop(ctx context.Context) error {
	if w == nil || ctx == nil {
		return ErrWorkerInvalidConfig
	}
	w.mu.Lock()
	if w.stopped {
		done := w.done
		w.mu.Unlock()
		if done == nil {
			return nil
		}
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	w.stopped = true
	if !w.started {
		w.mu.Unlock()
		return nil
	}
	cancel, done := w.cancel, w.done
	w.mu.Unlock()
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) RunOnce(ctx context.Context) (WorkerCycle, error) {
	if w == nil || ctx == nil {
		return WorkerCycle{}, ErrWorkerInvalidConfig
	}
	w.mu.Lock()
	stopped := w.stopped
	w.mu.Unlock()
	if stopped {
		return WorkerCycle{}, ErrWorkerStopped
	}
	w.cycleMu.Lock()
	defer w.cycleMu.Unlock()
	w.stats.cycles.Add(1)
	cycle, err := w.runCycle(ctx)
	if err != nil {
		w.stats.cycleErrors.Add(1)
	}
	return cycle, err
}

func (w *Worker) Stats() WorkerStats {
	return WorkerStats{
		Cycles: w.stats.cycles.Load(), CycleErrors: w.stats.cycleErrors.Load(),
		Claims: w.stats.claims.Load(), Finished: w.stats.finished.Load(),
		UnknownResults: w.stats.unknown.Load(), RecoveredClaims: w.stats.recovered.Load(),
		ExpiredDeliveries: w.stats.expired.Load(), ReconciledReceipts: w.stats.reconciled.Load(),
	}
}

func (w *Worker) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	for ctx.Err() == nil {
		_, _ = w.RunOnce(ctx)
		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) runCycle(ctx context.Context) (WorkerCycle, error) {
	var cycle WorkerCycle
	now := w.now().UTC()
	recovered, err := w.store.RecoverExpiredClaimsBounded(ctx, now, w.maintenanceCap)
	if err != nil {
		return cycle, err
	}
	cycle.RecoveredClaims = recovered
	w.stats.recovered.Add(uint64(recovered))
	expired, err := w.store.ExpireQueued(ctx, now, w.maintenanceCap)
	if err != nil {
		return cycle, err
	}
	cycle.ExpiredQueued = expired
	w.stats.expired.Add(uint64(expired))
	reconciled, err := w.store.ReconcileReceipts(ctx, w.maintenanceCap)
	if err != nil {
		return cycle, err
	}
	cycle.ReconciledReceipt = reconciled
	w.stats.reconciled.Add(uint64(reconciled))
	if ctx.Err() != nil {
		return cycle, nil
	}
	claims, nextTenant, err := w.store.ClaimDeliveriesFair(ctx, w.batchSize, w.leaseFor, w.cursor)
	if err != nil {
		return cycle, err
	}
	if len(claims) > w.concurrency {
		return cycle, fmt.Errorf("worker store violated claim limit")
	}
	cycle.Claimed = len(claims)
	w.stats.claims.Add(uint64(len(claims)))
	if len(claims) > 0 {
		w.cursor = nextTenant
	}
	if len(claims) == 0 {
		return cycle, nil
	}
	type outcome struct {
		unknown bool
		err     error
	}
	outcomes := make(chan outcome, len(claims))
	var wg sync.WaitGroup
	for _, claim := range claims {
		claim := claim
		wg.Add(1)
		go func() {
			defer wg.Done()
			unknown, err := w.processClaim(ctx, claim)
			outcomes <- outcome{unknown: unknown, err: err}
		}()
	}
	wg.Wait()
	close(outcomes)
	var persistErrors []error
	for result := range outcomes {
		if result.unknown {
			cycle.Unknown++
			w.stats.unknown.Add(1)
		}
		if result.err != nil {
			persistErrors = append(persistErrors, result.err)
		} else {
			cycle.Finished++
			w.stats.finished.Add(1)
		}
	}
	if len(persistErrors) > 0 {
		return cycle, errors.Join(append([]error{ErrWorkerPersistResult}, persistErrors...)...)
	}
	return cycle, nil
}

func (w *Worker) processClaim(parent context.Context, claim DeliveryClaim) (bool, error) {
	result := uncertainPluginSubmission(claim.DeliveryID)
	if parent.Err() == nil {
		deadline, parseErr := time.Parse(time.RFC3339Nano, claim.Request.ExpiresAt)
		if parseErr == nil && deadline.After(w.now()) {
			callDeadline := w.now().Add(w.pluginTimeout)
			if deadline.Before(callDeadline) {
				callDeadline = deadline
			}
			callCtx, cancel := context.WithDeadline(parent, callDeadline)
			sendResult, err := invokePluginSender(callCtx, w.sender, claim.Request)
			cancel()
			if err != nil {
				result = uncertainPluginSubmission(claim.DeliveryID)
			} else {
				result = normalizeWorkerResult(claim.DeliveryID, sendResult)
			}
		}
	}
	wasUnknown := result.Outcome == "unknown"
	finishCtx, cancel := context.WithTimeout(context.Background(), w.finishTimeout)
	defer cancel()
	finish := DeliveryFinish{
		TenantID: claim.TenantID, DeliveryID: claim.DeliveryID,
		AttemptID: claim.AttemptID, LeaseToken: claim.LeaseToken,
		Result: result, FinishedAt: w.now().UTC(),
	}
	return wasUnknown, w.store.FinishDelivery(finishCtx, finish)
}

func invokePluginSender(ctx context.Context, sender PluginSender, request contract.PluginSendRequestV1) (result contract.PluginSendResultV1, err error) {
	defer func() {
		if recover() != nil {
			result, err = uncertainPluginSubmission(request.DeliveryID), errors.New("plugin sender panicked")
		}
	}()
	return sender.Send(ctx, request)
}

func normalizeWorkerResult(deliveryID string, result contract.PluginSendResultV1) contract.PluginSendResultV1 {
	if result.DeliveryID != deliveryID || result.Outcome != "accepted" && result.Outcome != "rejected" && result.Outcome != "unknown" || result.Outcome == "rejected" && (result.Error == nil || result.Error.Code == "") {
		return uncertainPluginSubmission(deliveryID)
	}
	return result
}

var _ WorkerStore = (*Store)(nil)
