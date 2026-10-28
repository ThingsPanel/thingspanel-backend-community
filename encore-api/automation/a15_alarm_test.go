package automation

import (
	"encoding/json"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A15 — alarm policy and lifecycle
//
// The three things these tests exist to keep true:
//   - one continuous abnormality is one alarm, not one per sample;
//   - acknowledging says "someone knows", never "the device is healthy";
//   - an alarm transition is announced once, through the event exit, and the
//     linkage it triggers cannot be tricked into running twice or forever.
// ---------------------------------------------------------------------------

// alarmPolicy builds a condition-type alarm policy: temperature above 30 for
// longer than 0, recovering below 25.
func (e *testEnv) alarmPolicy(tune func(*domain.Definition)) *store.Rule {
	e.t.Helper()
	def := domain.Definition{
		Kind:  domain.KindAlarm,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "30", Unit: "℃",
		}}}}},
		Recovery: &domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpLt, Value: "25", Unit: "℃",
		}}}}},
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{e.dev1}},
		Steps: []domain.Step{{Kind: domain.StepNotify,
			NotificationPolicyID: "np-alarm", Value: "告警"}},
		Alarm: &domain.AlarmSpec{
			Key: "policy.temperature." + e.dev1, Sources: []domain.AlarmSource{domain.AlarmSourceDetector},
			Level: "H",
		},
	}
	if tune != nil {
		tune(&def)
	}
	return e.createRule(def)
}

// alarmEvent submits an event-channel action and fails on a transport error.
func (e *testEnv) alarmEvent(policyID string, req runtime.AlarmEventRequest) runtime.Decision {
	e.t.Helper()
	dec, err := e.rt.SubmitAlarmEvent(e.ctx(), e.tenant, policyID, req)
	if err != nil {
		e.t.Fatalf("submit alarm event: %v", err)
	}
	return dec
}

// activeAlarm returns the active episode of a policy key, or nil.
func (e *testEnv) activeAlarm(policyKey, subject string) *store.Episode {
	e.t.Helper()
	ep, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, policyKey, subject, "")
	if err != nil {
		return nil
	}
	return ep
}

func (e *testEnv) episodesOf(policyKey string) []*store.Episode {
	e.t.Helper()
	eps, err := e.st.ListEpisodes(e.ctx(), e.tenant, policyKey, "", "", 100)
	if err != nil {
		e.t.Fatalf("list episodes: %v", err)
	}
	return eps
}

// TestRepeatedAbnormalSamplesKeepOneActiveIdentity: a continuous abnormality is
// one alarm whose evidence keeps updating, not a new alarm per uplink.
func TestRepeatedAbnormalSamplesKeepOneActiveIdentity(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(nil)

	for i := 0; i < 5; i++ {
		e.advance(time.Second)
		e.ingest("ev-a15-repeat-"+itoaTest(i), e.dev1, "temperature", 35.0, "")
	}

	eps := e.episodesOf(policy.PolicyKey())
	if len(eps) != 1 {
		t.Fatalf("want exactly one alarm for one continuous condition, got %d", len(eps))
	}
	ep := eps[0]
	if ep.State != "active" {
		t.Fatalf("want active, got %q", ep.State)
	}
	if ep.SampleCount != 5 {
		t.Fatalf("want 5 samples folded into the episode, got %d", ep.SampleCount)
	}
	// One lifecycle fact, not five: the condition never changed.
	trs, err := e.st.ListTransitions(e.ctx(), ep.ID)
	if err != nil {
		t.Fatalf("transitions: %v", err)
	}
	if len(trs) != 1 || trs[0].Kind != runtime.TransitionQualified {
		t.Fatalf("want one qualified fact, got %+v", trs)
	}
	// And exactly one announcement, so a linkage fires once.
	if evs := e.eventsOfKind("alarm.qualified"); len(evs) != 1 {
		t.Fatalf("want one alarm.qualified event, got %d", len(evs))
	}
}

