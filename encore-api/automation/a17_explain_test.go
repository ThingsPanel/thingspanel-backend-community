package automation

import (
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
)

// ---------------------------------------------------------------------------
// A17: answering "why did it not run?"
//
// The answer must name one of a small number of causes, each of which leads an
// operator somewhere different: no data, condition false, still waiting, data
// unreadable, or it did run. Collapsing any two of them sends the customer to the
// wrong place — "the condition was false" when the sensor never reported is the
// single most expensive wrong answer this system can give.
//
// The last test is the one that matters most: when there is not enough history,
// the answer says so instead of inventing a cause.
// ---------------------------------------------------------------------------

func (e *testEnv) explain(ruleID, deviceID string, from, to time.Time) *runtime.ExplainResult {
	e.t.Helper()
	rule, err := e.st.GetRule(e.ctx(), e.tenant, ruleID)
	if err != nil {
		e.t.Fatalf("load rule: %v", err)
	}
	res, err := e.rt.Explain(e.ctx(), rule, deviceID, from, to)
	if err != nil {
		e.t.Fatalf("explain: %v", err)
	}
	return res
}

func TestExplainNamesTheRunThatFired(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	e.ingest("ev-a17-fired-1", e.dev1, "temperature", 35.0, "")

	res := e.explain(rule.ID, e.dev1, e.now.Add(-time.Hour), e.now.Add(time.Minute))
	if res.Verdict != runtime.ExplainFired {
		t.Fatalf("want fired, got %q (%s)", res.Verdict, res.Evidence.Note)
	}
	if len(res.Runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(res.Runs))
	}
	if len(res.Runs[0].Steps) == 0 {
		t.Fatal("the explanation must carry the steps, not just the header")
	}
	if len(res.Runs[0].CommandRefs) == 0 {
		t.Fatal("the explanation must let the customer jump to the original command")
	}
}

// Nothing ever reported: the honest answer is "no data", not "condition false".
func TestExplainSaysNoDataRatherThanConditionNotMet(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())

	res := e.explain(rule.ID, e.dev1, e.now.Add(-time.Hour), e.now)
	if res.Verdict != runtime.ExplainNoData {
		t.Fatalf("want no_data, got %q (%s)", res.Verdict, res.Evidence.Note)
	}
	if res.Evidence.Complete {
		t.Fatal("an answer without any sample must not claim to be complete")
	}
	if res.Evidence.StateObserved {
		t.Fatal("no sample was observed, so the evidence must say so")
	}
}

// A real reading below the threshold is a genuine "condition not met".
func TestExplainReportsAConditionThatWasNotMet(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	e.ingest("ev-a17-cold-1", e.dev1, "temperature", 10.0, "")

	res := e.explain(rule.ID, e.dev1, e.now.Add(-time.Hour), e.now.Add(time.Minute))
	if res.Verdict != runtime.ExplainConditionNotMet {
		t.Fatalf("want condition_not_met, got %q (%s)", res.Verdict, res.Evidence.Note)
	}
	if len(res.Runs) != 0 {
		t.Fatalf("a cold reading must not produce a run, got %d", len(res.Runs))
	}
	if res.Input == nil || res.Input.Key != "temperature" {
		t.Fatalf("the answer must show the sample it judged, got %+v", res.Input)
	}
	if !res.Evidence.Complete {
		t.Fatal("a judgement based on a real sample is complete")
	}
}

// The condition is met but the "for" window has not elapsed: that is a different
// answer from "condition not met", and it has a remaining time.
func TestExplainReportsTheOpenDurationWindow(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(10 * time.Minute)
	rule := e.createRule(def)

	e.ingest("ev-a17-pending-1", e.dev1, "temperature", 35.0, "")
	e.advance(2 * time.Minute)

	res := e.explain(rule.ID, e.dev1, e.now.Add(-time.Hour), e.now.Add(time.Minute))
	if res.Verdict != runtime.ExplainWaitingForDuration {
		t.Fatalf("want waiting_for_duration, got %q (%s)", res.Verdict, res.Evidence.Note)
	}
	if res.Pending == nil {
		t.Fatal("the answer must carry the open window")
	}
	if res.Pending.RemainingMillis <= 0 {
		t.Fatalf("the window has time left, got %d ms", res.Pending.RemainingMillis)
	}
	if len(res.Runs) != 0 {
		t.Fatalf("nothing has run yet, got %d runs", len(res.Runs))
	}
}

// A reading that is too old to be evidence is "undetermined", never "false".
func TestExplainReportsStaleEvidenceAsUndetermined(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Freshness.MaxAge = domain.Duration(2 * time.Minute)
	def.Freshness.MaxGap = domain.Duration(2 * time.Minute)
	rule := e.createRule(def)

	e.ingest("ev-a17-stale-1", e.dev1, "temperature", 10.0, "")
	e.advance(30 * time.Minute)

	res := e.explain(rule.ID, e.dev1, e.now.Add(-time.Hour), e.now.Add(time.Minute))
	if res.Verdict == runtime.ExplainConditionNotMet {
		t.Fatalf("stale data must not be reported as a false condition: %+v", res.Reasons)
	}
	if res.Verdict != runtime.ExplainUndetermined {
		t.Fatalf("want undetermined, got %q (%s)", res.Verdict, res.Evidence.Note)
	}
	if len(res.Reasons) == 0 {
		t.Fatal("an undetermined answer must say why it could not decide")
	}
}

// The window is respected: a run outside it is not presented as the answer.
func TestExplainOnlyLooksInsideTheWindow(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	e.ingest("ev-a17-window-1", e.dev1, "temperature", 35.0, "")

	// A window that ends before the trigger.
	res := e.explain(rule.ID, e.dev1, e.now.Add(-2*time.Hour), e.now.Add(-90*time.Minute))
	if res.Verdict == runtime.ExplainFired {
		t.Fatal("a run outside the window must not be reported as the cause")
	}
	if res.Evidence.RunsInWindow != 0 {
		t.Fatalf("want 0 runs in window, got %d", res.Evidence.RunsInWindow)
	}
}
