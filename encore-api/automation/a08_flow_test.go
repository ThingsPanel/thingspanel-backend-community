package automation

import (
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// A08: duration, recovery hysteresis and no-data handling.
//
// The three failures these tests pin down are the ones customers actually
// report: "it alarmed at 4 minutes 59", "it cleared although the temperature
// never came back down", and "the alarm stayed on for two days after the device
// died".

// ingestSignal delivers one sample of an arbitrary signal at the env's clock.
func (e *testEnv) ingestSignal(eventID, device string, signal domain.SignalKind, key string, value any) {
	e.t.Helper()
	if _, err := e.rt.Ingest(e.ctx(), store.IngestEvent{
		EventID: eventID, TenantID: e.tenant, DeviceID: device, Signal: signal,
		Key: key, Value: value, Quality: domain.QualityGood, ObservedAt: e.now,
	}); err != nil {
		e.t.Fatalf("ingest %s/%s: %v", signal, key, err)
	}
}

// lastTransitionOf reads the newest lifecycle fact of an episode.
func (e *testEnv) lastTransitionOf(episodeID string) *store.Transition {
	e.t.Helper()
	tr, err := e.st.LastTransition(e.ctx(), episodeID)
	if err != nil {
		e.t.Fatalf("last transition: %v", err)
	}
	return tr
}

// restart rebuilds the runtime over the same store and clock, which is what a
// process restart looks like to the durable state.
func (e *testEnv) restart() {
	e.t.Helper()
	e.rt = runtime.New(e.st, e.cmds, e.notif, e.dir, func() time.Time { return e.now })
}

// ---------------------------------------------------------------------------
// Hysteresis
// ---------------------------------------------------------------------------

// The acceptance fixture: trigger above 30, recover below 28. A reading of 29
// satisfies neither, so nothing happens — and the episode records that nothing
// happened, instead of silently keeping the old verdict.
func TestHysteresisNeutralZoneRecordsNoTransition(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	def.Trigger.Groups[0].Conditions[0].Value = "30" // > 30
	def.Recovery = &domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
		Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
		Operator: domain.OpLt, Value: "28", Unit: "℃",
	}}}}}
	rule := e.createRule(def)

	e.ingest("ev-a08-h1", e.dev1, "temperature", 35.0, "")
	ep, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, "")
	if err != nil {
		t.Fatalf("want an active episode: %v", err)
	}

	// 29 is inside the dead band.
	e.ingest("ev-a08-h2", e.dev1, "temperature", 29.0, "")
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err != nil {
		t.Fatalf("a neutral reading must not clear the alarm: %v", err)
	}
	if tr := e.lastTransitionOf(ep.ID); tr.Kind != runtime.TransitionNoTransition {
		t.Fatalf("want no_transition recorded, got %s", tr.Kind)
	}

	// 27 is below the recovery threshold, so now it clears.
	e.ingest("ev-a08-h3", e.dev1, "temperature", 27.0, "")
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err == nil {
		t.Fatal("a reading below the recovery threshold must clear the alarm")
	}
}

// A recovery window is a duration, not a hint: 4m59s of clean air is not 5m.
func TestRecoveryMustHoldForTheFullWindow(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	def.Recovery.For = domain.Duration(5 * time.Minute)
	def.Freshness = domain.Freshness{
		MaxAge: domain.Duration(10 * time.Minute), MaxGap: domain.Duration(10 * time.Minute),
	}
	rule := e.createRule(def)

	e.ingest("ev-a08-r1", e.dev1, "temperature", 35.0, "")
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err != nil {
		t.Fatalf("want an active episode: %v", err)
	}
	// The recovery condition starts holding.
	e.ingest("ev-a08-r2", e.dev1, "temperature", 20.0, "")

	e.advance(4*time.Minute + 59*time.Second)
	e.tick()
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err != nil {
		t.Fatalf("4m59s of recovery must not clear the alarm: %v", err)
	}

	e.advance(time.Second)
	e.tick()
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err == nil {
		t.Fatal("the full window must clear the alarm")
	}
}

