package automation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A09: finite runs, step records and stable invocation identity.
//
// The acceptance criteria of this task are all about what survives a process
// death:
//
//   - a resumed run continues from its checkpoint, so no finished step is
//     replayed and no step is left frozen in "pending";
//   - retrying the same action is not a second acceptance, while a genuinely new
//     execution is still accepted;
//   - every step kind the validator accepts has a real implementation.
//
// The tests build the exact state a dead worker leaves behind — unfinished, lease
// expired, program counter at the step that was in flight — instead of pretending
// to kill a goroutine, because that state *is* the crash contract.
// ---------------------------------------------------------------------------

// crashAt rewinds a run to what a worker that died mid-program leaves: still
// running, holding an expired lease, positioned at the given program counter.
func (e *testEnv) crashAt(runID string, pc int) {
	e.t.Helper()
	if _, err := e.st.DB.ExecContext(e.ctx(), `
        UPDATE auto_runs
           SET status='running', finished_at=NULL, reason_code=NULL, reason=NULL,
               cursor_pc=$2, lease_owner='dead-worker', lease_until=$3
         WHERE id=$1`, runID, pc, e.now.Add(-time.Minute)); err != nil {
		e.t.Fatalf("simulate crash: %v", err)
	}
}

func (e *testEnv) steps(runID string) []*store.Step {
	e.t.Helper()
	out, err := e.st.ListSteps(e.ctx(), runID)
	if err != nil {
		e.t.Fatalf("list steps: %v", err)
	}
	return out
}

func (e *testEnv) stepStatuses(runID string) []string {
	e.t.Helper()
	out := []string{}
	for _, s := range e.steps(runID) {
		out = append(out, fmt.Sprintf("%d:%s:%s", s.Idx, s.Kind, s.Status))
	}
	return out
}

func (e *testEnv) run(runID string) *store.Run {
	e.t.Helper()
	r, err := e.st.GetRun(e.ctx(), e.tenant, runID)
	if err != nil {
		e.t.Fatalf("get run: %v", err)
	}
	return r
}

func (e *testEnv) outboxFor(runID string) []*store.OutboxItem {
	e.t.Helper()
	rows, err := e.st.DB.QueryContext(e.ctx(),
		`SELECT id FROM auto_outbox WHERE run_id=$1 ORDER BY created_at, id`, runID)
	if err != nil {
		e.t.Fatalf("list outbox: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			e.t.Fatalf("scan outbox: %v", err)
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	var out []*store.OutboxItem
	for _, id := range ids {
		item, err := e.st.GetOutbox(e.ctx(), id)
		if err != nil {
			e.t.Fatalf("get outbox: %v", err)
		}
		out = append(out, item)
	}
	return out
}

// resumable reports whether the sweeper would pick this run up right now. It is
// scoped to one run so the assertion does not depend on other tests' leftovers.
func (e *testEnv) resumable(runID string) bool {
	e.t.Helper()
	ids, err := e.st.ResumableRuns(e.ctx(), e.now, 200)
	if err != nil {
		e.t.Fatalf("resumable runs: %v", err)
	}
	for _, id := range ids {
		if id == runID {
			return true
		}
	}
	return false
}

// twoActionDefinition is a flat program with two device actions, so a resume has
// a visible "already done" prefix and a visible "still to do" suffix.
func (e *testEnv) twoActionDefinition() domain.Definition {
	def := e.baseDefinition()
	def.Steps = []domain.Step{
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "0"},
	}
	return def
}

// ---------------------------------------------------------------------------
// Crash recovery
// ---------------------------------------------------------------------------

// TestResumeAfterCrashContinuesFromTheCheckpoint is the core A09 guarantee: a run
// interrupted after step 0 finishes without replaying step 0, and its second step
// is not left in "pending" forever.
func TestResumeAfterCrashContinuesFromTheCheckpoint(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.twoActionDefinition())

	e.ingest("ev-a09-crash-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	runID := runs[0].ID
	if e.cmds.count() != 2 {
		t.Fatalf("want 2 commands from the clean run, got %d", e.cmds.count())
	}

	// The worker died right after step 0 was dispatched, before step 1 was
	// recorded and before step 1's intent was written. Both are gone, so the
	// resume has to produce them — exactly once.
	e.crashAt(runID, 1)
	if _, err := e.st.DB.ExecContext(e.ctx(),
		`DELETE FROM auto_steps WHERE run_id=$1 AND idx=1`, runID); err != nil {
		t.Fatalf("rewind steps: %v", err)
	}
	if _, err := e.st.DB.ExecContext(e.ctx(),
		`DELETE FROM auto_outbox WHERE run_id=$1 AND idempotency_key LIKE '%:1:cmd:%'`, runID); err != nil {
		t.Fatalf("rewind intents: %v", err)
	}
	if !e.resumable(runID) {
		t.Fatal("an interrupted run with an expired lease must be resumable")
	}

	res := e.tick()
	if res.RunsResumed < 1 {
		t.Fatalf("want at least one resumed run, got %d", res.RunsResumed)
	}

	got := e.run(runID)
	if got.Status != runtime.StatusSucceeded {
		t.Fatalf("resumed run must finish, got status %q reason %q", got.Status, got.ReasonCode)
	}
	if got.ResumeCount != 1 {
		t.Fatalf("want resume_count=1, got %d", got.ResumeCount)
	}
	// Exactly one more command: step 1. Step 0 was not replayed.
	if e.cmds.count() != 3 {
		t.Fatalf("want 3 commands in total (2 + resumed step 1), got %d", e.cmds.count())
	}
	statuses := e.stepStatuses(runID)
	want := []string{"0:action:accepted", "1:action:accepted"}
	if len(statuses) != len(want) {
		t.Fatalf("want %v, got %v", want, statuses)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("want %v, got %v", want, statuses)
		}
	}
}

