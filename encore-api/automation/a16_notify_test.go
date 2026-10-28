package automation

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

type resolvingNotifier struct {
	scriptedNotifier
	snapshot   ports.NotificationPolicy
	resolveErr error
	resolved   int
}

func (n *resolvingNotifier) ResolvePolicy(_ context.Context, _, _ string) (ports.NotificationPolicy, error) {
	n.resolved++
	return n.snapshot, n.resolveErr
}

func TestDefaultAlarmPolicyIsFrozenBeforeDelivery(t *testing.T) {
	e := newEnv(t)
	n := &resolvingNotifier{snapshot: ports.NotificationPolicy{Configured: true, PolicyID: "native-original", GroupRevision: 3}}
	e.withNotifier(n)
	e.alarmPolicy(func(d *domain.Definition) { d.Alarm.NotifyUseDefault = true })
	e.ingest("default-snapshot", e.dev1, "temperature", 35.0, "")
	// A default change after the alarm cannot reroute its queued message.
	n.snapshot = ports.NotificationPolicy{Configured: true, PolicyID: "native-new", GroupRevision: 9}
	e.advance(time.Second)
	e.tick()
	if n.resolved != 1 || n.count() != 1 {
		t.Fatalf("resolve=%d send=%d", n.resolved, n.count())
	}
	if sent := n.at(0); sent.PolicyID != "native-original" || sent.GroupRevision != 3 {
		t.Fatalf("queued notification was rerouted: %+v", sent)
	}
}

func TestUnavailableDefaultKeepsAlarmAndNeverSends(t *testing.T) {
	e := newEnv(t)
	n := &resolvingNotifier{resolveErr: errors.New("unavailable")}
	e.withNotifier(n)
	policy := e.alarmPolicy(func(d *domain.Definition) { d.Alarm.NotifyUseDefault = true })
	e.ingest("default-unavailable", e.dev1, "temperature", 35.0, "")
	e.advance(time.Second)
	e.tick()
	if e.activeAlarm(policy.PolicyKey(), e.dev1) == nil {
		t.Fatal("alarm must survive")
	}
	if n.count() != 0 {
		t.Fatal("unavailable default must not send")
	}
	var count int
	if err := e.st.DB.QueryRowContext(e.ctx(), `SELECT count(*) FROM auto_outbox WHERE tenant_id=$1 AND state='unresolved'`, e.tenant).Scan(&count); err != nil || count != 1 {
		t.Fatalf("missing unresolved evidence: %d %v", count, err)
	}
}

func TestNoDefaultRecordsAlarmWithoutNotification(t *testing.T) {
	e := newEnv(t)
	n := &resolvingNotifier{}
	e.withNotifier(n)
	policy := e.alarmPolicy(func(d *domain.Definition) { d.Alarm.NotifyUseDefault = true })
	e.ingest("no-default", e.dev1, "temperature", 35.0, "")
	if e.activeAlarm(policy.PolicyKey(), e.dev1) == nil {
		t.Fatal("alarm must exist")
	}
	var count int
	if err := e.st.DB.QueryRowContext(e.ctx(), `SELECT count(*) FROM auto_outbox WHERE tenant_id=$1`, e.tenant).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unexpected delivery intent: %d %v", count, err)
	}
}

// ---------------------------------------------------------------------------
// A16 — the existing notification socket
//
// What these tests guard: "handed off to the notification core" is not "delivered
// to a phone"; a lost acceptance answer is replayed with the same frozen intent;
// and an unknown provider outcome is never resent from here.
// ---------------------------------------------------------------------------

// scriptedNotifier returns a scripted sequence of intake results, so a test can
// describe "the first answer was lost, the second was accepted".
type scriptedNotifier struct {
	mu      sync.Mutex
	calls   []ports.NotificationRequest
	results []ports.NotificationResult
}

func (s *scriptedNotifier) Submit(_ context.Context, req ports.NotificationRequest) (ports.NotificationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	idx := len(s.calls) - 1
	if idx >= len(s.results) {
		idx = len(s.results) - 1
	}
	if len(s.results) == 0 {
		return ports.NotificationResult{State: ports.NotificationAccepted, NotificationID: "n-1"}, nil
	}
	return s.results[idx], nil
}

func (s *scriptedNotifier) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *scriptedNotifier) at(i int) ports.NotificationRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[i]
}

