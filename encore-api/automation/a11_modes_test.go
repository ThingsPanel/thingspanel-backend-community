package automation

import (
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
)

// ---------------------------------------------------------------------------
// A11: the four run modes, and what happens to the trigger that loses.
//
// Each mode answers one question: "this rule is still running and it triggered
// again — what now?" The answer must be explicit and visible, because the
// alternatives are all silent: dropping the trigger, running it twice, or
// queueing it forever.
//
// The two properties worth testing beyond the happy path are:
//
//   - a trigger that waited too long must not fire late (the corridor light);
//   - a run cancelled by a newer generation must not pretend an already
//     accepted action was withdrawn.
// ---------------------------------------------------------------------------

// withMode returns the base definition set to a mode, with its bounds explicit
// so the test says what it is testing.
func (e *testEnv) withMode(mode domain.RunMode, tune func(*domain.Definition)) domain.Definition {
	def := e.baseDefinition()
	def.Mode = mode
	if tune != nil {
		tune(&def)
	}
	return def
}

// single: the second trigger is recorded as ignored, not silently dropped.
func TestSingleModeRecordsTheIgnoredTrigger(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeSingle, func(d *domain.Definition) {
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(time.Minute)}}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-single-1", e.dev1, "temperature", 35.0, "")
	e.ingest("ev-a11-single-2", e.dev1, "temperature", 36.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 2 {
		t.Fatalf("both triggers must leave a record, got %d runs", len(runs))
	}
	statuses := map[string]string{}
	for _, r := range runs {
		statuses[r.TriggerEventID] = r.Status + "/" + r.ReasonCode
	}
	if got := statuses["ev-a11-single-1"]; got != runtime.StatusRunning+"/" {
		t.Fatalf("the first trigger must be running, got %q", got)
	}
	if got := statuses["ev-a11-single-2"]; got != runtime.StatusIgnored+"/mode_single_busy" {
		t.Fatalf("the second trigger must be recorded as ignored, got %q", got)
	}
}

// restart: the old program is cancelled and the new one starts from its own
// first step, with a higher generation.
func TestRestartModeReplacesTheRunningProgram(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeRestart, func(d *domain.Definition) {
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(9 * time.Minute)}}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-restart-1", e.dev1, "temperature", 35.0, "")
	first := e.runs(rule.ID)[0]

	e.advance(2 * time.Minute)
	e.ingest("ev-a11-restart-2", e.dev1, "temperature", 36.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	old := e.run(first.ID)
	if old.Status != runtime.StatusCancelled {
		t.Fatalf("restart must cancel the old run, got %q", old.Status)
	}
	if old.ReasonCode != "mode_restart" {
		t.Fatalf("the cancellation must say why, got %q", old.ReasonCode)
	}
	for _, r := range runs {
		if r.ID == first.ID {
			continue
		}
		if r.Generation <= old.Generation {
			t.Fatalf("the replacing run must have a higher generation, got %d <= %d",
				r.Generation, old.Generation)
		}
	}
}

// queued: the triggers run one after another, in the order they arrived.
func TestQueuedModeRunsInArrivalOrder(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeQueued, func(d *domain.Definition) {
		d.Queue = &domain.QueueRule{TTL: domain.Duration(30 * time.Minute), MaxDepth: 5}
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(time.Minute)}}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-queue-1", e.dev1, "temperature", 35.0, "")
	e.ingest("ev-a11-queue-2", e.dev1, "temperature", 36.0, "")
	e.ingest("ev-a11-queue-3", e.dev1, "temperature", 37.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 3 {
		t.Fatalf("want 3 runs, got %d", len(runs))
	}
	triggerOf := map[string]string{}
	for _, r := range runs {
		triggerOf[r.ID] = r.TriggerEventID
	}
	var queued int
	for _, r := range runs {
		if r.Status == runtime.StatusQueued {
			queued++
		}
	}
	if queued != 2 {
		t.Fatalf("the two later triggers must be queued, got %d", queued)
	}
	// The queue order is the store's, not the listing's: ask the same query the
	// worker asks, otherwise this only tests the listing's sort.
	scope := runtime.ScopeKey(&rule.Definition, rule.ID, e.dev1)
	next, err := e.st.NextQueuedRun(e.ctx(), scope)
	if err != nil {
		t.Fatalf("next queued: %v", err)
	}
	if triggerOf[next] != "ev-a11-queue-2" {
		t.Fatalf("the queue must keep arrival order, next was %q", triggerOf[next])
	}

	// The waiting run finishes; the queue drains in order.
	e.advance(2 * time.Minute)
	e.tick()
	e.advance(2 * time.Minute)
	e.tick()

	states := map[string]string{}
	for _, r := range e.runs(rule.ID) {
		states[r.TriggerEventID] = r.Status
	}
	for _, trigger := range []string{"ev-a11-queue-1", "ev-a11-queue-2", "ev-a11-queue-3"} {
		if states[trigger] != runtime.StatusSucceeded {
			t.Fatalf("%s must have run, got %q", trigger, states[trigger])
		}
	}
}

