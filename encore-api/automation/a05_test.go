package automation

import (
	"encoding/json"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ingress"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// deliver runs an envelope through the adapter and then the runtime, exactly the
// way the service endpoint does. Tests go through the adapter on purpose: the
// dedupe guarantee is only worth something if it holds on the real path.
func (e *testEnv) deliver(class ingress.Class, env ingress.Envelope) []*store.Receipt {
	e.t.Helper()
	env.TenantID = e.tenant
	events, err := ingress.Normalize(class, env, e.now)
	if err != nil {
		e.t.Fatalf("normalize %s: %v", class, err)
	}
	out := make([]*store.Receipt, 0, len(events))
	for i := range events {
		rec, err := e.rt.Ingest(e.ctx(), events[i])
		if err != nil {
			e.t.Fatalf("ingest %s: %v", class, err)
		}
		out = append(out, rec)
	}
	return out
}

// eventsOfKind returns the published business transitions of a kind.
func (e *testEnv) eventsOfKind(kind string) []*store.EventRecord {
	e.t.Helper()
	all, err := e.st.EventsSince(e.ctx(), e.tenant, 0, 500)
	if err != nil {
		e.t.Fatalf("events: %v", err)
	}
	var out []*store.EventRecord
	for _, ev := range all {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

// A relay retry of the same uplink must produce one run, not two. This is the
// "repeat delivery does not double-execute" guarantee, proven end to end.
func TestRedeliveredUplinkProducesOneRun(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())

	env := ingress.Envelope{DeviceID: e.dev1, Values: map[string]any{"temperature": 31.5}}
	first := e.deliver(ingress.ClassTelemetry, env)
	second := e.deliver(ingress.ClassTelemetry, env)

	if first[0].Duplicate {
		t.Fatal("first delivery must not be a duplicate")
	}
	if !second[0].Duplicate {
		t.Fatal("redelivery must be recognised as a duplicate")
	}
	if first[0].EventID != second[0].EventID {
		t.Fatalf("retry changed the event id: %s vs %s", first[0].EventID, second[0].EventID)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("want exactly 1 run, got %d", n)
	}
	if n := e.cmds.count(); n != 1 {
		t.Fatalf("want exactly 1 command, got %d", n)
	}
}

// An event received before a crash but not yet evaluated must be evaluated after
// the restart, and only once even if recovery runs twice.
func TestRestartRecoversUnprocessedReceiptsOnce(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())

	// Simulate "received, then the process died": persist the receipt directly
	// so it is durable but still marked unprocessed.
	events, err := ingress.Normalize(ingress.ClassTelemetry, ingress.Envelope{
		TenantID: e.tenant, DeviceID: e.dev1,
		Values: map[string]any{"temperature": 33.0},
	}, e.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.InsertEvent(e.ctx(), &events[0]); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if n := len(e.runs(rule.ID)); n != 0 {
		t.Fatalf("nothing should have run before recovery, got %d", n)
	}

	recovered, err := e.rt.RecoverAfterRestart(e.ctx(), 100)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered < 1 {
		t.Fatal("the unprocessed receipt was not recovered")
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("want 1 run after recovery, got %d", n)
	}

	// A second recovery pass must find nothing left to do.
	if again, err := e.rt.RecoverAfterRestart(e.ctx(), 100); err != nil || again != 0 {
		t.Fatalf("second recovery reprocessed %d events (err=%v)", again, err)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("recovery duplicated the run: %d", n)
	}
}

// A device re-announces online on every reconnect. The repeat must be recognised
// as "no new information" instead of starting another run.
func TestRepeatedOnlineIsRecognised(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.Groups[0].Conditions[0] = domain.Condition{
		Source: "self", Signal: domain.SignalStatus, Key: "online",
		Operator: domain.OpEq, Value: "true",
	}
	rule := e.createRule(def)

	e.deliver(ingress.ClassStatus, ingress.Envelope{
		DeviceID: e.dev1, Values: map[string]any{"online": "1"},
	})
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("first online should start 1 run, got %d", n)
	}

	e.advance(10 * time.Second)
	e.deliver(ingress.ClassStatus, ingress.Envelope{
		DeviceID: e.dev1, Values: map[string]any{"online": "1"},
	})
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("repeated online started another run: %d", n)
	}
	if got := len(e.eventsOfKind("ingress.duplicate_state")); got != 1 {
		t.Fatalf("want 1 duplicate_state record, got %d", got)
	}

	// A genuine transition is still a new event: offline then online again.
	e.advance(2 * time.Minute)
	e.deliver(ingress.ClassStatus, ingress.Envelope{
		DeviceID: e.dev1, Values: map[string]any{"online": "0"},
	})
	e.advance(time.Second)
	e.deliver(ingress.ClassStatus, ingress.Envelope{
		DeviceID: e.dev1, Values: map[string]any{"online": "1"},
	})
	if n := len(e.runs(rule.ID)); n != 2 {
		t.Fatalf("a real reconnect must act again, got %d runs", n)
	}
}