// TestAcknowledgingDoesNotClearTheAlarm: ack changes "someone knows", not
// "the device is healthy".
func TestAcknowledgingDoesNotClearTheAlarm(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(nil)

	e.ingest("ev-a15-ack-1", e.dev1, "temperature", 35.0, "")
	ep := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if ep == nil {
		t.Fatal("want an active alarm")
	}

	ok, err := e.rt.Ack(e.ctx(), e.tenant, ep.ID, "user-1")
	if err != nil || !ok {
		t.Fatalf("ack: ok=%v err=%v", ok, err)
	}

	still := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if still == nil {
		t.Fatal("acknowledging must not clear the alarm")
	}
	if still.ID != ep.ID {
		t.Fatalf("the alarm identity must not change on ack: %s vs %s", still.ID, ep.ID)
	}
	if still.AckedBy != "user-1" || still.AckedAt == nil {
		t.Fatalf("the ack must be recorded: %+v", still)
	}

	// The condition is still abnormal, and that still produces no new alarm.
	e.advance(time.Second)
	e.ingest("ev-a15-ack-2", e.dev1, "temperature", 36.0, "")
	if got := len(e.episodesOf(policy.PolicyKey())); got != 1 {
		t.Fatalf("an acked alarm must stay the same alarm, got %d episodes", got)
	}
}