// TestResumedStepRowKeepsItsIdentity guards the "stuck step" failure mode: the
// step row id must come back from the database on a retry, otherwise every later
// status write updates zero rows and the step stays "pending" forever.
func TestResumedStepRowKeepsItsIdentity(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())

	e.ingest("ev-a09-ident-1", e.dev1, "temperature", 35.0, "")
	runID := e.runs(rule.ID)[0].ID
	before := e.steps(runID)
	if len(before) != 1 {
		t.Fatalf("want 1 step, got %d", len(before))
	}

	e.crashAt(runID, 0)
	if res := e.tick(); res.RunsResumed < 1 {
		t.Fatalf("want at least one resume, got %d", res.RunsResumed)
	}

	after := e.steps(runID)
	if len(after) != 1 {
		t.Fatalf("a retry must not create a second step row, got %d rows", len(after))
	}
	if after[0].ID != before[0].ID {
		t.Fatalf("step id changed across the retry: %s -> %s", before[0].ID, after[0].ID)
	}
	if after[0].Attempt != 1 {
		t.Fatalf("want attempt=1 after one retry, got %d", after[0].Attempt)
	}
	if after[0].Status != "accepted" {
		t.Fatalf("step must reach a terminal status, got %q", after[0].Status)
	}
}

// TestReplayOfAnAcceptedActionIsNotASecondAction is the "no duplicate acceptance"
// half of the contract. The step is re-entered, the intent already exists, so the
// recorded outcome is reused and the port is not called again.
func TestReplayOfAnAcceptedActionIsNotASecondAction(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())

	e.ingest("ev-a09-replay-1", e.dev1, "temperature", 35.0, "")
	runID := e.runs(rule.ID)[0].ID
	if e.cmds.count() != 1 {
		t.Fatalf("want 1 command, got %d", e.cmds.count())
	}
	items := e.outboxFor(runID)
	if len(items) != 1 || items[0].State != "accepted" {
		t.Fatalf("want 1 accepted intent, got %+v", items)
	}
	firstKey := items[0].IdempotencyKey

	// Force the exact replay window: the step was dispatched, then the process
	// died before the run could be closed.
	e.crashAt(runID, 0)
	if res := e.tick(); res.RunsResumed < 1 {
		t.Fatalf("want at least one resume, got %d", res.RunsResumed)
	}

	if e.cmds.count() != 1 {
		t.Fatalf("a replayed step must not call the port again, got %d calls", e.cmds.count())
	}
	again := e.outboxFor(runID)
	if len(again) != 1 {
		t.Fatalf("a replay must not add an intent, got %d rows", len(again))
	}
	if again[0].IdempotencyKey != firstKey {
		t.Fatalf("intent identity changed: %s -> %s", firstKey, again[0].IdempotencyKey)
	}
	if got := e.run(runID); got.Status != runtime.StatusSucceeded {
		t.Fatalf("replayed run must still finish, got %q", got.Status)
	}
}

