package automation

import (
	"testing"
	"time"

	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A10: reconnecting the existing device control, and carrying its answer all
// the way to the run record.
//
// The command owner can answer in more ways than "yes" and "no". Each of those
// answers has a different consequence for the run, and folding any two of them
// together is how an automation quietly claims to have switched something it did
// not switch:
//
//   - superseded  a newer trigger already won → this run is cancelled, not failed
//   - conflict    the same key was reused for different content → a bug, terminal
//   - in_flight   an identical attempt is still running → ask again shortly
//   - unknown     the attempt died mid-flight → terminal, a human must look
//
// These tests drive the runtime through the same outbox and dispatcher the
// production path uses; only the owner itself is replaced.
// ---------------------------------------------------------------------------

func (e *testEnv) setCommandState(state string) {
	e.t.Helper()
	e.cmds.mu.Lock()
	e.cmds.state = state
	e.cmds.mu.Unlock()
}

// TestASupersededActionCancelsTheRunInsteadOfSucceeding is the core A10
// guarantee: a command refused because a newer generation won must not leave the
// run looking successful.
func TestASupersededActionCancelsTheRunInsteadOfSucceeding(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	e.setCommandState(ports.CommandSuperseded)

	e.ingest("ev-a10-superseded-1", e.dev1, "temperature", 35.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	got := e.run(runs[0].ID)
	if got.Status == runtime.StatusSucceeded {
		t.Fatal("a superseded action must never be recorded as a successful run")
	}
	if got.Status != runtime.StatusCancelled {
		t.Fatalf("want status %q, got %q (reason %q)", runtime.StatusCancelled, got.Status, got.ReasonCode)
	}
	if got.ReasonCode != "superseded" {
		t.Fatalf("the reason must say superseded, got %q", got.ReasonCode)
	}
	if statuses := e.stepStatuses(runs[0].ID); len(statuses) != 1 || statuses[0] != "0:action:cancelled" {
		t.Fatalf("the step must be cancelled, got %v", statuses)
	}
	for _, item := range e.outboxFor(runs[0].ID) {
		if item.State != "cancelled" {
			t.Fatalf("the intent must be cancelled, got %q", item.State)
		}
	}
}

// The rest of the program must not run after a superseded action: every later
// step belongs to the same losing run.
func TestASupersededActionStopsTheRemainingProgram(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.twoActionDefinition())
	e.setCommandState(ports.CommandSuperseded)

	e.ingest("ev-a10-superseded-2", e.dev1, "temperature", 35.0, "")

	if e.cmds.count() != 1 {
		t.Fatalf("only the first action may be attempted, owner saw %d calls", e.cmds.count())
	}
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	if statuses := e.stepStatuses(runs[0].ID); len(statuses) != 1 {
		t.Fatalf("the program must stop at the superseded step, got %v", statuses)
	}
	if got := e.run(runs[0].ID); got.Status != runtime.StatusCancelled {
		t.Fatalf("want cancelled, got %q", got.Status)
	}
}

// A key reuse is a defect in our own identity generation. Retrying it cannot
// help, so it must be terminal and visible.
func TestAConflictingKeyIsFailedAndNotRetried(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	e.setCommandState(ports.CommandConflict)

	e.ingest("ev-a10-conflict-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	if got := e.run(runs[0].ID); got.Status != runtime.StatusFailed {
		t.Fatalf("a conflict must fail the run, got %q", got.Status)
	}
	if statuses := e.stepStatuses(runs[0].ID); statuses[0] != "0:action:failed" {
		t.Fatalf("the step must be failed, got %v", statuses)
	}

	// Nothing is due for a retry, so further ticks must not call the owner again.
	e.advance(5 * time.Minute)
	e.tick()
	e.tick()
	if e.cmds.count() != 1 {
		t.Fatalf("a conflict must not be retried, owner saw %d calls", e.cmds.count())
	}
}

// "The attempt died mid-flight" is not a reason to send again: a non-idempotent
// action may already have happened. It stays visible and stops retrying.
func TestAnUnresolvedActionIsNotRetriedForever(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	e.setCommandState(ports.CommandUnknown)

	e.ingest("ev-a10-unknown-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}

	items := e.outboxFor(runs[0].ID)
	if len(items) != 1 || items[0].State != "unresolved" {
		t.Fatalf("the intent must be parked as unresolved, got %+v", states(items))
	}
	if statuses := e.stepStatuses(runs[0].ID); statuses[0] != "0:action:unknown" {
		t.Fatalf("the step must stay unknown, got %v", statuses)
	}

	e.advance(10 * time.Minute)
	e.tick()
	e.tick()
	if e.cmds.count() != 1 {
		t.Fatalf("an unknown outcome must not be re-sent, owner saw %d calls", e.cmds.count())
	}
}

// "Still processing" is a temporary answer. The intent stays due, and when the
// owner finally accepts, the step that produced it is completed too — otherwise
// the run record would keep saying "unknown" about an action that did reach the
// device.
func TestAnActionStillProcessingIsAskedAgainAndThenConfirmed(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	e.setCommandState(ports.CommandInFlight)

	e.ingest("ev-a10-inflight-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	runID := runs[0].ID

	items := e.outboxFor(runID)
	if len(items) != 1 {
		t.Fatalf("want 1 intent, got %d", len(items))
	}
	if items[0].State != "dispatched" {
		t.Fatalf("an in-flight answer must stay retryable, got %q", items[0].State)
	}
	if statuses := e.stepStatuses(runID); statuses[0] != "0:action:unknown" {
		t.Fatalf("the step must be unconfirmed, got %v", statuses)
	}

	// The retry becomes due. The dispatcher uses the database clock, so the due
	// time is moved rather than waiting five real seconds.
	if _, err := e.st.DB.ExecContext(e.ctx(),
		`UPDATE auto_outbox SET next_attempt_at = now() - interval '1 second' WHERE id=$1`,
		items[0].ID); err != nil {
		t.Fatalf("make the retry due: %v", err)
	}
	e.setCommandState(ports.CommandAccepted)
	e.advance(time.Second)
	e.tick()

	after := e.outboxFor(runID)
	if len(after) != 1 || after[0].State != "accepted" {
		t.Fatalf("the retry must be accepted, got %+v", states(after))
	}
	if statuses := e.stepStatuses(runID); statuses[0] != "0:action:accepted" {
		t.Fatalf("the step must be completed by the late confirmation, got %v", statuses)
	}
}

// The intent must carry the identity the owner needs to dedupe it, otherwise the
// acceptance side has nothing to compare a retry against.
func TestTheIntentCarriesItsScopeGenerationAndKey(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	e.ingest("ev-a10-identity-1", e.dev1, "temperature", 35.0, "")

	req, ok := e.cmds.last()
	if !ok {
		t.Fatal("the owner must have been called")
	}
	if req.IdempotencyKey == "" {
		t.Fatal("a command without an idempotency key cannot be deduplicated")
	}
	if req.ScopeKey == "" || req.Generation == 0 {
		t.Fatalf("the command must carry its scope and generation, got %q/%d", req.ScopeKey, req.Generation)
	}
	// The key must be run-scoped, so the same step in a later run is a different
	// action rather than a replay of this one.
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	if want := runs[0].ID + ":0:cmd:" + e.dev1; req.IdempotencyKey != want {
		t.Fatalf("want key %q, got %q", want, req.IdempotencyKey)
	}
	if runs[0].Generation != req.Generation {
		t.Fatalf("the generation sent must be the run's, got %d vs %d", req.Generation, runs[0].Generation)
	}
}

// A second run of the same rule is a different action, not a replay of the first.
func TestASecondRunGetsADifferentKey(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	e.ingest("ev-a10-second-1", e.dev1, "temperature", 35.0, "")
	e.ingest("ev-a10-second-2", e.dev1, "temperature", 36.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	items := append(e.outboxFor(runs[0].ID), e.outboxFor(runs[1].ID)...)
	if len(items) != 2 {
		t.Fatalf("want 2 intents, got %d", len(items))
	}
	if items[0].IdempotencyKey == items[1].IdempotencyKey {
		t.Fatal("two runs of the same rule must not share an idempotency key")
	}
}

func states(items []*store.OutboxItem) []string {
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, i.State)
	}
	return out
}