// TestPublishingANewRevisionDoesNotDuplicateOrClearTheAlarm is the "发布新版不无故
// 重复建告警" requirement: the alarm keeps its identity and simply starts being
// judged by the new revision.
func TestPublishingANewRevisionDoesNotDuplicateOrClearTheAlarm(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(nil)

	firstRevision := policy.Revision
	e.ingest("ev-a15-rev-1", e.dev1, "temperature", 35.0, "")
	before := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if before == nil {
		t.Fatal("want an active alarm")
	}

	// Publish a new revision: same business key, tighter threshold.
	updated := policy
	updated.Definition = policy.Definition
	updated.Definition.Trigger.Groups[0].Conditions[0].Value = "32"
	updated.Definition.Alarm = &domain.AlarmSpec{
		Key: policy.PolicyKey(), Sources: []domain.AlarmSource{domain.AlarmSourceDetector},
		Level: "M",
	}
	if err := domain.Validate(&updated.Definition); err != nil {
		t.Fatalf("updated definition is invalid: %v", err)
	}
	updated.DefinitionHash = domain.DefinitionHash(updated.Definition)
	if err := e.st.UpdateRule(e.ctx(), updated, policy.Version); err != nil {
		t.Fatalf("update rule: %v", err)
	}
	reloaded, err := e.st.GetRule(e.ctx(), e.tenant, policy.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Revision <= firstRevision {
		t.Fatalf("the update must publish a new revision, got %d", reloaded.Revision)
	}

	e.advance(time.Second)
	e.ingest("ev-a15-rev-2", e.dev1, "temperature", 35.0, "")

	eps := e.episodesOf(policy.PolicyKey())
	if len(eps) != 1 {
		t.Fatalf("a new revision must not open a second alarm, got %d", len(eps))
	}
	after := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if after == nil {
		t.Fatal("a new revision must not clear the alarm")
	}
	if after.ID != before.ID {
		t.Fatalf("the alarm identity must survive a new revision: %s vs %s", after.ID, before.ID)
	}
	if after.Revision != reloaded.Revision {
		t.Fatalf("the alarm must be judged by the new revision: want %d got %d",
			reloaded.Revision, after.Revision)
	}
	if after.Level != "H" {
		// The level belongs to the revision the alarm was opened under; changing
		// it silently would rewrite history.
		t.Fatalf("the opened level must not be rewritten: %q", after.Level)
	}
}

// TestADetectorPolicyRefusesTheEventChannel: an ordinary business request must
// never be able to impersonate the evaluator.
func TestADetectorPolicyRefusesTheEventChannel(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(nil)

	dec := e.alarmEvent(policy.ID, runtime.AlarmEventRequest{
		ActionID: "act-1", Source: "scene:any", Kind: "qualified", SubjectID: e.dev1,
	})
	if dec.Outcome != runtime.OutcomeRejected || dec.ReasonCode != runtime.ReasonNotEvent {
		t.Fatalf("a detector-only policy must refuse the event channel, got %+v", dec)
	}
	if e.activeAlarm(policy.PolicyKey(), e.dev1) != nil {
		t.Fatal("a refused event must not raise an alarm")
	}
}

// TestAnEventSourceMustBeWhitelisted: the event channel is authorised per policy.
func TestAnEventSourceMustBeWhitelisted(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(func(d *domain.Definition) {
		d.Alarm.Sources = []domain.AlarmSource{domain.AlarmSourceDetector, domain.AlarmSourceEvent}
		d.Alarm.AllowedSources = []string{"scene:fire-drill"}
	})

	denied := e.alarmEvent(policy.ID, runtime.AlarmEventRequest{
		ActionID: "act-1", Source: "scene:other", Kind: "qualified", SubjectID: e.dev1,
	})
	if denied.Outcome != runtime.OutcomeRejected || denied.ReasonCode != runtime.ReasonSourceDenied {
		t.Fatalf("an unlisted source must be refused, got %+v", denied)
	}

	allowed := e.alarmEvent(policy.ID, runtime.AlarmEventRequest{
		ActionID: "act-2", Source: "scene:fire-drill", Kind: "qualified", SubjectID: e.dev1,
	})
	if allowed.Outcome != runtime.OutcomeQualified {
		t.Fatalf("a whitelisted source must be able to qualify, got %+v", allowed)
	}
	if ep := e.activeAlarm(policy.PolicyKey(), e.dev1); ep == nil || ep.Source != "event:scene:fire-drill" {
		t.Fatalf("the episode must record which channel raised it: %+v", ep)
	}
}

// TestTheSameEventActionIsAppliedOnce: a redelivered scene action must not raise
// a second alarm, and the second answer must be the first one.
func TestTheSameEventActionIsAppliedOnce(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(func(d *domain.Definition) {
		d.Alarm.Sources = []domain.AlarmSource{domain.AlarmSourceEvent}
		d.Alarm.AllowedSources = []string{"automation"}
	})

	req := runtime.AlarmEventRequest{
		ActionID: "act-once", Source: "automation", Kind: "qualified", SubjectID: e.dev1,
	}
	first := e.alarmEvent(policy.ID, req)
	if first.Outcome != runtime.OutcomeQualified || first.Replayed {
		t.Fatalf("first submission must qualify, got %+v", first)
	}
	second := e.alarmEvent(policy.ID, req)
	if !second.Replayed {
		t.Fatalf("the second submission must be answered from the first, got %+v", second)
	}
	if second.TransitionID != first.TransitionID || second.EpisodeID != first.EpisodeID {
		t.Fatalf("a replay must return the same identity: %+v vs %+v", second, first)
	}
	if got := len(e.episodesOf(policy.PolicyKey())); got != 1 {
		t.Fatalf("want one alarm, got %d", got)
	}
	if evs := e.eventsOfKind("alarm.qualified"); len(evs) != 1 {
		t.Fatalf("want one announcement, got %d", len(evs))
	}
}

// TestTheSameActionIdWithDifferentContentIsAConflict: an id that means two
// different things is a caller bug, not a second alarm.
func TestTheSameActionIdWithDifferentContentIsAConflict(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(func(d *domain.Definition) {
		d.Alarm.Sources = []domain.AlarmSource{domain.AlarmSourceEvent}
		d.Alarm.AllowedSources = []string{"automation"}
	})

	first := e.alarmEvent(policy.ID, runtime.AlarmEventRequest{
		ActionID: "act-conflict", Source: "automation", Kind: "qualified", SubjectID: e.dev1,
	})
	if first.Outcome != runtime.OutcomeQualified {
		t.Fatalf("first: %+v", first)
	}
	conflict := e.alarmEvent(policy.ID, runtime.AlarmEventRequest{
		ActionID: "act-conflict", Source: "automation", Kind: "clear", SubjectID: e.dev1,
	})
	if conflict.Outcome != runtime.OutcomeRejected || conflict.ReasonCode != runtime.ReasonConflict {
		t.Fatalf("a reused id with different content must be a conflict, got %+v", conflict)
	}
	if e.activeAlarm(policy.PolicyKey(), e.dev1) == nil {
		t.Fatal("the conflict must not clear the alarm it refused to interpret")
	}
}

// TestAnOutOfOrderOrStaleExecutorCannotRewriteTheLifecycle covers the two guards
// that make "拒绝旧执行者和乱序恢复" true rather than aspirational.
func TestAnOutOfOrderOrStaleExecutorCannotRewriteTheLifecycle(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(nil)

	e.ingest("ev-a15-guard-1", e.dev1, "temperature", 35.0, "")
	ep := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if ep == nil {
		t.Fatal("want an active alarm")
	}

	// A worker that read the alarm before the last sample must not rewind it.
	applied, err := e.st.RefreshEpisode(e.ctx(), nil, ep.ID, ep.LastSeq-1, nil)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if applied {
		t.Fatal("a sample older than the watermark must be refused")
	}

	// A clear from a superseded revision must not close a newer alarm.
	outcome, err := e.st.ClearEpisode(e.ctx(), nil, ep.ID, ep.LastSeq+100, ep.Revision-1)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if outcome != store.ClearStaleRev {
		t.Fatalf("want stale_revision, got %q", outcome)
	}

	// And an out-of-order clear is refused rather than silently ignored.
	outcome, err = e.st.ClearEpisode(e.ctx(), nil, ep.ID, ep.LastSeq, ep.Revision)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if outcome != store.ClearOutOfOrder {
		t.Fatalf("want out_of_order, got %q", outcome)
	}
	if e.activeAlarm(policy.PolicyKey(), e.dev1) == nil {
		t.Fatal("a refused clear must leave the alarm active")
	}
}

// TestTheTransitionIsAnnouncedThroughTheEventExit: the announcement carries the
// stable transition id, the policy and subject, the sequence and the chain.
func TestTheTransitionIsAnnouncedThroughTheEventExit(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(nil)

	e.ingest("ev-a15-exit-1", e.dev1, "temperature", 35.0, "")
	evs := e.eventsOfKind("alarm.qualified")
	if len(evs) != 1 {
		t.Fatalf("want one alarm.qualified, got %d", len(evs))
	}
	body := decodeJSON(t, evs[0].Payload)
	for _, key := range []string{"transition_id", "episode_id", "policy_id", "policy_key",
		"subject_id", "seq", "revision", "causal_root", "causal_hop", "level", "source"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("the announcement is missing %q: %s", key, evs[0].Payload)
		}
	}
	if body["policy_key"] != policy.PolicyKey() {
		t.Fatalf("policy_key: %v", body["policy_key"])
	}
	if body["transition_id"] == "" || body["transition_id"] == nil {
		t.Fatal("the announcement must carry a transition id")
	}
}

