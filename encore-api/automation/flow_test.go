package automation

import (
	"context"
	"sync"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// Test doubles for the outbound ports. They record intents so a test can assert
// exactly what left the process, and they never touch a real device.
// ---------------------------------------------------------------------------

type fakeCommands struct {
	mu    sync.Mutex
	calls []ports.CommandRequest
	state string
}

func (f *fakeCommands) Dispatch(_ context.Context, req ports.CommandRequest) (ports.CommandResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	st := f.state
	if st == "" {
		st = "accepted"
	}
	return ports.CommandResult{State: st, Ref: "ref-1", Message: "fake owner"}, nil
}

func (f *fakeCommands) Status(context.Context, string) (string, error) { return "accepted", nil }

func (f *fakeCommands) ActivateScene(context.Context, string, string) (string, string, string, error) {
	return "unknown", "", "no scene owner in test", ports.ErrNotConfigured
}

func (f *fakeCommands) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeCommands) last() (ports.CommandRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ports.CommandRequest{}, false
	}
	return f.calls[len(f.calls)-1], true
}

type fakeNotifier struct {
	mu    sync.Mutex
	calls []ports.NotificationRequest
}

func (f *fakeNotifier) Submit(_ context.Context, req ports.NotificationRequest) (ports.NotificationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	return ports.NotificationResult{State: "accepted", NotificationID: "n-1", Message: "intake"}, nil
}

type fakeDirectory struct {
	devices map[string]ports.Device
}

// visible mirrors the real directory, which only ever answers for the tenant
// that asked. Without this the fake would let a cross-tenant device through and
// hide exactly the leak the tests are meant to catch.
func (d *fakeDirectory) visible(dev ports.Device, tenantID string) bool {
	return dev.TenantID == "" || tenantID == "" || dev.TenantID == tenantID
}

func (d *fakeDirectory) ByIDs(_ context.Context, tenantID string, ids []string) ([]ports.Device, error) {
	var out []ports.Device
	for _, id := range ids {
		if dev, ok := d.devices[id]; ok && d.visible(dev, tenantID) {
			out = append(out, dev)
		}
	}
	return out, nil
}

func (d *fakeDirectory) ByDeviceConfig(_ context.Context, tenantID string, cfgIDs []string) ([]ports.Device, error) {
	want := map[string]bool{}
	for _, c := range cfgIDs {
		want[c] = true
	}
	var out []ports.Device
	for _, dev := range d.devices {
		if want[dev.DeviceConfigID] && d.visible(dev, tenantID) {
			out = append(out, dev)
		}
	}
	return out, nil
}

func (d *fakeDirectory) ByLabels(_ context.Context, tenantID string, labels []string, matchAll bool) ([]ports.Device, error) {
	var out []ports.Device
	for _, dev := range d.devices {
		// Same tokeniser as the HTTP directory, so a test cannot pass here and
		// fail against the real backend.
		have := map[string]bool{}
		for _, tok := range ports.LabelTokens(dev.Label) {
			have[tok] = true
		}
		hits := 0
		for _, l := range labels {
			if have[l] {
				hits++
			}
		}
		if !d.visible(dev, tenantID) {
			continue
		}
		if (matchAll && hits == len(labels)) || (!matchAll && hits > 0) {
			out = append(out, dev)
		}
	}
	return out, nil
}