// The corridor light: a trigger that waited longer than the rule allows must not
// fire when its turn finally comes.
func TestAQueuedTriggerThatWaitedTooLongDoesNotFireLate(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeQueued, func(d *domain.Definition) {
		d.Queue = &domain.QueueRule{TTL: domain.Duration(2 * time.Minute), MaxDepth: 5}
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(5 * time.Minute)}}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-ttl-1", e.dev1, "temperature", 35.0, "")
	e.ingest("ev-a11-ttl-2", e.dev1, "temperature", 36.0, "")

	// Ten minutes pass before the queue can move.
	e.advance(10 * time.Minute)
	e.tick()

	for _, r := range e.runs(rule.ID) {
		if r.TriggerEventID != "ev-a11-ttl-2" {
			continue
		}
		if r.Status == runtime.StatusSucceeded || r.Status == runtime.StatusRunning {
			t.Fatalf("a trigger that waited 10 minutes must not run, got %q", r.Status)
		}
		if r.ReasonCode != "queue_ttl_exceeded" {
			t.Fatalf("the reason must say the queue expired, got %q", r.ReasonCode)
		}
	}
	if e.cmds.count() != 0 {
		t.Fatalf("no device action may be sent for an expired queue entry, got %d", e.cmds.count())
	}
}

// A rule disabled while its trigger waited must not act afterwards.
func TestAQueuedTriggerForADisabledRuleDoesNotRun(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeQueued, func(d *domain.Definition) {
		d.Queue = &domain.QueueRule{TTL: domain.Duration(30 * time.Minute), MaxDepth: 5}
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(time.Minute)}}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-disabled-1", e.dev1, "temperature", 35.0, "")
	e.ingest("ev-a11-disabled-2", e.dev1, "temperature", 36.0, "")

	if _, err := e.st.SetEnabled(e.ctx(), e.tenant, rule.ID, false, rule.Version); err != nil {
		t.Fatalf("disable rule: %v", err)
	}
	e.advance(2 * time.Minute)
	e.tick()

	for _, r := range e.runs(rule.ID) {
		if r.TriggerEventID != "ev-a11-disabled-2" {
			continue
		}
		if r.Status != runtime.StatusCancelled {
			t.Fatalf("a queued trigger of a disabled rule must be cancelled, got %q", r.Status)
		}
		if r.ReasonCode != "rule_disabled_before_start" {
			t.Fatalf("the reason must name the disabled rule, got %q", r.ReasonCode)
		}
	}
}

// Overflow is refused visibly instead of growing without bound.
func TestQueueOverflowIsVisible(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeQueued, func(d *domain.Definition) {
		d.Queue = &domain.QueueRule{TTL: domain.Duration(30 * time.Minute), MaxDepth: 2}
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(10 * time.Minute)}}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-over-1", e.dev1, "temperature", 35.0, "")
	e.ingest("ev-a11-over-2", e.dev1, "temperature", 36.0, "")
	e.ingest("ev-a11-over-3", e.dev1, "temperature", 37.0, "")
	e.ingest("ev-a11-over-4", e.dev1, "temperature", 38.0, "")

	var rejected int
	for _, r := range e.runs(rule.ID) {
		if r.Status == runtime.StatusRejected && r.ReasonCode == "queue_overflow" {
			rejected++
		}
	}
	if rejected != 1 {
		t.Fatalf("the 4th trigger must be rejected for overflow, got %d rejections", rejected)
	}
}