// TestALegitimateSecondRunIsNotDeduplicated is the other half: a second genuine
// execution is a new occurrence and must be accepted, not folded into the first.
func TestALegitimateSecondRunIsNotDeduplicated(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())

	e.ingest("ev-a09-two-1", e.dev1, "temperature", 35.0, "")
	e.advance(2 * time.Second)
	e.ingest("ev-a09-two-2", e.dev1, "temperature", 36.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	if e.cmds.count() != 2 {
		t.Fatalf("each run must dispatch its own action, got %d calls", e.cmds.count())
	}
	keys := map[string]bool{}
	for _, r := range runs {
		items := e.outboxFor(r.ID)
		if len(items) != 1 {
			t.Fatalf("run %s must own exactly 1 intent, got %d", r.ID, len(items))
		}
		if keys[items[0].IdempotencyKey] {
			t.Fatalf("two runs share one action identity: %s", items[0].IdempotencyKey)
		}
		keys[items[0].IdempotencyKey] = true
	}
}

// ---------------------------------------------------------------------------
// Wait steps
// ---------------------------------------------------------------------------

// TestWaitResumesAfterTheWaitWithoutReplayingEarlierSteps proves the program
// counter, not a goroutine, carries the position: the step before the wait runs
// once, and the step after it runs only when the timer fires.
func TestWaitResumesAfterTheWaitWithoutReplayingEarlierSteps(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Steps = []domain.Step{
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
		{Kind: domain.StepWait, Wait: domain.Duration(time.Minute)},
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "0"},
	}
	rule := e.createRule(def)

	e.ingest("ev-a09-wait-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	runID := runs[0].ID
	if e.cmds.count() != 1 {
		t.Fatalf("only the step before the wait may run, got %d commands", e.cmds.count())
	}
	if got := e.run(runID); got.Status != runtime.StatusRunning {
		t.Fatalf("a waiting run stays running, got %q", got.Status)
	}
	// A resume scan must not jump over a scheduled wait.
	if e.resumable(runID) {
		t.Fatal("a run with a pending wait must not be resumed")
	}

	e.advance(time.Minute)
	res := e.tick()
	if res.RunTimers != 1 {
		t.Fatalf("want 1 due wait, got %d", res.RunTimers)
	}
	if e.cmds.count() != 2 {
		t.Fatalf("the step after the wait must run once, got %d commands", e.cmds.count())
	}
	got := e.run(runID)
	if got.Status != runtime.StatusSucceeded {
		t.Fatalf("run must finish after the wait, got %q", got.Status)
	}
	statuses := e.stepStatuses(runID)
	want := []string{"0:action:accepted", "1:wait:waiting", "2:action:accepted"}
	if len(statuses) != len(want) {
		t.Fatalf("want %v, got %v", want, statuses)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("want %v, got %v", want, statuses)
		}
	}
}

// ---------------------------------------------------------------------------
// Budget and lease
// ---------------------------------------------------------------------------

// TestOverdueRunIsExpiredInsteadOfStuck: a run whose budget is spent must become
// a finished fact, not a row that says "running" forever.
func TestOverdueRunIsExpiredInsteadOfStuck(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(time.Hour)}}
	rule := e.createRule(def)

	e.ingest("ev-a09-expire-1", e.dev1, "temperature", 35.0, "")
	runID := e.runs(rule.ID)[0].ID

	// Spend the budget: the deadline is now in the past.
	e.advance(2 * time.Hour)
	res := e.tick()
	if res.RunsExpired < 1 {
		t.Fatalf("want at least one expired run, got %d", res.RunsExpired)
	}
	got := e.run(runID)
	if got.Status != runtime.StatusExpired {
		t.Fatalf("want status expired, got %q", got.Status)
	}
	if got.ReasonCode != "run_budget_exceeded" {
		t.Fatalf("want a readable reason, got %q", got.ReasonCode)
	}
	if got.FinishedAt == nil {
		t.Fatal("an expired run must carry finished_at")
	}
	// An expired run must not be resumed afterwards, even though its wait timer
	// was due at the same time.
	if e.resumable(runID) {
		t.Fatal("an expired run must not be resumable")
	}
	if res := e.tick(); res.RunsResumed != 0 {
		t.Fatalf("nothing may be resumed after expiry, got %d", res.RunsResumed)
	}
	if e.cmds.count() != 0 {
		t.Fatalf("an expired run must not dispatch anything, got %d", e.cmds.count())
	}
}