// withNotifier rebuilds the runtime with a scripted notification owner.
func (e *testEnv) withNotifier(n ports.NotificationOwner) {
	e.rt = runtime.New(e.st, e.cmds, n, e.dir, func() time.Time { return e.now })
}

// notifyDefinition is a one-step scene that only notifies.
func (e *testEnv) notifyDefinition(text string, revision int64) domain.Definition {
	def := e.baseDefinition()
	def.Kind = domain.KindScene
	def.Trigger = domain.Trigger{}
	def.Steps = []domain.Step{{
		Kind: domain.StepNotify, NotificationPolicyID: "np-1",
		NotificationRevision: revision, Value: text,
	}}
	return def
}

func (e *testEnv) outboxByKey(key string) *store.OutboxItem {
	e.t.Helper()
	var item store.OutboxItem
	var payload []byte
	err := e.st.DB.QueryRowContext(e.ctx(), `
        SELECT id, tenant_id, coalesce(run_id,''), coalesce(step_id,''), kind, scope_key,
               generation, idempotency_key, payload, state, attempt,
               coalesce(accepted_ref,''), coalesce(last_error,'')
          FROM auto_outbox WHERE idempotency_key=$1`, key).
		Scan(&item.ID, &item.TenantID, &item.RunID, &item.StepID, &item.Kind, &item.ScopeKey,
			&item.Generation, &item.IdempotencyKey, &payload, &item.State, &item.Attempt,
			&item.AcceptedRef, &item.LastError)
	if err != nil {
		e.t.Fatalf("outbox %q: %v", key, err)
	}
	item.Payload = payload
	return &item
}

// TestA202WithAcceptanceIsHandedOffNotDelivered: the strongest claim this service
// may make is that the core took the message.
func TestA202WithAcceptanceIsHandedOffNotDelivered(t *testing.T) {
	e := newEnv(t)
	n := &scriptedNotifier{results: []ports.NotificationResult{{
		State: ports.NotificationAccepted, NotificationID: "n-42",
		DeliveryIDs: []string{"d-1", "d-2"}, Message: "accepted",
	}}}
	e.withNotifier(n)

	rule := e.createRule(e.notifyDefinition("门已打开", 7))
	run, err := e.rt.RunNow(e.ctx(), rule, e.dev1)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != runtime.StatusSucceeded {
		t.Fatalf("want succeeded, got %q (%s)", run.Status, run.Reason)
	}

	item := e.outboxByKey(e.onlyOutboxKey(run.ID))
	if item.State != "handed_off" {
		t.Fatalf("want handed_off, got %q", item.State)
	}
	if item.AcceptedRef != "n-42" {
		t.Fatalf("the notification id must be recorded, got %q", item.AcceptedRef)
	}
	// The step must not read as a terminal success at the phone.
	if got := e.stepStatuses(run.ID); len(got) != 1 || got[0] != "0:notify:handed_off" {
		t.Fatalf("want 0:notify:handed_off, got %v", got)
	}
	var body map[string]any
	if err := json.Unmarshal(item.Payload, &body); err != nil {
		t.Fatalf("payload: %v", err)
	}
	outcome, _ := body["outcome"].(map[string]any)
	if outcome["notification_id"] != "n-42" {
		t.Fatalf("the intake evidence must be stored: %v", body["outcome"])
	}
}