func (d *fakeDirectory) ByType(_ context.Context, tenantID, deviceType string) ([]ports.Device, error) {
	var out []ports.Device
	for _, dev := range d.devices {
		if d.visible(dev, tenantID) && dev.DeviceType == deviceType {
			out = append(out, dev)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Environment
// ---------------------------------------------------------------------------

type testEnv struct {
	t      *testing.T
	rt     *runtime.Runtime
	st     *store.Store
	now    time.Time
	tenant string
	dev1   string
	dev2   string
	cmds   *fakeCommands
	notif  *fakeNotifier
	dir    *fakeDirectory
}

// newEnv builds a runtime over the test database with a controllable clock.
//
// Each test gets its own tenant *and* its own device ids. auto_state is keyed by
// (device_id, signal, key) because community device ids are globally unique, so
// reusing an id across tests would let one test's latest value leak into another.
func newEnv(t *testing.T) *testEnv {
	t.Helper()
	suffix := runtime.NewID()[:8]
	e := &testEnv{
		t: t, st: store.New(db.Stdlib()),
		now:    time.Now().UTC().Truncate(time.Second),
		tenant: "test-" + runtime.NewID(),
		dev1:   "dev-1-" + suffix,
		dev2:   "dev-2-" + suffix,
		cmds:   &fakeCommands{},
		notif:  &fakeNotifier{},
	}
	e.dir = &fakeDirectory{devices: map[string]ports.Device{
		e.dev1: {ID: e.dev1, TenantID: e.tenant, Name: "设备1", Label: "floor-1",
			DeviceConfigID: "cfg-1", DeviceType: "sensor", Online: 1},
		e.dev2: {ID: e.dev2, TenantID: e.tenant, Name: "设备2", Label: "floor-1",
			DeviceConfigID: "cfg-1", DeviceType: "sensor", Online: 1},
	}}
	e.rt = runtime.New(e.st, e.cmds, e.notif, e.dir, func() time.Time { return e.now })

	// The worker sweeps the whole database, which is correct in production and
	// a trap in tests: a run left waiting by an earlier test would be resumed by
	// a later test's tick and dispatched through the wrong fake owner. Closing
	// this test's own unfinished work keeps every assertion local.
	t.Cleanup(func() { e.closeUnfinishedWork() })
	return e
}

// closeUnfinishedWork cancels everything this test left running, so no other
// test's clock advance can wake it up.
func (e *testEnv) closeUnfinishedWork() {
	bg := context.Background()
	_, _ = e.st.DB.ExecContext(bg, `
        UPDATE auto_runs SET status='cancelled', reason_code='test_teardown', finished_at=now()
         WHERE tenant_id=$1 AND status IN ('accepted','running','queued')`, e.tenant)
	_, _ = e.st.DB.ExecContext(bg, `
        UPDATE auto_run_timers SET state='cancelled'
         WHERE state='pending' AND run_id IN (SELECT id FROM auto_runs WHERE tenant_id=$1)`, e.tenant)
	_, _ = e.st.DB.ExecContext(bg, `
        UPDATE auto_outbox SET state='cancelled', updated_at=now()
         WHERE tenant_id=$1 AND state IN ('pending','dispatched')`, e.tenant)
	_, _ = e.st.DB.ExecContext(bg, `
        DELETE FROM auto_pending WHERE tenant_id=$1`, e.tenant)
}

func (e *testEnv) advance(d time.Duration) { e.now = e.now.Add(d) }

func (e *testEnv) ctx() context.Context { return context.Background() }

// createRule validates and stores a definition, failing the test if the body is
// not publishable. Using the same validation the API uses keeps the tests honest.
func (e *testEnv) createRule(def domain.Definition) *store.Rule {
	e.t.Helper()
	def.Normalize()
	if err := domain.Validate(&def); err != nil {
		e.t.Fatalf("test rule is invalid: %v", err)
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: e.tenant, Name: "测试规则", Kind: def.Kind,
		Enabled: true, Definition: def, DefinitionHash: domain.DefinitionHash(def),
	}
	if err := e.st.CreateRule(e.ctx(), rule); err != nil {
		e.t.Fatalf("create rule: %v", err)
	}
	// CreateRule writes revision=1 and version=1; re-read so the caller works
	// with the stored values instead of the zero-value struct it passed in.
	stored, err := e.st.GetRule(e.ctx(), e.tenant, rule.ID)
	if err != nil {
		e.t.Fatalf("reload rule: %v", err)
	}
	return stored
}

func (e *testEnv) ingest(eventID, device, key string, value any, quality string) *store.Receipt {
	e.t.Helper()
	if quality == "" {
		quality = string(domain.QualityGood)
	}
	rec, err := e.rt.Ingest(e.ctx(), store.IngestEvent{
		EventID: eventID, TenantID: e.tenant, DeviceID: device,
		Signal: domain.SignalTelemetry, Key: key, Value: value, Unit: "℃",
		Quality: domain.Quality(quality), ObservedAt: e.now,
	})
	if err != nil {
		e.t.Fatalf("ingest: %v", err)
	}
	return rec
}

func (e *testEnv) runs(ruleID string) []*store.Run {
	e.t.Helper()
	runs, err := e.st.ListRuns(e.ctx(), e.tenant, ruleID, "", 100)
	if err != nil {
		e.t.Fatalf("list runs: %v", err)
	}
	return runs
}

func (e *testEnv) tick() runtime.TickResult {
	e.t.Helper()
	res, err := e.rt.Tick(e.ctx())
	if err != nil {
		e.t.Fatalf("tick: %v", err)
	}
	return res
}

// baseDefinition is the smallest legal automation rule: one condition, one
// telemetry action on the triggering device.
func (e *testEnv) baseDefinition() domain.Definition {
	return domain.Definition{
		Kind:  domain.KindAutomation,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "30", Unit: "℃",
		}}}}},
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{e.dev1}},
		Steps: []domain.Step{{
			Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1",
		}},
	}
}