// TestOneTransitionTriggersTheLinkedRule is the real linkage: an alarm transition
// becomes a rule input through the event exit, without anyone polling the alarm
// table.
func TestOneTransitionTriggersTheLinkedRule(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(nil)

	linked := domain.Definition{
		Kind:  domain.KindAutomation,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalAlarm, Key: "qualified",
			Operator: domain.OpEq, Value: "qualified",
		}}}}},
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{e.dev1}},
		Steps: []domain.Step{{Kind: domain.StepAction, DeviceRef: "target",
			Action: "telemetry", Identifier: "fan", Value: "1"}},
	}
	e.createRule(linked)

	before := e.cmds.count()
	e.ingest("ev-a15-link-1", e.dev1, "temperature", 35.0, "")
	if e.activeAlarm(policy.PolicyKey(), e.dev1) == nil {
		t.Fatal("want an active alarm before the linkage can fire")
	}
	if e.cmds.count() != before {
		t.Fatal("the linkage must not fire before the transition is relayed")
	}
	e.tick()
	if e.cmds.count() != before+1 {
		t.Fatalf("the alarm transition must trigger the linked rule once, got %d commands",
			e.cmds.count()-before)
	}

	// Redelivering the same transition must not run the linkage again.
	e.tick()
	if e.cmds.count() != before+1 {
		t.Fatalf("a redelivered transition must not run the linkage twice, got %d",
			e.cmds.count()-before)
	}
}

// TestAnAlarmLinkageLoopIsStoppedByTheHopLimit constructs the loop on purpose: a
// rule reacts to an alarm transition and its action raises the same alarm again.
// The chain must be cut at the hop limit and the reason must be published.
func TestAnAlarmLinkageLoopIsStoppedByTheHopLimit(t *testing.T) {
	e := newEnv(t)

	// The policy is fed only by the event channel, so the loop is the only source
	// of transitions.
	policy := e.alarmPolicy(func(d *domain.Definition) {
		d.Alarm.Sources = []domain.AlarmSource{domain.AlarmSourceEvent}
		d.Alarm.AllowedSources = []string{"automation"}
	})

	// The linkage reacts to a qualified transition and, in its own steps, clears
	// the alarm and raises it again — a deliberate cycle.
	loopDef := domain.Definition{
		Kind:  domain.KindAutomation,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalAlarm, Key: "qualified",
			Operator: domain.OpEq, Value: "qualified",
		}}}}},
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{e.dev1}},
		Steps: []domain.Step{
			{Kind: domain.StepAlarm, AlarmPolicyID: policy.ID, DeviceRef: "target", Value: "clear"},
			{Kind: domain.StepAlarm, AlarmPolicyID: policy.ID, DeviceRef: "target", Value: "qualified"},
		},
	}
	loop := e.createRule(loopDef)

	// Seed the chain.
	seed := e.alarmEvent(policy.ID, runtime.AlarmEventRequest{
		ActionID: "seed", Source: "automation", Kind: "qualified", SubjectID: e.dev1,
	})
	if seed.Outcome != runtime.OutcomeQualified {
		t.Fatalf("seed: %+v", seed)
	}

	// Let the chain run. Every pass relays at most the transitions produced by the
	// previous one, so a bounded number of ticks is enough to hit the limit.
	for i := 0; i < 30; i++ {
		e.advance(time.Second)
		e.tick()
	}

	if len(e.eventsOfKind("causal.hop_exceeded")) == 0 {
		t.Fatal("a deliberately constructed alarm-linkage loop must be stopped and traced")
	}
	// Bounded, not unbounded: the hop limit is what makes this a loop that ends.
	if runs := e.runs(loop.ID); len(runs) > runtime.MaxCausalHop+2 {
		t.Fatalf("the loop must be bounded by the hop limit, got %d runs", len(runs))
	}
}