// TestALostAcceptanceAnswerIsReplayedWithTheSameFrozenIntent: the request may have
// arrived. Replaying the same key and the same body is the only safe retry, and it
// must not turn into a second message.
func TestALostAcceptanceAnswerIsReplayedWithTheSameFrozenIntent(t *testing.T) {
	e := newEnv(t)
	n := &scriptedNotifier{results: []ports.NotificationResult{
		{State: ports.NotificationAcceptanceUnknown, Message: "read timeout"},
		{State: ports.NotificationAccepted, NotificationID: "n-7", Message: "accepted"},
	}}
	e.withNotifier(n)

	rule := e.createRule(e.notifyDefinition("阀门已关", 3))
	run, err := e.rt.RunNow(e.ctx(), rule, e.dev1)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != runtime.StatusSucceeded {
		t.Fatalf("an unknown acceptance must not fail the run, got %q (%s)", run.Status, run.Reason)
	}
	key := e.onlyOutboxKey(run.ID)
	if item := e.outboxByKey(key); item.State != "dispatched" {
		t.Fatalf("an unknown acceptance must stay dispatched for replay, got %q", item.State)
	}
	if got := e.stepStatuses(run.ID); len(got) != 1 || got[0] != "0:notify:dispatched" {
		t.Fatalf("want 0:notify:dispatched, got %v", got)
	}

	// The core is reachable again. The replay must be the same intent.
	// The retry time is a database timestamp, so it is forced due here instead of
	// waiting for the wall clock.
	e.forceOutboxDue(key)
	e.advance(45 * time.Second)
	e.tick()
	if n.count() != 2 {
		t.Fatalf("want a second attempt, got %d calls", n.count())
	}
	first, second := n.at(0), n.at(1)
	if first.IdempotencyKey != second.IdempotencyKey {
		t.Fatalf("the replay must reuse the idempotency key: %q vs %q",
			first.IdempotencyKey, second.IdempotencyKey)
	}
	if first.Text != second.Text || !first.OccurredAt.Equal(second.OccurredAt) ||
		first.GroupRevision != second.GroupRevision {
		t.Fatalf("the replay must resend the frozen body: %+v vs %+v", first, second)
	}
	if item := e.outboxByKey(key); item.State != "handed_off" {
		t.Fatalf("after the replay the intent must be handed off, got %q", item.State)
	}
}

// TestAProviderUnknownOutcomeIsNotResent: the core owns the message; resending
// from here would bypass it and could deliver twice.
func TestAProviderUnknownOutcomeIsNotResent(t *testing.T) {
	e := newEnv(t)
	n := &scriptedNotifier{results: []ports.NotificationResult{{
		State: ports.NotificationProviderUnknown, NotificationID: "n-9",
		Message: "provider_unknown",
	}}}
	e.withNotifier(n)

	rule := e.createRule(e.notifyDefinition("泵已启动", 1))
	run, err := e.rt.RunNow(e.ctx(), rule, e.dev1)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	item := e.outboxByKey(e.onlyOutboxKey(run.ID))
	if item.State != "unresolved" {
		t.Fatalf("want unresolved, got %q", item.State)
	}
	if got := e.stepStatuses(run.ID); len(got) != 1 || got[0] != "0:notify:provider_unknown" {
		t.Fatalf("want 0:notify:provider_unknown, got %v", got)
	}

	// Several passes later it must still be exactly one call.
	for i := 0; i < 3; i++ {
		e.advance(2 * time.Minute)
		e.tick()
	}
	if n.count() != 1 {
		t.Fatalf("a provider-unknown outcome must not be resent, got %d calls", n.count())
	}
}

// TestEditingTheNotificationGroupDoesNotRewriteAQueuedMessage: the body is frozen
// when the intent is created, not when it is sent.
func TestEditingTheNotificationGroupDoesNotRewriteAQueuedMessage(t *testing.T) {
	e := newEnv(t)
	n := &scriptedNotifier{results: []ports.NotificationResult{
		{State: ports.NotificationAcceptanceUnknown, Message: "read timeout"},
		{State: ports.NotificationAccepted, NotificationID: "n-11"},
	}}
	e.withNotifier(n)

	rule := e.createRule(e.notifyDefinition("原文本", 5))
	run, err := e.rt.RunNow(e.ctx(), rule, e.dev1)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Somebody edits the notification group and the rule in the meantime.
	edited := *rule
	edited.Definition = rule.Definition
	edited.Definition.Steps = []domain.Step{{
		Kind: domain.StepNotify, NotificationPolicyID: "np-2",
		NotificationRevision: 9, Value: "改过的文本",
	}}
	edited.DefinitionHash = domain.DefinitionHash(edited.Definition)
	if err := e.st.UpdateRule(e.ctx(), &edited, rule.Version); err != nil {
		t.Fatalf("update rule: %v", err)
	}

	e.forceOutboxDue(e.onlyOutboxKey(run.ID))
	e.advance(45 * time.Second)
	e.tick()
	if n.count() != 2 {
		t.Fatalf("want the replay, got %d calls", n.count())
	}
	sent := n.at(1)
	if sent.Text != "原文本" {
		t.Fatalf("a queued message must not be rewritten, got %q", sent.Text)
	}
	if sent.GroupRevision != 5 {
		t.Fatalf("the frozen group revision must be kept, got %d", sent.GroupRevision)
	}
	if sent.PolicyID != "np-1" {
		t.Fatalf("the frozen policy must be kept, got %q", sent.PolicyID)
	}
	if item := e.outboxByKey(e.onlyOutboxKey(run.ID)); item.State != "handed_off" {
		t.Fatalf("the replay must still hand off, got %q", item.State)
	}
}