// TestTwoWorkersCannotWalkTheSameRun: the lease is the only thing standing
// between two replicas and a doubled side effect.
func TestTwoWorkersCannotWalkTheSameRun(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())

	e.ingest("ev-a09-lease-1", e.dev1, "temperature", 35.0, "")
	runID := e.runs(rule.ID)[0].ID

	// A second runtime standing in for a second replica.
	other := runtime.New(e.st, e.cmds, e.notif, e.dir, func() time.Time { return e.now })

	e.crashAt(runID, 0)
	now := e.now
	first, err := e.st.ClaimRun(e.ctx(), runID, "worker-a", now, now.Add(store.RunLeaseDuration))
	if err != nil || !first {
		t.Fatalf("first claim must succeed: ok=%v err=%v", first, err)
	}
	second, err := e.st.ClaimRun(e.ctx(), runID, "worker-b", now, now.Add(store.RunLeaseDuration))
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if second {
		t.Fatal("a live lease must block a second worker")
	}

	// The second runtime must refuse to walk the program.
	claimed, err := other.ResumeRun(e.ctx(), runID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if claimed {
		t.Fatal("a worker without the lease must not execute the run")
	}
	if e.cmds.count() != 1 {
		t.Fatalf("no extra command may be sent while another worker holds the run, got %d", e.cmds.count())
	}

	// Once the lease expires the run is resumable again.
	e.advance(store.RunLeaseDuration + time.Second)
	if res := e.tick(); res.RunsResumed < 1 {
		t.Fatalf("an expired lease must allow a resume, got %d", res.RunsResumed)
	}
	if got := e.run(runID); got.Status != runtime.StatusSucceeded {
		t.Fatalf("run must finish once the lease is free, got %q", got.Status)
	}
	if e.cmds.count() != 1 {
		t.Fatalf("the replay must reuse the accepted intent, got %d calls", e.cmds.count())
	}
}

// ---------------------------------------------------------------------------
// Declared steps vs implemented steps
// ---------------------------------------------------------------------------

// TestUnsupportedStepIsRejectedAtPublish: an unimplemented step must never reach
// a run, and the message must tell the author what is available.
func TestUnsupportedStepIsRejectedAtPublish(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Steps = []domain.Step{{Kind: domain.StepKind("loop_forever"), Value: "x"}}

	err := domain.Validate(&def)
	if err == nil {
		t.Fatal("an unsupported step kind must be rejected at publish time")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("want a validation error, got %T", err)
	}
	var found *domain.Issue
	for i := range ve.Issues {
		if ve.Issues[i].Code == "unsupported_step" {
			found = &ve.Issues[i]
		}
	}
	if found == nil {
		t.Fatalf("want an unsupported_step issue, got %+v", ve.Issues)
	}
	if found.Field != "steps[0].kind" {
		t.Fatalf("the issue must point at the offending field, got %q", found.Field)
	}
	// The message must name the executable kinds, otherwise the author has to
	// guess. Every declared kind has to appear in it.
	for kind := range domain.StepImplementations {
		if !strings.Contains(found.Message, string(kind)) {
			t.Fatalf("message %q must mention the supported kind %q", found.Message, kind)
		}
	}
}