// TestUnknownAndErrorAreNotRecoveries keeps the two undetermined verdicts from
// being folded into "the alarm cleared".
func TestUnknownAndErrorAreNotRecoveries(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(nil)

	e.ingest("ev-a15-und-1", e.dev1, "temperature", 35.0, "")
	ep := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if ep == nil {
		t.Fatal("want an active alarm")
	}

	e.advance(time.Second)
	e.ingest("ev-a15-und-2", e.dev1, "temperature", 35.0, string(domain.QualityUnknown))
	if e.activeAlarm(policy.PolicyKey(), e.dev1) == nil {
		t.Fatal("an unknown sample must not clear the alarm")
	}
	last, err := e.st.LastTransition(e.ctx(), ep.ID)
	if err != nil {
		t.Fatalf("last transition: %v", err)
	}
	if last.Kind != runtime.TransitionUnknown {
		t.Fatalf("want an explicit unknown fact, got %q", last.Kind)
	}
}

// TestTheAlarmEventIntakeRejectsAnUnsupportedKind: only qualified and clear can be
// asserted from outside; "unknown" and "error" are the evaluator's to know.
func TestTheAlarmEventIntakeRejectsAnUnsupportedKind(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(func(d *domain.Definition) {
		d.Alarm.Sources = []domain.AlarmSource{domain.AlarmSourceEvent}
		d.Alarm.AllowedSources = []string{"automation"}
	})

	for _, kind := range []string{"unknown", "error", "no_transition"} {
		dec := e.alarmEvent(policy.ID, runtime.AlarmEventRequest{
			ActionID: "act-" + kind, Source: "automation", Kind: kind, SubjectID: e.dev1,
		})
		if dec.Outcome != runtime.OutcomeRejected || dec.ReasonCode != runtime.ReasonBadKind {
			t.Fatalf("kind %q must be refused, got %+v", kind, dec)
		}
	}
}

// decodeJSON reads a published payload into a map so an assertion can name the
// field it cares about instead of matching a substring.
func decodeJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("payload is not an object: %v (%s)", err, raw)
	}
	return out
}

// itoaTest keeps the test file free of a strconv import that would only be used
// once.
func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestASustainedAlarmEventuallyOpens is the regression guard for a gate that was
// written twice.
//
// The "hold for this long" rule belongs to the detector's entry point. When the
// same check was repeated in decideQualified, the timer's own verdict came back
// through that second check, decided the wait had not happened, and re-armed the
// pending window — so an alarm policy with a duration re-armed itself on every
// tick and never opened, while every log line looked healthy.
func TestASustainedAlarmEventuallyOpens(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(func(d *domain.Definition) {
		d.Trigger.For = domain.Duration(30 * time.Second)
	})

	// One hot sample starts the wait but must not open the alarm.
	e.ingest("ev-a15-sustained-1", e.dev1, "temperature", 35.0, "")
	if eps := e.episodesOf(policy.PolicyKey()); len(eps) != 0 {
		t.Fatalf("one sample opened an alarm that asks for 30s of sustained condition")
	}

	// After the declared duration the timer produces the qualified verdict.
	e.advance(31 * time.Second)
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	eps := e.episodesOf(policy.PolicyKey())
	if len(eps) != 1 {
		t.Fatalf("the sustained condition never opened an alarm (got %d)", len(eps))
	}
	if eps[0].State != "active" {
		t.Fatalf("want an active alarm, got %q", eps[0].State)
	}

	// And the wait is not re-armed afterwards: a second tick must not create a
	// second pending window or a second alarm.
	e.advance(31 * time.Second)
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if eps := e.episodesOf(policy.PolicyKey()); len(eps) != 1 {
		t.Fatalf("the alarm was re-opened instead of staying one alarm (got %d)", len(eps))
	}
}