// ---------------------------------------------------------------------------
// Ingress idempotency
// ---------------------------------------------------------------------------

// TestIngestIsIdempotent is the duplicate-action guard: the same broker message
// delivered twice must not run the rule twice.
func TestIngestIsIdempotent(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())

	first := e.ingest("ev-dup-1", e.dev1, "temperature", 35.0, "")
	if first.Duplicate {
		t.Fatal("first delivery must not be a duplicate")
	}
	second := e.ingest("ev-dup-1", e.dev1, "temperature", 35.0, "")
	if !second.Duplicate {
		t.Fatal("second delivery must be reported as duplicate")
	}
	if got := len(e.runs(rule.ID)); got != 1 {
		t.Fatalf("want exactly 1 run, got %d", got)
	}
	if got := e.cmds.count(); got != 1 {
		t.Fatalf("want exactly 1 dispatched command, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Continuous condition
// ---------------------------------------------------------------------------

// TestContinuousConditionWaitsForFor: a rule with for=5m must not act on the
// first satisfying sample, and must act once the window elapses.
func TestContinuousConditionWaitsForFor(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(5 * time.Minute)
	rule := e.createRule(def)

	e.ingest("ev-for-1", e.dev1, "temperature", 35.0, "")
	if got := len(e.runs(rule.ID)); got != 0 {
		t.Fatalf("must not run before the window elapses, got %d runs", got)
	}
	if _, err := e.st.GetPending(e.ctx(), runtime.ScopeKey(&def, rule.ID, e.dev1)); err != nil {
		t.Fatalf("pending record must exist: %v", err)
	}

	e.advance(4 * time.Minute)
	if res := e.tick(); res.PendingFired != 0 {
		t.Fatalf("must not fire at 4m, fired=%d", res.PendingFired)
	}

	e.advance(1 * time.Minute)
	if res := e.tick(); res.PendingFired != 1 {
		t.Fatalf("want 1 fired at 5m, got %d", res.PendingFired)
	}
	runs := e.runs(rule.ID)
	if len(runs) != 1 || runs[0].Status != runtime.StatusSucceeded {
		t.Fatalf("want 1 succeeded run, got %+v", runs)
	}
	if e.cmds.count() != 1 {
		t.Fatalf("want 1 command, got %d", e.cmds.count())
	}
}

// TestConditionFalseCancelsPending: a normal sample inside the window must cancel
// the window, otherwise a flapping sensor would still fire.
func TestConditionFalseCancelsPending(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(5 * time.Minute)
	rule := e.createRule(def)

	e.ingest("ev-cancel-1", e.dev1, "temperature", 35.0, "")
	e.advance(time.Minute)
	e.ingest("ev-cancel-2", e.dev1, "temperature", 20.0, "")

	e.advance(5 * time.Minute)
	if res := e.tick(); res.PendingFired != 0 {
		t.Fatalf("cancelled window must not fire, fired=%d", res.PendingFired)
	}
	if got := len(e.runs(rule.ID)); got != 0 {
		t.Fatalf("want 0 runs, got %d", got)
	}
}

// TestInputProgressBarrier is V31: a sample received before the deadline but not
// yet evaluated must defer the timer instead of being overtaken by it.
func TestInputProgressBarrier(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(5 * time.Minute)
	rule := e.createRule(def)
	scope := runtime.ScopeKey(&def, rule.ID, e.dev1)

	e.ingest("ev-v31-1", e.dev1, "temperature", 35.0, "")

	// A second sample arrives at 4:59 and is *received* but not evaluated yet.
	e.advance(299 * time.Second)
	if _, err := e.st.InsertEvent(e.ctx(), &store.IngestEvent{
		EventID: "ev-v31-2", TenantID: e.tenant, DeviceID: e.dev1,
		Signal: domain.SignalTelemetry, Key: "temperature", Value: 20.0, Unit: "℃",
		Quality: domain.QualityGood, ObservedAt: e.now,
	}); err != nil {
		t.Fatalf("insert pending receipt: %v", err)
	}

	e.advance(time.Second) // 5:00, the deadline
	res := e.tick()
	if res.PendingDeferred != 1 {
		t.Fatalf("want the deadline deferred by the input barrier, got %+v", res)
	}
	if got := len(e.runs(rule.ID)); got != 0 {
		t.Fatalf("must not act while input is behind, got %d runs", got)
	}

	// The receipt is evaluated now; the next pass may decide.
	if err := e.st.MarkProcessed(e.ctx(), "ev-v31-2"); err != nil {
		t.Fatalf("mark processed: %v", err)
	}
	e.advance(2 * time.Second)
	if res := e.tick(); res.PendingFired != 1 {
		t.Fatalf("want fired after the barrier cleared, got %+v", res)
	}
	if _, err := e.st.GetPending(e.ctx(), scope); err == nil {
		t.Fatal("pending must be cleared after firing")
	}
}

// TestStaleEvidenceDoesNotFire: a timer firing is not proof that the physical
// condition still holds. Expired evidence must drop the window, not act on it.
func TestStaleEvidenceDoesNotFire(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(5 * time.Minute)
	def.Freshness = domain.Freshness{
		MaxAge: domain.Duration(2 * time.Minute),
		MaxGap: domain.Duration(2 * time.Minute),
	}
	rule := e.createRule(def)

	e.ingest("ev-stale-1", e.dev1, "temperature", 35.0, "")
	e.advance(5 * time.Minute)
	res := e.tick()
	if res.PendingDropped != 1 {
		t.Fatalf("want the window dropped for stale evidence, got %+v", res)
	}
	if got := len(e.runs(rule.ID)); got != 0 {
		t.Fatalf("stale evidence must not act, got %d runs", got)
	}
}

// ---------------------------------------------------------------------------
// Run modes
// ---------------------------------------------------------------------------

// waitingDefinition keeps a run alive so the run-mode contract is observable.
func (e *testEnv) waitingDefinition(mode domain.RunMode) domain.Definition {
	def := e.baseDefinition()
	def.Mode = mode
	def.Steps = []domain.Step{
		{Kind: domain.StepWait, Wait: domain.Duration(10 * time.Minute)},
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
	}
	return def
}

// TestSingleModeRecordsIgnored: single mode must not silently drop a trigger; it
// records the decision so the user can see why nothing happened.
func TestSingleModeRecordsIgnored(t *testing.T) {
	e := newEnv(t)
	def := e.waitingDefinition(domain.ModeSingle)
	rule := e.createRule(def)

	e.ingest("ev-single-1", e.dev1, "temperature", 35.0, "")
	e.advance(time.Second)
	e.ingest("ev-single-2", e.dev1, "temperature", 35.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 2 {
		t.Fatalf("want 2 run records, got %d", len(runs))
	}
	var ignored *store.Run
	for _, r := range runs {
		if r.Status == runtime.StatusIgnored {
			ignored = r
		}
	}
	if ignored == nil {
		t.Fatalf("want one ignored run, got %+v", runs)
	}
	if ignored.ReasonCode != "mode_single_busy" {
		t.Fatalf("want reason mode_single_busy, got %q", ignored.ReasonCode)
	}
}

// TestRestartModeCancelsPrevious: restart mode replaces the in-flight run and
// records a new generation.
func TestRestartModeCancelsPrevious(t *testing.T) {
	e := newEnv(t)
	def := e.waitingDefinition(domain.ModeRestart)
	rule := e.createRule(def)

	e.ingest("ev-restart-1", e.dev1, "temperature", 35.0, "")
	first := e.runs(rule.ID)
	if len(first) != 1 || first[0].Status != runtime.StatusRunning {
		t.Fatalf("want 1 running run, got %+v", first)
	}

	e.advance(time.Second)
	e.ingest("ev-restart-2", e.dev1, "temperature", 35.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	var cancelled, running int
	for _, r := range runs {
		switch r.Status {
		case runtime.StatusCancelled:
			cancelled++
		case runtime.StatusRunning:
			running++
		}
	}
	if cancelled != 1 || running != 1 {
		t.Fatalf("want 1 cancelled + 1 running, got %+v", runs)
	}
	// The cancelled run must not leave a live continuation behind.
	for _, r := range runs {
		if r.Status == runtime.StatusCancelled {
			if r.ReasonCode != "mode_restart" {
				t.Fatalf("want reason mode_restart, got %q", r.ReasonCode)
			}
		}
	}
}

// TestQueuedModeOverflow: queued mode admits up to max_depth and rejects beyond
// it, with a readable reason instead of a silent drop.
func TestQueuedModeOverflow(t *testing.T) {
	e := newEnv(t)
	def := e.waitingDefinition(domain.ModeQueued)
	def.Queue = &domain.QueueRule{TTL: domain.Duration(2 * time.Minute), MaxDepth: 1}
	rule := e.createRule(def)

	e.ingest("ev-q-1", e.dev1, "temperature", 35.0, "")
	e.advance(time.Second)
	e.ingest("ev-q-2", e.dev1, "temperature", 35.0, "")
	e.advance(time.Second)
	e.ingest("ev-q-3", e.dev1, "temperature", 35.0, "")

	var running, queued, rejected int
	for _, r := range e.runs(rule.ID) {
		switch r.Status {
		case runtime.StatusRunning:
			running++
		case runtime.StatusQueued:
			queued++
		case runtime.StatusRejected:
			rejected++
			if r.ReasonCode != "queue_overflow" {
				t.Fatalf("want queue_overflow, got %q", r.ReasonCode)
			}
		}
	}
	if running != 1 || queued != 1 || rejected != 1 {
		t.Fatalf("want 1 running/1 queued/1 rejected, got %+v", e.runs(rule.ID))
	}
}

// TestParallelModeRejectsOverLimit: parallel mode is bounded too.
func TestParallelModeRejectsOverLimit(t *testing.T) {
	e := newEnv(t)
	def := e.waitingDefinition(domain.ModeParallel)
	def.Parallel = &domain.ParallelRule{MaxConcurrent: 1, TTL: domain.Duration(5 * time.Minute)}
	rule := e.createRule(def)

	e.ingest("ev-p-1", e.dev1, "temperature", 35.0, "")
	e.advance(time.Second)
	e.ingest("ev-p-2", e.dev1, "temperature", 35.0, "")

	rejected := 0
	for _, r := range e.runs(rule.ID) {
		if r.Status == runtime.StatusRejected && r.ReasonCode == "parallel_limit" {
			rejected++
		}
	}
	if rejected != 1 {
		t.Fatalf("want 1 rejected by parallel limit, got %+v", e.runs(rule.ID))
	}
}

// ---------------------------------------------------------------------------
// Finite execution
// ---------------------------------------------------------------------------

// TestWaitStepResumesFromProgramCounter: a wait is a durable timer, not a
// goroutine. The run resumes after the due time and dispatches exactly once.
func TestWaitStepResumes(t *testing.T) {
	e := newEnv(t)
	def := e.waitingDefinition(domain.ModeSingle)
	def.Steps[0].Wait = domain.Duration(time.Minute)
	rule := e.createRule(def)

	e.ingest("ev-wait-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 || runs[0].Status != runtime.StatusRunning {
		t.Fatalf("want a running run waiting on the timer, got %+v", runs)
	}
	if e.cmds.count() != 0 {
		t.Fatal("the action must not run before the wait elapses")
	}

	e.advance(61 * time.Second)
	if res := e.tick(); res.RunTimers != 1 {
		t.Fatalf("want 1 run timer fired, got %+v", res)
	}
	runs = e.runs(rule.ID)
	if len(runs) != 1 || runs[0].Status != runtime.StatusSucceeded {
		t.Fatalf("want the run to succeed after the wait, got %+v", runs)
	}
	if e.cmds.count() != 1 {
		t.Fatalf("want exactly 1 command, got %d", e.cmds.count())
	}
	cmd, _ := e.cmds.last()
	if cmd.DeviceID != e.dev1 || cmd.Value != "1" || cmd.Kind != ports.CommandTelemetry {
		t.Fatalf("unexpected command intent: %+v", cmd)
	}
	if cmd.IdempotencyKey == "" {
		t.Fatal("the intent must carry a stable idempotency key")
	}
}

// TestBranchUnknownStopsRun: an undecidable branch must stop with an explanation
// and must not fall into the else branch.
func TestBranchUnknownStopsRun(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = 0
	def.Steps = []domain.Step{{
		Kind: domain.StepBranch,
		Condition: &domain.Group{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "humidity",
			Operator: domain.OpGt, Value: "50",
		}}},
		Then: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "1"}},
		Else: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "0"}},
	}}
	rule := e.createRule(def)

	e.ingest("ev-branch-1", e.dev1, "temperature", 35.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	if runs[0].Status != runtime.StatusFailed || runs[0].ReasonCode != "branch_undetermined" {
		t.Fatalf("want failed/branch_undetermined, got %s/%s", runs[0].Status, runs[0].ReasonCode)
	}
	if e.cmds.count() != 0 {
		t.Fatalf("neither branch may run when the verdict is unknown, dispatched=%d", e.cmds.count())
	}
}