// Parallel runs up to the cap and refuses the rest, with a reason.
func TestParallelModeEnforcesItsCap(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeParallel, func(d *domain.Definition) {
		d.Parallel = &domain.ParallelRule{MaxConcurrent: 2, TTL: domain.Duration(10 * time.Minute)}
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(5 * time.Minute)}}
	})
	rule := e.createRule(def)

	for _, id := range []string{"ev-a11-par-1", "ev-a11-par-2", "ev-a11-par-3"} {
		e.ingest(id, e.dev1, "temperature", 35.0, "")
	}

	var running, rejected int
	for _, r := range e.runs(rule.ID) {
		switch {
		case r.Status == runtime.StatusRunning:
			running++
		case r.Status == runtime.StatusRejected && r.ReasonCode == "parallel_limit":
			rejected++
		}
	}
	if running != 2 || rejected != 1 {
		t.Fatalf("want 2 running and 1 rejected, got %d/%d", running, rejected)
	}
}

// A parallel run that holds its slot past the rule's TTL is expired with a
// reason that names the slot, not the generic step budget.
func TestAParallelRunExpiresAtItsTTL(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeParallel, func(d *domain.Definition) {
		d.Parallel = &domain.ParallelRule{MaxConcurrent: 2, TTL: domain.Duration(3 * time.Minute)}
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(time.Hour)}}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-parttl-1", e.dev1, "temperature", 35.0, "")
	e.advance(4 * time.Minute)
	res := e.tick()
	if res.RunsExpired < 1 {
		t.Fatalf("want at least one expired run, got %d", res.RunsExpired)
	}

	runs := e.runs(rule.ID)
	if runs[0].Status != runtime.StatusExpired {
		t.Fatalf("want expired, got %q", runs[0].Status)
	}
	if runs[0].ReasonCode != "parallel_ttl_exceeded" {
		t.Fatalf("the reason must name the parallel TTL, got %q", runs[0].ReasonCode)
	}
}

// Cancelling a run must not rewrite the outcome of an action the device already
// accepted. "We stopped trying" and "it did not happen" are different facts.
func TestCancellingARunDoesNotWithdrawAnAcceptedAction(t *testing.T) {
	e := newEnv(t)
	// A wait after the action keeps the run open, so the cancel lands while the
	// run is still in progress — the only situation where the question "did the
	// cancel undo the action?" can be asked.
	def := e.withMode(domain.ModeSingle, func(d *domain.Definition) {
		d.Steps = []domain.Step{
			{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
			{Kind: domain.StepWait, Wait: domain.Duration(5 * time.Minute)},
		}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-accepted-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	runID := runs[0].ID
	if e.cmds.count() != 1 {
		t.Fatalf("want 1 command, got %d", e.cmds.count())
	}

	if err := e.st.CancelRun(e.ctx(), runID, "operator cancelled", e.now); err != nil {
		t.Fatalf("cancel run: %v", err)
	}
	items := e.outboxFor(runID)
	if len(items) != 1 {
		t.Fatalf("want 1 intent, got %d", len(items))
	}
	if items[0].State != "accepted" {
		t.Fatalf("an already accepted action must stay accepted, got %q", items[0].State)
	}
	if got := e.run(runID); got.Status != runtime.StatusCancelled {
		t.Fatalf("the run itself must be cancelled, got %q", got.Status)
	}
}

// The mode decides who wins; the scope decides who competes. Two devices of the
// same rule must not cancel each other in single mode.
func TestSingleModeScopeIsPerDevice(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeSingle, func(d *domain.Definition) {
		d.Scope = domain.ScopeDevice
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(5 * time.Minute)}}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-scope-1", e.dev1, "temperature", 35.0, "")
	e.ingest("ev-a11-scope-2", e.dev2, "temperature", 35.0, "")

	for _, r := range e.runs(rule.ID) {
		if r.Status != runtime.StatusRunning {
			t.Fatalf("each device must run independently, %s is %q", r.DeviceID, r.Status)
		}
	}
}

// A rule-scoped rule is one competition, so the second device's trigger is the
// one that gets ignored.
func TestRuleScopeMakesDevicesCompete(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeSingle, func(d *domain.Definition) {
		d.Scope = domain.ScopeRule
		d.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(5 * time.Minute)}}
	})
	rule := e.createRule(def)

	e.ingest("ev-a11-rscope-1", e.dev1, "temperature", 35.0, "")
	e.ingest("ev-a11-rscope-2", e.dev2, "temperature", 35.0, "")

	var running, ignored int
	for _, r := range e.runs(rule.ID) {
		switch r.Status {
		case runtime.StatusRunning:
			running++
		case runtime.StatusIgnored:
			ignored++
		}
	}
	if running != 1 || ignored != 1 {
		t.Fatalf("rule scope must allow one run at a time, got %d running / %d ignored", running, ignored)
	}
}