// A restart in the middle of a recovery window must continue the same window.
// Starting a new one would mean the alarm clears 5 minutes after every restart,
// which is exactly the "restart resets the timer" defect being avoided.
func TestRecoveryPendingSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	def.Recovery.For = domain.Duration(5 * time.Minute)
	def.Freshness = domain.Freshness{
		MaxAge: domain.Duration(10 * time.Minute), MaxGap: domain.Duration(10 * time.Minute),
	}
	rule := e.createRule(def)

	e.ingest("ev-a08-p1", e.dev1, "temperature", 35.0, "")
	e.ingest("ev-a08-p2", e.dev1, "temperature", 20.0, "")

	scope := runtime.ScopeKey(&def, rule.ID, e.dev1) + ":recovery"
	before, err := e.st.GetPending(e.ctx(), scope)
	if err != nil {
		t.Fatalf("want a recovery pending: %v", err)
	}
	startedAt, deadline := before.StartedAt, before.Deadline

	// Four minutes in, the process restarts.
	e.advance(4 * time.Minute)
	e.restart()

	after, err := e.st.GetPending(e.ctx(), scope)
	if err != nil {
		t.Fatalf("the recovery pending must survive a restart: %v", err)
	}
	if !after.StartedAt.Equal(startedAt) || !after.Deadline.Equal(deadline) {
		t.Fatalf("a restart must not restart the window: started %s->%s deadline %s->%s",
			startedAt, after.StartedAt, deadline, after.Deadline)
	}

	// The original deadline still clears it, one minute later.
	e.advance(time.Minute)
	e.tick()
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err == nil {
		t.Fatal("the original window must still complete after a restart")
	}
}

// ---------------------------------------------------------------------------
// Silence
// ---------------------------------------------------------------------------

// A single high reading followed by silence must not become "high for 5
// minutes". The pending expires with a readable reason instead of firing.
func TestSilenceAfterHighReadingDoesNotQualify(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	def.Trigger.For = domain.Duration(5 * time.Minute)
	def.Freshness = domain.Freshness{
		MaxAge: domain.Duration(2 * time.Minute), MaxGap: domain.Duration(2 * time.Minute),
	}
	rule := e.createRule(def)

	e.ingest("ev-a08-s1", e.dev1, "temperature", 35.0, "")
	// Nothing else arrives; the device is gone.
	e.advance(10 * time.Minute)
	e.tick()

	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err == nil {
		t.Fatal("silence must not qualify an alarm")
	}
	expired := e.eventsOfKind("pending.expired")
	if len(expired) != 1 {
		t.Fatalf("want one pending.expired, got %d", len(expired))
	}
	if scope, _ := e.st.GetPending(e.ctx(), runtime.ScopeKey(&def, rule.ID, e.dev1)); scope != nil {
		t.Fatal("the pending must be cleared once it expired")
	}
}

// An alarm that is already active when the device dies stays active — the
// device may be fine — but the episode records that it can no longer be
// confirmed. Otherwise the alarm silently keeps asserting yesterday's value.
func TestNoDataMarksActiveAlarmUnconfirmed(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	def.Freshness = domain.Freshness{
		MaxAge: domain.Duration(10 * time.Minute), MaxGap: domain.Duration(10 * time.Minute),
		NoData: domain.Duration(5 * time.Minute),
	}
	rule := e.createRule(def)

	e.ingest("ev-a08-n1", e.dev1, "temperature", 35.0, "")
	ep, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, "")
	if err != nil {
		t.Fatalf("want an active episode: %v", err)
	}

	// Six minutes of silence.
	e.advance(6 * time.Minute)
	res := e.tick()
	if res.NoDataNoted != 1 {
		t.Fatalf("want one no-data note, got %+v", res)
	}
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err != nil {
		t.Fatalf("no-data must not clear the alarm: %v", err)
	}
	tr := e.lastTransitionOf(ep.ID)
	if tr.Kind != runtime.TransitionUnknown {
		t.Fatalf("want an unknown transition, got %s", tr.Kind)
	}
	evidence, _ := tr.Evidence.(map[string]any)
	if code, _ := evidence["code"].(string); code != "no_data" {
		t.Fatalf("want code no_data, got %v", evidence["code"])
	}
	if len(e.eventsOfKind("alarm.no_data")) != 1 {
		t.Fatal("a no-data note must be published once")
	}

	// The note is written once per silence, not on every pass.
	if again := e.tick(); again.NoDataNoted != 0 {
		t.Fatalf("the note must not repeat, got %+v", again)
	}

	// Silence does not block a real recovery: the device comes back cool.
	e.ingest("ev-a08-n2", e.dev1, "temperature", 20.0, "")
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, ""); err == nil {
		t.Fatal("a genuine recovery must still clear the alarm")
	}
}