// TestBranchFalseTakesElse: a definite false takes the else branch.
func TestBranchFalseTakesElse(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Steps = []domain.Step{{
		Kind: domain.StepBranch,
		Condition: &domain.Group{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "50", Unit: "℃",
		}}},
		Then: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "1"}},
		Else: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "0"}},
	}}
	rule := e.createRule(def)

	e.ingest("ev-branch-2", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 || runs[0].Status != runtime.StatusSucceeded {
		t.Fatalf("want a succeeded run, got %+v", runs)
	}
	cmd, ok := e.cmds.last()
	if !ok || cmd.Value != "0" {
		t.Fatalf("want the else action (value 0), got %+v", cmd)
	}
}

// TestNotificationStepUsesNotificationOwner: the notify step goes through the
// notification owner port and is never reimplemented locally.
func TestNotificationStepUsesNotificationOwner(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Steps = []domain.Step{{
		Kind: domain.StepNotify, NotificationPolicyID: "np-1", Value: "温度过高",
	}}
	e.createRule(def)

	e.ingest("ev-notify-1", e.dev1, "temperature", 35.0, "")
	if len(e.notif.calls) != 1 {
		t.Fatalf("want 1 notification intent, got %d", len(e.notif.calls))
	}
	if e.notif.calls[0].PolicyID != "np-1" {
		t.Fatalf("want policy np-1, got %q", e.notif.calls[0].PolicyID)
	}
}