// An alarm transition is an input. The linked rule runs, and the run records
// which transition started it so the chain can be traced.
func TestAlarmTransitionTriggersLinkedRule(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.Groups[0].Conditions[0] = domain.Condition{
		Source: "self", Signal: domain.SignalAlarm, Key: "qualified",
		Operator: domain.OpEq, Value: "active",
	}
	rule := e.createRule(def)

	e.deliver(ingress.ClassAlarm, ingress.Envelope{
		DeviceID: e.dev1,
		Values:   map[string]any{"transition_id": "tr-100", "kind": "qualified", "state": "active"},
	})

	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("alarm transition should start 1 run, got %d", len(runs))
	}
	if runs[0].CausalRoot != "tr-100" {
		t.Fatalf("run lost the chain root: %q", runs[0].CausalRoot)
	}
	if runs[0].CausalHop != 1 {
		t.Fatalf("first link should be hop 1, got %d", runs[0].CausalHop)
	}
}

// Redelivering the same transition id must not start the linked action twice.
func TestAlarmTransitionRedeliveryIsIdempotent(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.Groups[0].Conditions[0] = domain.Condition{
		Source: "self", Signal: domain.SignalAlarm, Key: "qualified",
		Operator: domain.OpEq, Value: "active",
	}
	rule := e.createRule(def)

	env := ingress.Envelope{
		DeviceID: e.dev1,
		Values:   map[string]any{"transition_id": "tr-200", "kind": "qualified", "state": "active"},
	}
	e.deliver(ingress.ClassAlarm, env)
	e.deliver(ingress.ClassAlarm, env)

	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("redelivered transition started %d runs", n)
	}
}

// A chain that reaches the hop limit is stopped, and the reason is queryable.
// Without this, alarm A -> rule R -> alarm A would loop forever.
func TestCausalHopLimitStopsTheLoop(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.Groups[0].Conditions[0] = domain.Condition{
		Source: "self", Signal: domain.SignalAlarm, Key: "qualified",
		Operator: domain.OpEq, Value: "active",
	}
	rule := e.createRule(def)

	// Feed the chain one link past the limit.
	events, err := ingress.Normalize(ingress.ClassAlarm, ingress.Envelope{
		TenantID: e.tenant, DeviceID: e.dev1,
		Values:     map[string]any{"transition_id": "tr-loop", "kind": "qualified", "state": "active"},
		CausalRoot: "tr-loop", CausalHop: runtime.MaxCausalHop,
	}, e.now)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.rt.Ingest(e.ctx(), events[0])
	if err != nil {
		t.Fatal(err)
	}
	if rec.Duplicate {
		t.Fatal("unexpected duplicate")
	}
	if n := len(e.runs(rule.ID)); n != 0 {
		t.Fatalf("a run past the hop limit must not start, got %d", n)
	}

	stopped := e.eventsOfKind("causal.hop_exceeded")
	if len(stopped) != 1 {
		t.Fatalf("want 1 recorded stop reason, got %d", len(stopped))
	}
	var payload map[string]any
	if err := json.Unmarshal(stopped[0].Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["causal_root"] != "tr-loop" {
		t.Fatalf("stop record lost the root: %v", payload["causal_root"])
	}
	if payload["reason"] == "" {
		t.Fatal("stop record must say why")
	}
}

// An attribute is a durable value; an event is instantaneous. They must not be
// stored as the same kind of thing, or an old event would read as a lasting
// condition.
func TestAttributeAndEventStayDistinct(t *testing.T) {
	e := newEnv(t)

	e.deliver(ingress.ClassAttribute, ingress.Envelope{
		DeviceID: e.dev1, Values: map[string]any{"mode": "auto"},
	})
	e.deliver(ingress.ClassEvent, ingress.Envelope{
		DeviceID: e.dev1, MessageID: "ev-1",
		Values: map[string]any{"identify": "door_open", "level": 2},
	})

	attr, ok, err := e.st.LatestState(e.ctx(), e.dev1, domain.SignalAttribute, "mode")
	if err != nil || !ok {
		t.Fatalf("attribute not projected: ok=%v err=%v", ok, err)
	}
	if attr.Value != "auto" {
		t.Fatalf("attribute value lost: %#v", attr.Value)
	}

	ev, ok, err := e.st.LatestState(e.ctx(), e.dev1, domain.SignalEvent, "door_open")
	if err != nil || !ok {
		t.Fatalf("event not projected: ok=%v err=%v", ok, err)
	}
	if ev.Value != true {
		t.Fatalf("event must project as the literal true, got %#v", ev.Value)
	}
	if ev.Params == nil {
		// Params live on the inbox row, not the state projection; the point of
		// the check is that the two signals stayed separate.
		t.Log("event params are carried on the inbox row, not the state row")
	}
}

