package automation

import (
	"context"
	"database/sql"
	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type reviewBlockingCommands struct {
	fakeCommands
	entered chan struct{}
	release chan struct{}
}

func (c *reviewBlockingCommands) Dispatch(ctx context.Context, req ports.CommandRequest) (ports.CommandResult, error) {
	c.mu.Lock()
	c.calls = append(c.calls, req)
	c.mu.Unlock()
	c.entered <- struct{}{}
	select {
	case <-c.release:
	case <-ctx.Done():
		return ports.CommandResult{}, ctx.Err()
	}
	return ports.CommandResult{State: "accepted", Ref: "review-only-owner"}, nil
}

func TestQ09SameRuntimeLiveRecoveryCannotShareExecutionLease(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	ctx, cancel := context.WithTimeout(e.ctx(), 20*time.Second)
	defer cancel()
	fault := &q09BindingFaultConnector{db: e.st.DB}
	pool := sql.OpenDB(fault)
	defer pool.Close()
	claims := make(chan struct{}, 4)
	claimRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseClaims := func() { releaseOnce.Do(func() { close(claimRelease) }) }
	defer releaseClaims()
	var claimCount atomic.Int32
	fault.beforeExec = func(q string) error {
		if strings.Contains(q, "resume_count = resume_count") {
			claimCount.Add(1)
			claims <- struct{}{}
			select {
			case <-claimRelease:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	commands := &reviewBlockingCommands{entered: make(chan struct{}, 4), release: make(chan struct{})}
	var ownerOnce sync.Once
	releaseOwner := func() { ownerOnce.Do(func() { close(commands.release) }) }
	defer releaseOwner()
	rt := runtime.New(store.New(pool), commands, e.notif, e.dir, func() time.Time { return e.now })
	event := store.IngestEvent{EventID: runtime.NewID(), TenantID: e.tenant, DeviceID: e.dev1, Signal: domain.SignalTelemetry, Key: "temperature", Value: 38.0, Quality: domain.QualityGood, ObservedAt: e.now}
	liveDone := make(chan error, 1)
	recoverDone := make(chan error, 1)
	go func() { _, err := rt.Ingest(ctx, event); liveDone <- err }()
	select {
	case <-claims:
	case <-ctx.Done():
		t.Fatal("first accepted run never reached claim")
	}
	go func() { _, err := rt.RecoverAfterRestart(ctx, 100); recoverDone <- err }()
	select {
	case <-claims:
	case <-ctx.Done():
		t.Fatal("same-runtime recovery never reached claim")
	}
	releaseClaims()
	select {
	case <-commands.entered:
	case <-ctx.Done():
		t.Fatal("no owner dispatch")
	}
	duplicate := false
	select {
	case <-commands.entered:
		duplicate = true
	case <-time.After(2 * time.Second):
	}
	releaseOwner()
	for _, ch := range []chan error{liveDone, recoverDone} {
		select {
		case err := <-ch:
			if err != nil {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Fatal("runtime did not finish")
		}
	}
	runs := e.runs(rule.ID)
	var outboxes int
	if err := e.st.DB.QueryRowContext(ctx, "SELECT count(*) FROM auto_outbox WHERE tenant_id=$1", e.tenant).Scan(&outboxes); err != nil {
		t.Fatal(err)
	}
	commands.mu.Lock()
	calls := append([]ports.CommandRequest{}, commands.calls...)
	commands.mu.Unlock()
	keys := []string{}
	for _, c := range calls {
		keys = append(keys, c.IdempotencyKey)
	}
	t.Logf("same_runtime=true claims=%d runs=%d outboxes=%d outbound_calls=%d keys=%v duplicate_overlap=%v", claimCount.Load(), len(runs), outboxes, len(calls), keys, duplicate)
	if claimCount.Load() != 2 || len(runs) != 1 || outboxes != 1 || runs[0].Status != runtime.StatusSucceeded {
		t.Fatal("expected two contenders and one successful run/outbox")
	}
	if len(calls) != 1 {
		t.Fatalf("same Runtime walked one stable run concurrently: want 1 outbound dispatch got %d (same key is not proof of physical double execution)", len(calls))
	}
}

func TestQ09ScopeChangeStableIdentityAdmissionCanRetry(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	ctx, cancel := context.WithTimeout(e.ctx(), 20*time.Second)
	defer cancel()
	connector := &q09BindingFaultConnector{db: e.st.DB}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	st := store.New(pool)
	reached := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	connector.beforeExec = func(q string) error {
		if strings.Contains(q, "INSERT INTO auto_runs") {
			reached <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	one := &store.Run{ID: "event-" + runtime.NewID(), TenantID: e.tenant, RuleID: rule.ID, Revision: rule.Revision, DefinitionHash: rule.DefinitionHash, ScopeKey: "rule:" + rule.ID, DeviceID: e.dev1, TriggerEventID: runtime.NewID(), Status: "accepted", Snapshot: []byte(`{}`), StartedAt: e.now}
	two := *one
	two.ScopeKey += ":device:" + e.dev1
	done := make(chan error, 2)
	go func() { done <- st.AdmitEventRun(ctx, one, nil, rule.Definition) }()
	select {
	case <-reached:
	case <-ctx.Done():
		t.Fatal("first insert barrier")
	}
	go func() { done <- st.AdmitEventRun(ctx, &two, nil, rule.Definition) }()
	select {
	case <-reached:
	case <-ctx.Done():
		t.Fatal("different scope insert barrier")
	}
	unblock()
	err1, err2 := <-done, <-done
	t.Logf("concurrent admission err1=%v err2=%v", err1, err2)
	if (err1 == nil) == (err2 == nil) {
		t.Fatal("want one commit and one unique-conflict retry")
	}
	// Both proposal IDs are identical. Retrying either proposal must read durable winner.
	if err := st.AdmitEventRun(ctx, &two, nil, rule.Definition); err != nil {
		t.Fatal(err)
	}
	runs := e.runs(rule.ID)
	if len(runs) != 1 || runs[0].ID != one.ID {
		t.Fatal("retry changed stable identity")
	}
	t.Logf("retry succeeded; runs=%d winner_scope=%s replay_scope=%s", len(runs), runs[0].ScopeKey, two.ScopeKey)
}

func TestQ09InvocationReleaseDoesNotClearSuccessorLease(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	ctx, cancel := context.WithTimeout(e.ctx(), 15*time.Second)
	defer cancel()
	commands := &reviewBlockingCommands{entered: make(chan struct{}, 2), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(commands.release) }) }
	defer release()
	rt := runtime.New(e.st, commands, e.notif, e.dir, func() time.Time { return e.now })
	done := make(chan error, 1)
	go func() {
		_, err := rt.Ingest(ctx, store.IngestEvent{EventID: runtime.NewID(), TenantID: e.tenant, DeviceID: e.dev1,
			Signal: domain.SignalTelemetry, Key: "temperature", Value: 38.0, Quality: domain.QualityGood, ObservedAt: e.now})
		done <- err
	}()
	select {
	case <-commands.entered:
	case <-ctx.Done():
		t.Fatal("execution did not reach owner")
	}
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatal("expected one run")
	}
	var oldOwner string
	if err := e.st.DB.QueryRowContext(ctx, "SELECT lease_owner FROM auto_runs WHERE id=$1", runs[0].ID).Scan(&oldOwner); err != nil {
		t.Fatal(err)
	}
	// Exercise the existing expiry takeover API, without claiming that this
	// patch fences all state writes made by a worker after its lease expires.
	successor := "qa-successor-" + runtime.NewID()
	claimed, err := e.st.ClaimRun(ctx, runs[0].ID, successor, e.now.Add(3*time.Minute), e.now.Add(5*time.Minute))
	if err != nil || !claimed {
		t.Fatalf("takeover: claimed=%v err=%v", claimed, err)
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("execution did not complete")
	}
	var remaining string
	if err := e.st.DB.QueryRowContext(ctx, "SELECT coalesce(lease_owner,'') FROM auto_runs WHERE id=$1", runs[0].ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if oldOwner == successor || remaining != successor {
		t.Fatalf("stale invocation cleared successor lease: old=%s remaining=%s", oldOwner, remaining)
	}
	t.Log("stale invocation ReleaseRun preserved the successor token; terminal write fencing remains outside this patch")
}