// TestAnAlarmAndItsNotificationAreCommittedTogether: the alarm owner writes the
// transition and the notification intent in one transaction, keyed by the
// transition id.
func TestAnAlarmAndItsNotificationAreCommittedTogether(t *testing.T) {
	e := newEnv(t)
	n := &scriptedNotifier{results: []ports.NotificationResult{{
		State: ports.NotificationAccepted, NotificationID: "n-alarm",
	}}}
	e.withNotifier(n)

	policy := e.alarmPolicy(func(d *domain.Definition) {
		d.Alarm.NotifyPolicyID = "np-alarm"
		d.Alarm.NotifyRevision = 4
	})
	e.ingest("ev-a16-alarm-1", e.dev1, "temperature", 35.0, "")

	ep := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if ep == nil {
		t.Fatal("want an active alarm")
	}
	var transitionID string
	if err := e.st.DB.QueryRowContext(e.ctx(), `
        SELECT id FROM alarm_transitions WHERE episode_id=$1 AND kind='qualified'`, ep.ID).
		Scan(&transitionID); err != nil {
		t.Fatalf("transition: %v", err)
	}

	item := e.outboxByKey("alarm:" + transitionID + ":notify")
	if item.Kind != "notification" {
		t.Fatalf("want a notification intent, got %q", item.Kind)
	}
	if item.RunID != "" {
		t.Fatal("an alarm notification does not belong to a run")
	}
	var body map[string]any
	if err := json.Unmarshal(item.Payload, &body); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if body["group_revision"] != float64(4) {
		t.Fatalf("the group revision must be frozen: %v", body["group_revision"])
	}

	// Dispatching it hands it off, and the alarm is still active.
	e.advance(time.Second)
	e.tick()
	if got := e.outboxByKey("alarm:" + transitionID + ":notify"); got.State != "handed_off" {
		t.Fatalf("want handed_off, got %q", got.State)
	}
	if n.count() != 1 {
		t.Fatalf("want one notification, got %d", n.count())
	}
	if e.activeAlarm(policy.PolicyKey(), e.dev1) == nil {
		t.Fatal("notifying must not change the alarm state")
	}
}

// TestAMissingNotificationOwnerKeepsTheAlarm: losing the message must not lose the
// business fact.
func TestAMissingNotificationOwnerKeepsTheAlarm(t *testing.T) {
	e := newEnv(t)
	e.withNotifier(nil)

	policy := e.alarmPolicy(func(d *domain.Definition) {
		d.Alarm.NotifyPolicyID = "np-alarm"
	})
	e.ingest("ev-a16-nonotify-1", e.dev1, "temperature", 35.0, "")
	ep := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if ep == nil {
		t.Fatal("the alarm must survive a missing notification owner")
	}

	e.advance(time.Second)
	e.tick()
	var state string
	if err := e.st.DB.QueryRowContext(e.ctx(), `
        SELECT state FROM auto_outbox WHERE kind='notification' AND tenant_id=$1`,
		e.tenant).Scan(&state); err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if state != "unresolved" {
		t.Fatalf("want unresolved, got %q", state)
	}
	if e.activeAlarm(policy.PolicyKey(), e.dev1) == nil {
		t.Fatal("the alarm must still be active")
	}
}

// forceOutboxDue makes a queued intent claimable now.
//
// The retry time is written as a database timestamp, so advancing the test clock
// alone would not make it due; the tests that replay an intent force the row
// instead of sleeping.
func (e *testEnv) forceOutboxDue(key string) {
	e.t.Helper()
	if _, err := e.st.DB.ExecContext(e.ctx(),
		`UPDATE auto_outbox SET next_attempt_at = now() - interval '1 second' WHERE idempotency_key=$1`,
		key); err != nil {
		e.t.Fatalf("force due: %v", err)
	}
}

// onlyOutboxKey returns the single intent of a run, failing when there is not
// exactly one.
func (e *testEnv) onlyOutboxKey(runID string) string {
	e.t.Helper()
	items := e.outboxFor(runID)
	if len(items) != 1 {
		e.t.Fatalf("want exactly one intent, got %d", len(items))
	}
	return items[0].IdempotencyKey
}