// TestUnknownCommandResultIsNotSuccess: a command owner that cannot confirm must
// leave the intent retryable and the step unknown, never "succeeded".
func TestUnknownCommandResultIsNotSuccess(t *testing.T) {
	e := newEnv(t)
	e.cmds.state = "unknown"
	def := e.baseDefinition()
	rule := e.createRule(def)

	e.ingest("ev-unknown-cmd-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	if runs[0].Status == runtime.StatusSucceeded {
		t.Fatal("an unconfirmed command must not be reported as success")
	}
	steps, err := e.st.ListSteps(e.ctx(), runs[0].ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) == 0 || steps[0].Status != "unknown" {
		t.Fatalf("want an unknown step, got %+v", steps)
	}
	outbox, err := e.st.OutboxForRun(e.ctx(), runs[0].ID)
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if len(outbox) != 1 || outbox[0].State == "accepted" {
		t.Fatalf("the intent must stay unaccepted, got %+v", outbox)
	}
}

// ---------------------------------------------------------------------------
// Alarm lifecycle
// ---------------------------------------------------------------------------

func (e *testEnv) alarmDefinition() domain.Definition {
	def := e.baseDefinition()
	def.Kind = domain.KindAlarm
	def.Steps = []domain.Step{{
		Kind: domain.StepNotify, NotificationPolicyID: "np-alarm", Value: "告警",
	}}
	def.Recovery = &domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
		Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
		Operator: domain.OpLt, Value: "25", Unit: "℃",
	}}}}}
	return def
}

