package marketinstall

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"encore.app/marketinstall/app"
	"encore.app/marketinstall/store"
	"github.com/google/uuid"
)

type durableTestProvider struct {
	mu         sync.Mutex
	receipts   map[string]app.Receipt
	executions int
	loseReply  bool
	unknown    bool
}

func (p *durableTestProvider) Prepare(context.Context, app.PrepareRequest) (app.Plan, error) {
	return app.Plan{ArtifactDigest: "sha256:" + strings.Repeat("a", 64), Steps: []app.Step{{ID: "native:create", Owner: "thingmodel", Action: "create", ResourceKey: "native", Payload: json.RawMessage(`{"model":{"name":"fixture"}}`)}}}, nil
}
func (p *durableTestProvider) Query(ctx context.Context, req app.StepRequest) (app.Observation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.unknown {
		return app.Observation{State: app.Unknown}, nil
	}
	if receipt, ok := p.receipts[req.IdempotencyKey]; ok {
		return app.Observation{State: app.Confirmed, Receipt: &receipt}, nil
	}
	return app.Observation{State: app.NotObserved}, nil
}
func (p *durableTestProvider) Execute(ctx context.Context, req app.StepRequest) (app.Observation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.receipts == nil {
		p.receipts = map[string]app.Receipt{}
	}
	receipt, exists := p.receipts[req.IdempotencyKey]
	if !exists {
		p.executions++
		receipt = app.Receipt{Owner: req.Step.Owner, ResourceID: "resource-" + uuid.NewString(), PayloadHash: req.PayloadHash, Data: json.RawMessage(`{"durable":true}`)}
		p.receipts[req.IdempotencyKey] = receipt
	}
	if p.loseReply {
		p.loseReply = false
		return app.Observation{}, errors.New("owner committed, reply lost")
	}
	return app.Observation{State: app.Confirmed, Receipt: &receipt}, nil
}
func newPostgresCoordinator(p *durableTestProvider) *app.Coordinator {
	return &app.Coordinator{Store: store.New(db.Stdlib()), Provider: p, NewID: uuid.NewString, LeaseSeconds: 60}
}
func submitPostgresOperation(t *testing.T, c *app.Coordinator) (app.Operation, app.Identity) {
	t.Helper()
	identity := app.Identity{TenantID: "test-" + uuid.NewString(), SubjectID: "test-" + uuid.NewString(), AuthorizationRef: "fixture-ref"}
	op, err := c.Submit(context.Background(), identity, "request-"+uuid.NewString(), app.Request{BundleKey: "fixture", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM market_install_steps WHERE operation_id=$1`, op.ID)
		_, _ = db.Exec(context.Background(), `DELETE FROM market_install_operations WHERE id=$1`, op.ID)
	})
	return op, identity
}
func readOperation(t *testing.T, op app.Operation) app.Operation {
	t.Helper()
	current, err := store.New(db.Stdlib()).GetByID(context.Background(), op.TenantID, op.SubjectID, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func TestPostgresMarketInstallLostReplyRestartQueriesOriginalKey(t *testing.T) {
	provider := &durableTestProvider{loseReply: true}
	coordinator := newPostgresCoordinator(provider)
	op, identity := submitPostgresOperation(t, coordinator)
	worked, err := coordinator.RunOne(context.Background())
	if err != nil || !worked {
		t.Fatalf("first execution: %v %v", worked, err)
	}
	current := readOperation(t, op)
	if current.State != app.Paused || provider.executions != 1 {
		t.Fatalf("unknown must pause: %#v executions=%d", current, provider.executions)
	}
	if worked, err := coordinator.RunOne(context.Background()); err != nil || worked {
		t.Fatalf("paused operation auto-retried: %v %v", worked, err)
	}
	// Rebuild the application/store exactly as a new process would. No old worker
	// in-memory progress is used; the provider's own receipt is authoritative.
	restarted := newPostgresCoordinator(provider)
	if err := restarted.Store.Resume(context.Background(), identity.TenantID, identity.SubjectID, op.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	current = readOperation(t, op)
	if current.State != app.Completed || provider.executions != 1 || current.Generation != 2 {
		t.Fatalf("restart duplicated effect or lost result: %#v executions=%d", current, provider.executions)
	}
}

func TestPostgresMarketInstallGenerationFenceAndTenant(t *testing.T) {
	coordinator := newPostgresCoordinator(&durableTestProvider{})
	op, _ := submitPostgresOperation(t, coordinator)
	ledger := store.New(db.Stdlib())
	ctx := context.Background()
	old, err := ledger.Claim(ctx, 60)
	if err != nil || old == nil {
		t.Fatalf("claim: %#v %v", old, err)
	}
	if err := ledger.Dispatch(ctx, *old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE market_install_operations SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1`, op.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := ledger.Claim(ctx, 60)
	if err != nil || fresh == nil {
		t.Fatalf("reclaim: %#v %v", fresh, err)
	}
	if fresh.IdempotencyKey != old.IdempotencyKey || fresh.Operation.Generation != old.Operation.Generation+1 {
		t.Fatal("generation changed original step key")
	}
	receipt := app.Receipt{Owner: old.Step.Owner, ResourceID: "original", PayloadHash: old.PayloadHash, Data: json.RawMessage(`{}`)}
	if err := ledger.Confirm(ctx, *old, receipt); !errors.Is(err, app.ErrLeaseLost) {
		t.Fatalf("old generation advanced: %v", err)
	}
	foreign := *fresh
	foreign.Operation.TenantID = "foreign"
	if err := ledger.Confirm(ctx, foreign, receipt); !errors.Is(err, app.ErrLeaseLost) {
		t.Fatalf("foreign tenant advanced: %v", err)
	}
	if _, err := ledger.GetByID(ctx, op.TenantID, "foreign", op.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("foreign subject read: %v", err)
	}
	if err := ledger.Confirm(ctx, *fresh, receipt); err != nil {
		t.Fatal(err)
	}
}

type failOnceConfirmStore struct {
	app.Store
	failed bool
}

func (s *failOnceConfirmStore) Confirm(ctx context.Context, work app.Work, receipt app.Receipt) error {
	if !s.failed {
		s.failed = true
		return errors.New("injected local confirm acknowledgement failure")
	}
	return s.Store.Confirm(ctx, work, receipt)
}

func TestPostgresMarketInstallLocalConfirmFailureRecoversFromOwner(t *testing.T) {
	provider := &durableTestProvider{}
	coordinator := newPostgresCoordinator(provider)
	op, _ := submitPostgresOperation(t, coordinator)
	ctx := context.Background()
	// The runtime database role intentionally cannot create functions/triggers.
	// Inject only this acknowledgement boundary; all intent/state reads, dispatch
	// writes and recovery use the actual PostgreSQL store without elevated rights.
	coordinator.Store = &failOnceConfirmStore{Store: coordinator.Store}
	if _, err := coordinator.RunOne(ctx); err == nil {
		t.Fatal("failed local receipt acknowledgement reported completion")
	}
	if current := readOperation(t, op); current.State != app.Running {
		t.Fatalf("uncommitted completion escaped: %#v", current)
	}
	if provider.executions != 1 {
		t.Fatal("provider effect did not run once")
	}
	if _, err := db.Exec(ctx, `UPDATE market_install_operations SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1`, op.ID); err != nil {
		t.Fatal(err)
	}
	restarted := newPostgresCoordinator(provider)
	if _, err := restarted.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	if current := readOperation(t, op); current.State != app.Completed || provider.executions != 1 {
		t.Fatalf("recovery repeated effect: %#v count=%d", current, provider.executions)
	}
}

func TestPostgresMarketInstallConcurrentReplayAndConflict(t *testing.T) {
	provider := &durableTestProvider{}
	coordinator := newPostgresCoordinator(provider)
	ctx := context.Background()
	identity := app.Identity{TenantID: "test-" + uuid.NewString(), SubjectID: "subject", AuthorizationRef: "fixture"}
	key := "concurrent-" + uuid.NewString()
	req := app.Request{BundleKey: "fixture", Version: "1"}
	var wg sync.WaitGroup
	results := make(chan app.Operation, 20)
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			op, err := coordinator.Submit(ctx, identity, key, req)
			if err != nil {
				failures <- err
			} else {
				results <- op
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var original app.Operation
	for op := range results {
		if original.ID == "" {
			original = op
		} else if original.ID != op.ID {
			t.Fatal("same key created multiple operations")
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM market_install_steps WHERE operation_id=$1`, original.ID)
		_, _ = db.Exec(ctx, `DELETE FROM market_install_operations WHERE id=$1`, original.ID)
	})
	req.Version = "2"
	if _, err := coordinator.Submit(ctx, identity, key, req); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("payload conflict accepted: %v", err)
	}
	foreign := identity
	foreign.SubjectID = "foreign"
	req.Version = "1"
	if _, err := coordinator.Submit(ctx, foreign, key, req); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("same tenant other subject stole key: %v", err)
	}
}
