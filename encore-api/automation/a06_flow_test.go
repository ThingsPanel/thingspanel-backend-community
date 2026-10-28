package automation

import (
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
)

// The legacy defect this file guards against: a value that stopped being
// refreshed was still compared as if it were current. It made an alarm clear on
// data nobody had checked for hours, and it made a pending window fire on a
// reading that had already gone stale.

// A recovery condition must not be satisfied by a stale reading. The alarm
// stays active until fresh evidence arrives, and the decision is explainable.
func TestStaleRecoveryEvidenceDoesNotClearAlarm(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	// Recovery watches a different signal from the trigger, so the recovery
	// read really can be older than the sample that provoked the evaluation.
	def.Recovery = &domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
		Source: "self", Signal: domain.SignalTelemetry, Key: "ambient",
		Operator: domain.OpLt, Value: "25", Unit: "℃",
	}}}}}
	def.Freshness = domain.Freshness{
		MaxAge: domain.Duration(5 * time.Minute),
		MaxGap: domain.Duration(5 * time.Minute),
	}
	rule := e.createRule(def)

	e.ingest("ev-a06-1", e.dev1, "temperature", 35.0, "")
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err != nil {
		t.Fatalf("want an active episode: %v", err)
	}
	// The ambient reading arrives while the alarm is open.
	e.ingest("ev-a06-2", e.dev1, "ambient", 20.0, "")

	// An hour passes with no new ambient reading, so it is now stale.
	e.advance(time.Hour)

	// A fresh temperature sample says the trigger no longer holds, which is the
	// only moment the recovery condition is evaluated.
	e.ingest("ev-a06-3", e.dev1, "temperature", 22.0, "")

	ep, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, "")
	if err != nil {
		t.Fatalf("stale recovery evidence cleared the alarm: %v", err)
	}

	// The decision is recorded, not swallowed: the transition says the recovery
	// condition could not be determined.
	trs, err := e.st.ListTransitions(e.ctx(), ep.ID)
	if err != nil {
		t.Fatalf("transitions: %v", err)
	}
	var kinds []string
	for _, tr := range trs {
		kinds = append(kinds, tr.Kind)
	}
	var sawUndetermined bool
	for _, tr := range trs {
		if tr.Kind == runtime.TransitionUnknown || tr.Kind == runtime.TransitionNoTransition {
			sawUndetermined = true
		}
	}
	if !sawUndetermined {
		t.Fatalf("no transition explains why the alarm stayed active: %v", kinds)
	}
}

// A pending window that comes due must not fire on evidence that stopped being
// refreshed while it waited. It expires with a recorded reason instead.
func TestPendingExpiresInsteadOfFiringOnStaleEvidence(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(2 * time.Minute)
	def.Freshness = domain.Freshness{
		MaxAge: domain.Duration(3 * time.Minute),
		MaxGap: domain.Duration(3 * time.Minute),
	}
	rule := e.createRule(def)

	e.ingest("ev-a06-p1", e.dev1, "temperature", 35.0, "")
	// The window opens; nothing else arrives for well past max_gap.
	e.advance(10 * time.Minute)
	e.tick()

	if n := len(e.runs(rule.ID)); n != 0 {
		t.Fatalf("a run fired on stale evidence: %d", n)
	}
	if n := e.cmds.count(); n != 0 {
		t.Fatalf("a command fired on stale evidence: %d", n)
	}
	if n := len(e.eventsOfKind("pending.expired")); n != 1 {
		t.Fatalf("want 1 pending.expired record, got %d", n)
	}
	if p, err := e.st.GetPending(e.ctx(), runtime.ScopeKey(&rule.Definition, rule.ID, e.dev1)); err == nil && p != nil {
		t.Fatalf("expired pending must be cleared, still present: deadline=%s", p.Deadline)
	}
}

// Two devices with the same field name must be judged independently. This is
// the case that used to read whichever device the code happened to have in hand.
func TestTwoDevicesSameFieldDifferentValues(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	// The rule fires on dev1 only when dev2 is also hot.
	def.Trigger.Groups[0].Conditions = []domain.Condition{
		{Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "30", Unit: "℃"},
		{Source: "device", DeviceID: e.dev2, Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "30", Unit: "℃"},
	}
	rule := e.createRule(def)

	// dev1 is hot, dev2 is cold: no run.
	e.ingest("ev-a06-d1", e.dev1, "temperature", 40.0, "")
	e.ingest("ev-a06-d2", e.dev2, "temperature", 10.0, "")
	if n := len(e.runs(rule.ID)); n != 0 {
		t.Fatalf("cross-device condition fired on the wrong device: %d run(s)", n)
	}

	// dev2 becomes hot: now the pair is satisfied.
	e.ingest("ev-a06-d3", e.dev2, "temperature", 41.0, "")
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("want 1 run once both devices are hot, got %d", n)
	}
}