// TestAlarmUnknownDoesNotClear is the defect this rewrite exists to fix: a
// device that stops reporting must not silently recover an active alarm.
func TestAlarmUnknownDoesNotClear(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	rule := e.createRule(def)

	e.ingest("ev-alarm-1", e.dev1, "temperature", 35.0, "")
	ep, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, "")
	if err != nil {
		t.Fatalf("want an active episode: %v", err)
	}

	// The device now reports an untrustworthy value.
	e.advance(time.Second)
	e.ingest("ev-alarm-2", e.dev1, "temperature", 0.0, string(domain.QualityUnknown))

	still, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, "")
	if err != nil {
		t.Fatal("an unknown sample must not clear the active alarm")
	}
	if still.ID != ep.ID {
		t.Fatalf("the episode identity must not change: %s vs %s", still.ID, ep.ID)
	}
	trs, err := e.st.ListTransitions(e.ctx(), ep.ID)
	if err != nil {
		t.Fatalf("transitions: %v", err)
	}
	var sawUnknown bool
	for _, tr := range trs {
		if tr.Kind == runtime.TransitionUnknown {
			sawUnknown = true
		}
		if tr.Kind == runtime.TransitionClear {
			t.Fatal("no clear transition may be written for an unknown sample")
		}
	}
	if !sawUnknown {
		t.Fatalf("want an explicit unknown transition, got %+v", trs)
	}
}

