package notification

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

type workerStoreSpy struct {
	mu             sync.Mutex
	claims         []DeliveryClaim
	claimed        int
	finished       []DeliveryFinish
	finishCtxValid bool
	cursor         string
	recovered      int
	expired        int
	reconciled     int
}

func (s *workerStoreSpy) ClaimDeliveriesFair(_ context.Context, limit int, _ time.Duration, afterTenant string) ([]DeliveryClaim, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursor = afterTenant
	remaining := s.claims[s.claimed:]
	if len(remaining) > limit {
		remaining = remaining[:limit]
	}
	s.claimed += len(remaining)
	next := afterTenant
	if len(remaining) > 0 {
		next = remaining[len(remaining)-1].TenantID
	}
	return append([]DeliveryClaim(nil), remaining...), next, nil
}

func (s *workerStoreSpy) FinishDelivery(ctx context.Context, finish DeliveryFinish) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishCtxValid = ctx.Err() == nil
	s.finished = append(s.finished, finish)
	return nil
}

func (s *workerStoreSpy) RecoverExpiredClaimsBounded(context.Context, time.Time, int) (int, error) {
	return s.recovered, nil
}

func (s *workerStoreSpy) ExpireQueued(context.Context, time.Time, int) (int, error) {
	return s.expired, nil
}

func (s *workerStoreSpy) ReconcileReceipts(context.Context, int) (int, error) {
	return s.reconciled, nil
}

type workerSenderFunc func(context.Context, contract.PluginSendRequestV1) (contract.PluginSendResultV1, error)

func (f workerSenderFunc) Send(ctx context.Context, request contract.PluginSendRequestV1) (contract.PluginSendResultV1, error) {
	return f(ctx, request)
}

func workerClaim(tenant, delivery string) DeliveryClaim {
	expiresAt := time.Now().Add(time.Minute)
	return DeliveryClaim{TenantID: tenant, DeliveryID: delivery, AttemptID: "attempt-" + delivery, LeaseToken: "lease-" + delivery, ExpiresAt: expiresAt, Request: contract.PluginSendRequestV1{DeliveryID: delivery, AttemptID: "attempt-" + delivery, ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano)}}
}

func TestWorkerRunOnceBoundsConcurrentProviderCallsAndFencesResults(t *testing.T) {
	store := &workerStoreSpy{claims: []DeliveryClaim{workerClaim("tenant-a", "delivery-a"), workerClaim("tenant-b", "delivery-b")}}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var active, maxActive atomic.Int32
	var canceledCalls atomic.Int32
	sender := workerSenderFunc(func(ctx context.Context, request contract.PluginSendRequestV1) (contract.PluginSendResultV1, error) {
		current := active.Add(1)
		for old := maxActive.Load(); current > old && !maxActive.CompareAndSwap(old, current); old = maxActive.Load() {
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			canceledCalls.Add(1)
		}
		active.Add(-1)
		return contract.PluginSendResultV1{DeliveryID: request.DeliveryID, Outcome: "accepted"}, nil
	})
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: sender, Concurrency: 2, PluginTimeout: time.Second, FinishTimeout: time.Second, LeaseFor: 3 * time.Second, BatchSize: 2})
	if err != nil {
		t.Fatal("construct worker")
	}
	resultCh := make(chan WorkerCycle, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := worker.RunOnce(context.Background())
		resultCh <- result
		errCh <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("worker did not start both bounded provider calls")
		}
	}
	close(release)
	select {
	case <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("worker cycle did not complete")
	}
	if err := <-errCh; err != nil {
		t.Fatalf("worker cycle: %v", err)
	}
	if maxActive.Load() != 2 {
		t.Fatalf("maximum concurrent sends=%d, want exactly 2", maxActive.Load())
	}
	if canceledCalls.Load() != 0 {
		t.Fatalf("unexpectedly canceled %d provider sends", canceledCalls.Load())
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.finished) != 2 || !store.finishCtxValid {
		t.Fatalf("finished=%d independent finish context valid=%v", len(store.finished), store.finishCtxValid)
	}
	for _, finish := range store.finished {
		if finish.Result.Outcome != "accepted" || finish.AttemptID == "" || finish.LeaseToken == "" {
			t.Fatalf("finish omitted result fencing: delivery=%s outcome=%s code=%s", finish.DeliveryID, finish.Result.Outcome, finish.Result.Error.Code)
		}
	}
}

func TestWorkerShutdownCancelsSendButPersistsUnknownWithIndependentContext(t *testing.T) {
	store := &workerStoreSpy{claims: []DeliveryClaim{workerClaim("tenant-stop", "delivery-stop")}}
	started := make(chan struct{})
	sender := workerSenderFunc(func(ctx context.Context, request contract.PluginSendRequestV1) (contract.PluginSendResultV1, error) {
		close(started)
		<-ctx.Done()
		return contract.PluginSendResultV1{}, ctx.Err()
	})
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: sender, Concurrency: 1, PluginTimeout: time.Minute, FinishTimeout: time.Second, LeaseFor: 62 * time.Second, PollInterval: 10 * time.Millisecond, BatchSize: 1})
	if err != nil {
		t.Fatal("construct worker")
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start send")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Stop(stopCtx); err != nil {
		t.Fatalf("bounded worker stop: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.finished) != 1 || store.finished[0].Result.Outcome != "unknown" || !store.finishCtxValid {
		t.Fatalf("shutdown should persist unknown with independent context: %+v finishCtx=%v", store.finished, store.finishCtxValid)
	}
}

func TestWorkerMapsSenderErrorsToUnknownWithoutRetryEvidence(t *testing.T) {
	store := &workerStoreSpy{claims: []DeliveryClaim{workerClaim("tenant-error", "delivery-error")}}
	sender := workerSenderFunc(func(_ context.Context, request contract.PluginSendRequestV1) (contract.PluginSendResultV1, error) {
		return contract.PluginSendResultV1{DeliveryID: request.DeliveryID, Outcome: "rejected", Error: &contract.PluginDeliveryErrorV1{Code: "provider_429", Retryable: true}}, errors.New("transport ended after request write")
	})
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: sender, Concurrency: 1, PluginTimeout: time.Second, FinishTimeout: time.Second, LeaseFor: 3 * time.Second, BatchSize: 1})
	if err != nil {
		t.Fatal("construct worker")
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("run worker: %v", err)
	}
	if len(store.finished) != 1 || store.finished[0].Result.Outcome != "unknown" || store.finished[0].Result.Error == nil || store.finished[0].Result.Error.Retryable {
		t.Fatalf("ambiguous sender error authorized a retry: %+v", store.finished)
	}
}

func TestWorkerRejectsLeaseShorterThanSendAndFinishWindow(t *testing.T) {
	_, err := NewWorker(WorkerOptions{Store: &workerStoreSpy{}, Sender: workerSenderFunc(func(context.Context, contract.PluginSendRequestV1) (contract.PluginSendResultV1, error) {
		return contract.PluginSendResultV1{}, nil
	}), Concurrency: 1, PluginTimeout: 10 * time.Second, FinishTimeout: 2 * time.Second, LeaseFor: 11 * time.Second, BatchSize: 1})
	if err == nil {
		t.Fatal("worker accepted a lease shorter than send plus finish timeout")
	}
}