// A policy without a no_data window keeps the legacy behaviour: silence alone
// does not produce lifecycle facts.
func TestNoDataIsOptIn(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	rule := e.createRule(def)

	e.ingest("ev-a08-o1", e.dev1, "temperature", 35.0, "")
	ep, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, rule.ID, e.dev1, "")
	if err != nil {
		t.Fatalf("want an active episode: %v", err)
	}
	e.advance(2 * time.Hour)
	res := e.tick()
	if res.NoDataNoted != 0 {
		t.Fatalf("no_data is opt-in, got %+v", res)
	}
	if tr := e.lastTransitionOf(ep.ID); tr.Kind != runtime.TransitionQualified {
		t.Fatalf("nothing should have been added, last is %s", tr.Kind)
	}
}

// ---------------------------------------------------------------------------
// Change-only signals end to end
// ---------------------------------------------------------------------------

// A change-only field keeps working after its value stops changing, and stops
// working when the heartbeat lapses. This is the case where "use the value's
// timestamp" is simply wrong.
func TestOnChangeFieldUsesHeartbeatEndToEnd(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.Groups[0].Conditions = []domain.Condition{
		{Source: "self", Signal: domain.SignalTelemetry, Key: "pressure",
			Operator: domain.OpGt, Value: "0", Unit: ""},
		{Source: "self", Signal: domain.SignalAttribute, Key: "mode",
			Operator: domain.OpEq, Value: "1", Unit: ""},
	}
	def.Freshness = domain.Freshness{
		MaxAge: domain.Duration(1 * time.Minute), MaxGap: domain.Duration(1 * time.Minute),
		Mode: domain.FreshnessOnChange, Heartbeat: domain.Duration(10 * time.Minute),
	}
	rule := e.createRule(def)

	// The attribute is reported once, and the device announces it is online.
	e.ingestSignal("ev-a08-c1", e.dev1, domain.SignalAttribute, "mode", 1)
	e.ingestSignal("ev-a08-c2", e.dev1, domain.SignalStatus, "online", true)
	e.ingestSignal("ev-a08-c3", e.dev1, domain.SignalTelemetry, "pressure", 5)
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("want 1 run, got %d", n)
	}

	// Five minutes later the attribute value is far older than max_age, but the
	// heartbeat is still inside its window, so the value is still usable.
	e.advance(5 * time.Minute)
	e.ingestSignal("ev-a08-c4", e.dev1, domain.SignalTelemetry, "pressure", 6)
	if n := len(e.runs(rule.ID)); n != 2 {
		t.Fatalf("a change-only value must not expire by its own age, got %d runs", n)
	}

	// The heartbeat lapses: now the same value cannot be used.
	e.advance(6 * time.Minute) // 11 minutes since the status
	e.ingestSignal("ev-a08-c5", e.dev1, domain.SignalTelemetry, "pressure", 7)
	if n := len(e.runs(rule.ID)); n != 2 {
		t.Fatalf("a lapsed heartbeat must stop new runs, got %d", n)
	}
}