// TestAlarmRecoveryRequiresCondition: the abnormal condition ending is not a
// recovery; only the explicit recovery condition is.
func TestAlarmRecoveryRequiresCondition(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	rule := e.createRule(def)

	e.ingest("ev-rec-1", e.dev1, "temperature", 35.0, "")
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err != nil {
		t.Fatalf("want an active episode: %v", err)
	}

	// 28℃ is inside the hysteresis band: not abnormal enough to re-qualify, not
	// recovered either. The alarm must stay active.
	e.advance(time.Second)
	e.ingest("ev-rec-2", e.dev1, "temperature", 28.0, "")
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err != nil {
		t.Fatal("the hysteresis band must keep the alarm active")
	}

	// 20℃ satisfies the recovery condition.
	e.advance(time.Second)
	e.ingest("ev-rec-3", e.dev1, "temperature", 20.0, "")
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err == nil {
		t.Fatal("the recovery condition must clear the alarm")
	}
}

// TestAlarmDoesNotDuplicateEpisodes: a stream of abnormal samples is one alarm,
// not one per sample.
func TestAlarmDoesNotDuplicateEpisodes(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	rule := e.createRule(def)

	for i := 0; i < 5; i++ {
		e.ingest("ev-dup-ep-"+itoa(i), e.dev1, "temperature", 35.0+i, "")
		e.advance(time.Second)
	}
	eps, err := e.st.ListEpisodes(e.ctx(), e.tenant, rule.ID, "", "active", 10)
	if err != nil {
		t.Fatalf("list episodes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("want exactly 1 active episode, got %d", len(eps))
	}
	trs, err := e.st.ListTransitions(e.ctx(), eps[0].ID)
	if err != nil {
		t.Fatalf("transitions: %v", err)
	}
	qualified := 0
	for _, tr := range trs {
		if tr.Kind == runtime.TransitionQualified {
			qualified++
		}
	}
	if qualified != 1 {
		t.Fatalf("want exactly 1 qualified transition, got %d", qualified)
	}
}

// TestAckDoesNotClearAlarm: acknowledging means "someone knows", not "the device
// is healthy".
func TestAckDoesNotClearAlarm(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	rule := e.createRule(def)

	e.ingest("ev-ack-1", e.dev1, "temperature", 35.0, "")
	ep, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, "")
	if err != nil {
		t.Fatalf("want an active episode: %v", err)
	}
	ok, err := e.rt.Ack(e.ctx(), e.tenant, ep.ID, "user-1")
	if err != nil || !ok {
		t.Fatalf("ack failed: ok=%v err=%v", ok, err)
	}
	after, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, "")
	if err != nil {
		t.Fatal("acknowledging must not clear the episode")
	}
	if after.AckedAt == nil || after.AckedBy != "user-1" {
		t.Fatalf("want the ack recorded, got %+v", after)
	}
}