// TestEveryDeclaredStepKindHasARealImplementation is the "no declared step is a
// stub" proof: for each kind the validator accepts, a published rule must reach a
// real outcome instead of failing with unsupported_step.
func TestEveryDeclaredStepKindHasARealImplementation(t *testing.T) {
	e := newEnv(t)

	// A real alarm policy for the alarm step to reference.
	alarmDef := e.baseDefinition()
	alarmDef.Kind = domain.KindAlarm
	alarmDef.Steps = []domain.Step{{Kind: domain.StepNotify,
		NotificationPolicyID: "np-alarm", Value: "告警通知"}}
	alarmPolicy := e.createRule(alarmDef)

	cases := map[domain.StepKind]func() []domain.Step{
		domain.StepAction: func() []domain.Step {
			return []domain.Step{{Kind: domain.StepAction, Action: "telemetry",
				Identifier: "switch", Value: "1"}}
		},
		domain.StepWait: func() []domain.Step {
			return []domain.Step{
				{Kind: domain.StepWait, Wait: domain.Duration(time.Second)},
				{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
			}
		},
		domain.StepBranch: func() []domain.Step {
			return []domain.Step{{
				Kind: domain.StepBranch,
				Condition: &domain.Group{Conditions: []domain.Condition{{
					Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
					Operator: domain.OpGt, Value: "0",
				}}},
				Then: []domain.Step{{Kind: domain.StepAction, Action: "telemetry",
					Identifier: "switch", Value: "1"}},
			}}
		},
		domain.StepAlarm: func() []domain.Step {
			return []domain.Step{{Kind: domain.StepAlarm, AlarmPolicyID: alarmPolicy.ID}}
		},
		domain.StepNotify: func() []domain.Step {
			return []domain.Step{{Kind: domain.StepNotify,
				NotificationPolicyID: "np-1", Value: "温度过高"}}
		},
		domain.StepActivateScene: func() []domain.Step {
			return []domain.Step{{Kind: domain.StepActivateScene, Value: "scene-1"}}
		},
	}

	for kind := range domain.StepImplementations {
		build, ok := cases[kind]
		if !ok {
			t.Fatalf("step kind %q is declared as implemented but this test has no fixture for it;"+
				" add one or remove the kind from domain.StepImplementations", kind)
		}
		t.Run(string(kind), func(t *testing.T) {
			def := e.baseDefinition()
			def.Steps = build()
			def.Normalize()
			if err := domain.Validate(&def); err != nil {
				t.Fatalf("fixture for %q must be publishable: %v", kind, err)
			}
			rule := &store.Rule{
				ID: runtime.NewID(), TenantID: e.tenant, Name: "步骤实现 " + string(kind),
				Kind: def.Kind, Enabled: true, Definition: def,
				DefinitionHash: domain.DefinitionHash(def),
			}
			if err := e.st.CreateRule(e.ctx(), rule); err != nil {
				t.Fatalf("create rule: %v", err)
			}
			if _, err := e.rt.Ingest(e.ctx(), store.IngestEvent{
				EventID: "ev-a09-kind-" + string(kind), TenantID: e.tenant,
				DeviceID: e.dev1, Signal: domain.SignalTelemetry, Key: "temperature",
				Value: 35.0, Unit: "℃", Quality: domain.QualityGood, ObservedAt: e.now,
			}); err != nil {
				t.Fatalf("ingest: %v", err)
			}
			runs := e.runs(rule.ID)
			if len(runs) != 1 {
				t.Fatalf("want 1 run for %q, got %d", kind, len(runs))
			}
			got := runs[0]
			if got.ReasonCode == "unsupported_step" || got.ReasonCode == "step_rejected" {
				t.Fatalf("step kind %q has no real implementation: reason %q (%s)",
					kind, got.ReasonCode, got.Reason)
			}
			var found bool
			for _, s := range e.steps(got.ID) {
				if s.Kind == string(kind) {
					found = true
					if s.Status == "pending" {
						t.Fatalf("step %q was left pending", kind)
					}
				}
			}
			if !found {
				t.Fatalf("no step row recorded for kind %q", kind)
			}
			// Let a wait run out so the next subtest is not blocked by an
			// unfinished run in the same scope.
			e.advance(2 * time.Second)
			e.tick()
		})
	}
}

// TestRunStatusIsNeverLeftUnfinishedAfterAnError: an error inside a step must
// close the run with a reason, not abandon it in "running".
func TestRunStatusIsNeverLeftUnfinishedAfterAnError(t *testing.T) {
	e := newEnv(t)
	// The alarm policy id does not exist, so the alarm step fails cleanly.
	def := e.baseDefinition()
	def.Steps = []domain.Step{{Kind: domain.StepAlarm, AlarmPolicyID: "does-not-exist"}}
	rule := e.createRule(def)

	e.ingest("ev-a09-error-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	got := runs[0]
	if got.Status != runtime.StatusFailed {
		t.Fatalf("want failed, got %q", got.Status)
	}
	if got.FinishedAt == nil {
		t.Fatal("a failed run must carry finished_at")
	}
	if got.ReasonCode == "" || got.Reason == "" {
		t.Fatalf("a failed run must explain itself, got code=%q reason=%q", got.ReasonCode, got.Reason)
	}
	for _, s := range e.steps(got.ID) {
		if s.Status == "pending" {
			t.Fatalf("step %d left pending after a failure", s.Idx)
		}
	}
}