// ---------------------------------------------------------------------------
// Simulation
// ---------------------------------------------------------------------------

// TestSimulateHasNoSideEffects is the proof that a preview cannot act: the trace
// is produced, WouldRun is counted, and nothing leaves the process.
func TestSimulateHasNoSideEffects(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(5 * time.Minute)
	rule := e.createRule(def)

	samples := []runtime.SimSample{
		{At: "0s", DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: "35", Unit: "℃"},
		{At: "60s", DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: "35", Unit: "℃"},
		{At: "300s", DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: "35", Unit: "℃"},
	}
	res, err := e.rt.Simulate(e.ctx(), e.tenant, rule.ID, samples)
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if res.SideEffects != 0 {
		t.Fatalf("simulation must have zero side effects, got %d", res.SideEffects)
	}
	if res.WouldRun < 1 {
		t.Fatalf("want the simulation to predict a run, got %d", res.WouldRun)
	}
	if got := len(e.runs(rule.ID)); got != 0 {
		t.Fatalf("simulation must not create runs, got %d", got)
	}
	if e.cmds.count() != 0 {
		t.Fatal("simulation must not dispatch commands")
	}
}

// TestSimulateUnknownDoesNotPredictAction: a preview must report the unknown
// window honestly instead of predicting an action.
func TestSimulateUnknownDoesNotPredictAction(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(5 * time.Minute)
	rule := e.createRule(def)

	samples := []runtime.SimSample{
		{At: "0s", DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: "35", Unit: "℃"},
		{At: "60s", DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: "0",
			Unit: "℃", Quality: "unknown"},
	}
	res, err := e.rt.Simulate(e.ctx(), e.tenant, rule.ID, samples)
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if res.WouldRun != 0 {
		t.Fatalf("an unknown window must not predict a run, got %d", res.WouldRun)
	}
	var sawHold bool
	for _, s := range res.Steps {
		if s.Kind == "pending" && (s.Verdict == "unknown" || s.Verdict == "error") {
			sawHold = true
		}
	}
	if !sawHold {
		t.Fatalf("want an explicit unknown hold in the trace, got %+v", res.Steps)
	}
}

// ---------------------------------------------------------------------------
// Target resolution
// ---------------------------------------------------------------------------

// TestAreaTargetIsExplicitlyExcluded: the community directory has no area level,
// so an area target must be reported as unsupported instead of silently
// resolving to an empty set.
func TestAreaTargetIsExplicitlyExcluded(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Targets = domain.TargetSpec{AreaIDs: []string{"area-1"}}
	rule := e.createRule(def)

	e.ingest("ev-area-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	targets, err := e.st.ListTargets(e.ctx(), runs[0].ID)
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	if len(targets) != 1 || targets[0].ReasonCode != "area_not_supported" {
		t.Fatalf("want an explicit area_not_supported row, got %+v", targets)
	}
}

// TestLabelTargetResolvesThroughDirectory: label targets resolve through the
// directory owner, not by scanning automation tables.
func TestLabelTargetResolvesThroughDirectory(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Targets = domain.TargetSpec{Labels: []string{"floor-1"}, LabelMatch: "or"}
	rule := e.createRule(def)

	e.ingest("ev-label-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	targets, err := e.st.ListTargets(e.ctx(), runs[0].ID)
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("want 2 devices by label, got %+v", targets)
	}
	if e.cmds.count() != 2 {
		t.Fatalf("want 2 command intents, got %d", e.cmds.count())
	}
}

// TestMaxTargetsCapsFanOut: the per-run target budget is enforced and the excess
// is recorded, not silently dropped.
func TestMaxTargetsCapsFanOut(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Targets = domain.TargetSpec{Labels: []string{"floor-1"}, LabelMatch: "or", MaxTargets: 1}
	rule := e.createRule(def)

	e.ingest("ev-cap-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	targets, err := e.st.ListTargets(e.ctx(), runs[0].ID)
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	var excluded int
	for _, tg := range targets {
		if tg.ReasonCode == "target_budget_exceeded" {
			excluded++
		}
	}
	if excluded != 1 {
		t.Fatalf("want 1 excluded by the target budget, got %+v", targets)
	}
	if e.cmds.count() != 1 {
		t.Fatalf("want 1 command for the capped fan-out, got %d", e.cmds.count())
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
