package automation

import (
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/domain"
	"encore.app/automation/executor"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A28 — the external executor boundary
//
// These tests drive the five endpoints a Node-RED or Flink deployment talks to.
// They exist because the interesting failures here are not "the handler returned
// 400": they are "the same physical action happened twice", "an engine deployed
// against last week's threshold raised an alarm today", and "a raw observation
// was read as a five-minute hold". Every case below therefore asserts on what
// the platform did *after* the refusal — runs, commands, alarms — not only on
// the status code.
//
// The service-level tests run under the fixed tenant the auth layer hands back
// outside a real request, so rules and policies are created in serviceTestTenant.
// ---------------------------------------------------------------------------

// executorContract is the smallest registration that may both run a rule and
// qualify an alarm. Each test starts from this and breaks exactly one field, so
// a failure names one rule instead of "the envelope is wrong somewhere".
func executorContract(deviceIDs ...string) executor.Contract {
	return executor.Contract{
		Engine:     executor.EngineNodeRed,
		InputScope: executor.Scope{DeviceIDs: deviceIDs},
		Outputs: []executor.Capability{
			executor.CapabilityActionIntent,
			executor.CapabilityAlarmVerdict,
		},
		TargetScope:   executor.Scope{DeviceIDs: deviceIDs},
		MaxTargets:    5,
		MaxRatePerMin: 120,
	}
}

func (e *testEnv) registerExecutor(name string, contract executor.Contract) *ExecutorResponse {
	e.t.Helper()
	out, err := e.svc().RegisterExecutor(e.ctx(), &RegisterExecutorRequest{
		Name: name, Contract: contract,
	})
	if err != nil {
		e.t.Fatalf("register executor %q: %v", name, err)
	}
	return out
}

// visibleDevice re-registers one of this test's devices as visible to every
// tenant.
//
// newEnv gives its devices the test's own random tenant, which is right for the
// runtime-level tests. The service endpoints run under the fixed tenant the auth
// layer hands back, so without this the directory answers "no such device" and
// target resolution fails for a reason that has nothing to do with the rule
// under test.
func (e *testEnv) visibleDevice(id string) string {
	e.dir.devices[id] = ports.Device{
		ID: id, Name: "外部可见设备", DeviceConfigID: "cfg-1", Label: "floor-1",
		DeviceType: "sensor", Online: 1,
	}
	return id
}

// intentFor builds a well-formed intent. Seq starts at 1 for every executor
// because each test registers its own.
func (e *testEnv) intentFor(reg *ExecutorResponse, rule *store.Rule, deviceID, key string) *IntentRequest {
	return &IntentRequest{
		Epoch: reg.Epoch, Seq: 1, ContractHash: reg.ContractHash,
		IssuedAt: e.now, ExpiresAt: e.now.Add(2 * time.Minute),
		IdempotencyKey: key,
		RuleID:         rule.ID, Revision: rule.Revision, DeviceID: deviceID,
	}
}

func (e *testEnv) submitIntent(reg *ExecutorResponse, req *IntentRequest) *IntentResponse {
	e.t.Helper()
	res, err := e.svc().SubmitExecutorIntent(e.ctx(), reg.ID, req)
	if err != nil {
		e.t.Fatalf("submit intent: %v", err)
	}
	return res
}

// verdictFor builds a raw "the condition is true now" judgement, pinned to the
// revision and definition the executor read.
func (e *testEnv) verdictFor(reg *ExecutorResponse, policy *store.Rule, key string) *VerdictRequest {
	return &VerdictRequest{
		Epoch: reg.Epoch, Seq: 1, ContractHash: reg.ContractHash,
		IssuedAt: e.now, ExpiresAt: e.now.Add(2 * time.Minute),
		IdempotencyKey: key,
		PolicyID:       policy.ID, PolicyRevision: policy.Revision,
		DefinitionHash: policy.DefinitionHash,
		Mode:           string(executor.ModeRaw), Observed: true,
		SubjectID: e.dev1, ActionKind: "qualified",
	}
}

func (e *testEnv) submitVerdict(reg *ExecutorResponse, req *VerdictRequest) *VerdictResponse {
	e.t.Helper()
	res, err := e.svc().SubmitExecutorVerdict(e.ctx(), reg.ID, req)
	if err != nil {
		e.t.Fatalf("submit verdict: %v", err)
	}
	return res
}

// externalPolicy is an alarm policy that accepts the event channel from exactly
// one registered executor. hold is the published `for`, which is the value an
// external verdict must not be able to shorten.
func (e *testEnv) externalPolicy(tenant, executorID string, hold time.Duration) *store.Rule {
	e.t.Helper()
	def := domain.Definition{
		Kind:  domain.KindAlarm,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{
			For: domain.Duration(hold),
			Groups: []domain.Group{{Conditions: []domain.Condition{{
				Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
				Operator: domain.OpGt, Value: "30", Unit: "℃",
			}}}},
		},
		Recovery: &domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpLt, Value: "25", Unit: "℃",
		}}}}},
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{e.dev1}},
		Alarm: &domain.AlarmSpec{
			Key:            "policy.external." + e.dev1,
			Sources:        []domain.AlarmSource{domain.AlarmSourceEvent},
			AllowedSources: []string{executorID},
			Level:          "H",
		},
	}
	return e.createNamedRuleIn(tenant, "外部判定策略", def)
}

// hasField reports whether the wire details point at one input.
func hasField(details map[string]any, field string) bool {
	list, _ := details["fields"].([]any)
	for _, item := range list {
		if m, ok := item.(map[string]any); ok && m["field"] == field {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

// TestARegistrationWithNoTargetScopeIsRefusedWithTheField: the scope is a
// whitelist, so an empty one means "no device", and the caller has to be told
// which input produced that — not just that the registration failed.
func TestARegistrationWithNoTargetScopeIsRefusedWithTheField(t *testing.T) {
	e := newEnv(t)
	contract := executorContract(e.dev1)
	contract.TargetScope = executor.Scope{}

	_, err := e.svc().RegisterExecutor(e.ctx(), &RegisterExecutorRequest{
		Name: "空范围", Contract: contract,
	})
	if err == nil {
		t.Fatal("a registration that may not address any device must be refused")
	}
	if code := errs.Code(err); code != errs.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v (%v)", code, err)
	}
	details := wireDetail(t, err)
	if details["code"] != "executor_contract_invalid" {
		t.Fatalf("want executor_contract_invalid, got %v", details["code"])
	}
	if !hasField(details, "contract.target_scope") {
		t.Fatalf("the caller must be told which input is wrong, got %v", details["fields"])
	}
}

// TestTwoExecutorsCannotShareAName: the name is how an operator tells two
// deployments apart in the list, so a second one with the same name is refused
// instead of silently shadowing the first.
func TestTwoExecutorsCannotShareAName(t *testing.T) {
	e := newEnv(t)
	e.registerExecutor("同名", executorContract(e.dev1))

	_, err := e.svc().RegisterExecutor(e.ctx(), &RegisterExecutorRequest{
		Name: "同名", Contract: executorContract(e.dev2),
	})
	if err == nil {
		t.Fatal("a duplicate name must be refused")
	}
	if code := errs.Code(err); code != errs.AlreadyExists {
		t.Fatalf("want AlreadyExists, got %v (%v)", code, err)
	}
	if details := wireDetail(t, err); details["code"] != "duplicate_name" {
		t.Fatalf("want duplicate_name, got %v", details["code"])
	}
}

// TestTheExecutorListShowsTheContractAndItsLiveness: "registered" has to be
// visible, otherwise the operator cannot answer "which version is out there".
func TestTheExecutorListShowsTheContractAndItsLiveness(t *testing.T) {
	e := newEnv(t)
	reg := e.registerExecutor("可见性", executorContract(e.dev1))
	if reg.ContractHash == "" {
		t.Fatal("a registration must be pinned to a contract hash")
	}

	list, err := e.svc().ListExecutors(e.ctx(), &ListExecutorsRequest{Limit: 100})
	if err != nil {
		t.Fatalf("list executors: %v", err)
	}
	var found *ExecutorResponse
	for i := range list.Executors {
		if list.Executors[i].ID == reg.ID {
			found = &list.Executors[i]
			break
		}
	}
	if found == nil {
		t.Fatal("the executor just registered is not in the list")
	}
	if !strings.Contains(found.Summary, "action_intent") {
		t.Fatalf("the summary must name what it may produce, got %q", found.Summary)
	}
	if !found.Healthy {
		t.Fatal("registering is itself a sign of life; a brand new executor must not read as unhealthy")
	}

	hb, err := e.svc().HeartbeatExecutor(e.ctx(), reg.ID, &HeartbeatRequest{Epoch: reg.Epoch})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if hb.Epoch != reg.Epoch {
		t.Fatalf("the heartbeat must echo the epoch the platform holds, got %d want %d", hb.Epoch, reg.Epoch)
	}
}

// TestTheMessageLogShowsWhatTheEngineSentAndWhatWasDecided: "registered" is not
// evidence. The operator has to see the refused message too, because that is the
// one an engine will insist it never sent.
func TestTheMessageLogShowsWhatTheEngineSentAndWhatWasDecided(t *testing.T) {
	e := newEnv(t)
	dev := e.visibleDevice(e.dev1)
	rule := e.createNamedRuleIn(serviceTestTenant, "流水规则", e.baseDefinition())
	reg := e.registerExecutor("nodered-log", executorContract(dev))

	e.submitIntent(reg, e.intentFor(reg, rule, dev, "log-ok"))

	denied := e.intentFor(reg, rule, dev, "log-denied")
	denied.Seq = 2
	denied.DeviceID = e.dev2 // outside the registered scope
	if res := e.submitIntent(reg, denied); res.Accepted {
		t.Fatalf("the out-of-scope intent should have been refused, got %+v", res)
	}

	log, err := e.svc().ListExecutorMessages(e.ctx(), reg.ID, &ListExecutorMessagesRequest{Limit: 50})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if log.Total != 2 {
		t.Fatalf("want both messages on record, got %d", log.Total)
	}
	byKey := map[string]ExecutorMessageResponse{}
	for _, m := range log.Messages {
		byKey[m.IdempotencyKey] = m
	}
	accepted, ok := byKey["log-ok"]
	if !ok {
		t.Fatal("the accepted intent is missing from the log")
	}
	if accepted.Direction != "intent" || accepted.Outcome == "" {
		t.Fatalf("the accepted intent must record its direction and outcome, got %+v", accepted)
	}
	refused, ok := byKey["log-denied"]
	if !ok {
		t.Fatal("a refused message must stay on record; otherwise it looks like it was never sent")
	}
	if refused.Outcome != "denied" || refused.ReasonCode != "out_of_scope" {
		t.Fatalf("want denied/out_of_scope, got %q/%q", refused.Outcome, refused.ReasonCode)
	}
}

// ---------------------------------------------------------------------------
// Action intents
// ---------------------------------------------------------------------------

// TestAnExternalIntentRunsThePublishedRuleOnce: the intent names a rule, and the
// existing run/dispatch machinery does the rest — including reaching the command
// owner exactly once.
func TestAnExternalIntentRunsThePublishedRuleOnce(t *testing.T) {
	e := newEnv(t)
	rule := e.createNamedRuleIn(serviceTestTenant, "外部意图规则", e.baseDefinition())
	e.visibleDevice(e.dev1)
	reg := e.registerExecutor("nodered-intent", executorContract(e.dev1))

	res := e.submitIntent(reg, e.intentFor(reg, rule, e.dev1, "intent-ok-1"))
	if !res.Accepted {
		t.Fatalf("a valid intent must be accepted, got %+v", res)
	}
	if res.RunID == "" {
		t.Fatal("an accepted intent must name the run it created")
	}
	if res.Replayed {
		t.Fatal("the first submission is not a replay")
	}

	runs := e.runsIn(serviceTestTenant, rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want exactly one run, got %d", len(runs))
	}
	if runs[0].ID != res.RunID {
		t.Fatalf("the response names %q but the run is %q", res.RunID, runs[0].ID)
	}
	if got := e.cmds.count(); got != 1 {
		t.Fatalf("the intent must reach the existing command owner exactly once, got %d", got)
	}
}

// TestARetriedIntentIsTheSameRunAndNotASecondCommand: a transport retry keeps
// its key, and the answer is the original run rather than a second one.
func TestARetriedIntentIsTheSameRunAndNotASecondCommand(t *testing.T) {
	e := newEnv(t)
	rule := e.createNamedRuleIn(serviceTestTenant, "重投规则", e.baseDefinition())
	e.visibleDevice(e.dev1)
	reg := e.registerExecutor("nodered-retry", executorContract(e.dev1))

	first := e.submitIntent(reg, e.intentFor(reg, rule, e.dev1, "intent-retry"))
	second := e.submitIntent(reg, e.intentFor(reg, rule, e.dev1, "intent-retry"))

	if !second.Accepted || !second.Replayed {
		t.Fatalf("a retry with the same key must be answered as a replay, got %+v", second)
	}
	if second.RunID != first.RunID {
		t.Fatalf("a retry must name the same run: %q then %q", first.RunID, second.RunID)
	}
	if got := len(e.runsIn(serviceTestTenant, rule.ID)); got != 1 {
		t.Fatalf("a retry must not create a second run, got %d", got)
	}
	if got := e.cmds.count(); got != 1 {
		t.Fatalf("a retry must not dispatch a second command, got %d", got)
	}
}

// TestAReplayedSequenceDoesNotRunTheRuleAgain: the same message re-sent under a
// new key is still a replay, because the sequence did not move forward.
func TestAReplayedSequenceDoesNotRunTheRuleAgain(t *testing.T) {
	e := newEnv(t)
	rule := e.createNamedRuleIn(serviceTestTenant, "乱序规则", e.baseDefinition())
	e.visibleDevice(e.dev1)
	reg := e.registerExecutor("nodered-seq", executorContract(e.dev1))

	e.submitIntent(reg, e.intentFor(reg, rule, e.dev1, "seq-first"))

	replay := e.intentFor(reg, rule, e.dev1, "seq-second")
	replay.Seq = 1 // same number, different key
	res := e.submitIntent(reg, replay)
	if res.Accepted {
		t.Fatal("a sequence that did not move forward must not be accepted")
	}
	if res.ReasonCode != "replayed_sequence" {
		t.Fatalf("want replayed_sequence, got %q (%+v)", res.ReasonCode, res.Issues)
	}
	if got := len(e.runsIn(serviceTestTenant, rule.ID)); got != 1 {
		t.Fatalf("a replayed sequence must not create a second run, got %d", got)
	}
	if got := e.cmds.count(); got != 1 {
		t.Fatalf("a replayed sequence must not dispatch a second command, got %d", got)
	}
}

// TestAnIntentFromASupersededEpochIsRefused: re-registering with a narrower
// scope must take effect immediately, even though the old deployment is still
// sending. That is the whole reason the epoch exists.
func TestAnIntentFromASupersededEpochIsRefused(t *testing.T) {
	e := newEnv(t)
	rule := e.createNamedRuleIn(serviceTestTenant, "旧代际规则", e.baseDefinition())
	reg := e.registerExecutor("nodered-epoch", executorContract(e.dev1))

	narrowed := executorContract(e.dev2)
	if _, err := e.st.RebindExecutorContract(e.ctx(), serviceTestTenant, reg.ID, narrowed, narrowed.Hash()); err != nil {
		t.Fatalf("rebind: %v", err)
	}

	res := e.submitIntent(reg, e.intentFor(reg, rule, e.dev1, "epoch-old"))
	if res.Accepted {
		t.Fatal("a message from the previous registration generation must not be accepted")
	}
	if res.ReasonCode != "superseded_epoch" {
		t.Fatalf("want superseded_epoch, got %q (%+v)", res.ReasonCode, res.Issues)
	}
	if got := len(e.runsIn(serviceTestTenant, rule.ID)); got != 0 {
		t.Fatalf("a superseded epoch must not run anything, got %d runs", got)
	}
}

// TestAnIntentOutsideTheRegisteredScopeIsRefused: the device is real, the rule
// is real, but the contract does not cover that device.
func TestAnIntentOutsideTheRegisteredScopeIsRefused(t *testing.T) {
	e := newEnv(t)
	rule := e.createNamedRuleIn(serviceTestTenant, "越界规则", e.baseDefinition())
	reg := e.registerExecutor("nodered-scope", executorContract(e.dev1))

	res := e.submitIntent(reg, e.intentFor(reg, rule, e.dev2, "scope-1"))
	if res.Accepted {
		t.Fatal("a device outside the registered scope must not be addressable")
	}
	if res.ReasonCode != "out_of_scope" {
		t.Fatalf("want out_of_scope, got %q (%+v)", res.ReasonCode, res.Issues)
	}
	if got := len(e.runsIn(serviceTestTenant, rule.ID)); got != 0 {
		t.Fatalf("an out-of-scope intent must not run anything, got %d runs", got)
	}
}

// TestAnIntentAgainstADisabledRuleIsRefused: "停用" is the operator's stop
// button, and an external engine must not be a way around it.
func TestAnIntentAgainstADisabledRuleIsRefused(t *testing.T) {
	e := newEnv(t)
	rule := e.createNamedRuleIn(serviceTestTenant, "停用规则", e.baseDefinition())
	reg := e.registerExecutor("nodered-disabled", executorContract(e.dev1))

	if _, err := e.svc().SetRuleEnabled(e.ctx(), rule.ID, &SetEnabledRequest{
		Enabled: false, Version: rule.Version,
	}); err != nil {
		t.Fatalf("disable rule: %v", err)
	}

	res := e.submitIntent(reg, e.intentFor(reg, rule, e.dev1, "disabled-1"))
	if res.Accepted {
		t.Fatal("a disabled rule must not be runnable from outside")
	}
	if res.ReasonCode != "rule_disabled" {
		t.Fatalf("want rule_disabled, got %q (%+v)", res.ReasonCode, res.Issues)
	}
}

// TestAnIntentAgainstAnOlderRuleRevisionIsRefused: the engine read revision N,
// the operator published N+1. Running N would execute a rule that no longer
// exists as written.
func TestAnIntentAgainstAnOlderRuleRevisionIsRefused(t *testing.T) {
	e := newEnv(t)
	rule := e.createNamedRuleIn(serviceTestTenant, "旧版本规则", e.baseDefinition())
	reg := e.registerExecutor("nodered-rev", executorContract(e.dev1))

	req := e.intentFor(reg, rule, e.dev1, "revision-old")
	req.Revision = rule.Revision - 1
	res := e.submitIntent(reg, req)
	if res.Accepted {
		t.Fatal("an intent pinned to an older revision must not be accepted")
	}
	if res.ReasonCode != "stale_rule_revision" {
		t.Fatalf("want stale_rule_revision, got %q (%+v)", res.ReasonCode, res.Issues)
	}
}

// TestAnIntentCannotRunAlongsideANativeRun: the native trigger and the external
// intent share one run owner, so the rule's own mode decides what happens to the
// second one. The external path gets no private lane — that is what stops two
// paths from owning the same device.
func TestAnIntentCannotRunAlongsideANativeRun(t *testing.T) {
	e := newEnv(t)
	dev := e.visibleDevice(e.dev1)
	rule := e.createNamedRuleIn(serviceTestTenant, "原生占用规则", e.waitingDefinition(domain.ModeSingle))
	reg := e.registerExecutor("nodered-busy", executorContract(dev))

	// A native trigger opens a run that is waiting, so the device is occupied.
	e.ingestFor(serviceTestTenant, "ev-native-busy", dev, "temperature", 35.0)
	if runs := e.runsIn(serviceTestTenant, rule.ID); len(runs) != 1 {
		t.Fatalf("the native trigger must have started one run, got %d", len(runs))
	}
	before := e.cmds.count()

	res := e.submitIntent(reg, e.intentFor(reg, rule, dev, "intent-busy"))
	if !res.Accepted {
		t.Fatalf("the intent was admitted; the mode decides what happens next, got %+v", res)
	}
	if res.Status != runtime.StatusIgnored {
		t.Fatalf("want the second run ignored by single mode, got %q", res.Status)
	}
	if got := e.cmds.count(); got != before {
		t.Fatalf("an ignored run must not dispatch a command: %d commands, was %d", got, before)
	}
}

// ---------------------------------------------------------------------------
// Alarm verdicts
// ---------------------------------------------------------------------------

// TestAnExternalVerdictReachesTheAlarmOwner: a legal verdict goes through the
// same alarm owner the detector uses, opens exactly one alarm identity, and a
// retry is the same judgement rather than a second alarm.
func TestAnExternalVerdictReachesTheAlarmOwner(t *testing.T) {
	e := newEnv(t)
	reg := e.registerExecutor("nodered-alarm", executorContract(e.dev1))
	policy := e.externalPolicy(serviceTestTenant, reg.ID, 0)

	res := e.submitVerdict(reg, e.verdictFor(reg, policy, "verdict-ok-1"))
	if !res.Accepted {
		t.Fatalf("a legal verdict must be accepted, got %+v", res)
	}
	if res.Outcome != "qualified" {
		t.Fatalf("want qualified, got %q (%+v)", res.Outcome, res.Issues)
	}
	if res.EpisodeID == "" {
		t.Fatal("an accepted qualified verdict must open an alarm identity")
	}
	if got := e.activeEpisodes(policy.PolicyKey()); got != 1 {
		t.Fatalf("want exactly one active alarm, got %d", got)
	}

	again := e.submitVerdict(reg, e.verdictFor(reg, policy, "verdict-ok-1"))
	if !again.Accepted || !again.Replayed {
		t.Fatalf("a retried verdict must be answered as a replay, got %+v", again)
	}
	if got := e.activeEpisodes(policy.PolicyKey()); got != 1 {
		t.Fatalf("a retried verdict must not open a second alarm, got %d", got)
	}
}

// TestAVerdictAgainstAnOlderPolicyRevisionIsRefused: this is the check that
// stops an external job deployed against last week's threshold from raising an
// alarm today.
func TestAVerdictAgainstAnOlderPolicyRevisionIsRefused(t *testing.T) {
	e := newEnv(t)
	reg := e.registerExecutor("nodered-stale", executorContract(e.dev1))
	policy := e.externalPolicy(serviceTestTenant, reg.ID, 0)

	req := e.verdictFor(reg, policy, "verdict-stale")
	req.PolicyRevision = policy.Revision - 1
	res := e.submitVerdict(reg, req)
	if res.Accepted {
		t.Fatal("a verdict computed from an older revision must not be accepted")
	}
	if res.ReasonCode != "stale_policy_revision" {
		t.Fatalf("want stale_policy_revision, got %q (%+v)", res.ReasonCode, res.Issues)
	}
	if got := e.activeEpisodes(policy.PolicyKey()); got != 0 {
		t.Fatalf("a stale verdict must not open an alarm, got %d", got)
	}
}

// TestARawObservationCannotClaimThePublishedHold: "the value is above 30 now" is
// not "it has been above 30 for five minutes", and the platform's published
// duration is the only thing allowed to decide the second one.
func TestARawObservationCannotClaimThePublishedHold(t *testing.T) {
	e := newEnv(t)
	reg := e.registerExecutor("nodered-raw", executorContract(e.dev1))
	policy := e.externalPolicy(serviceTestTenant, reg.ID, 5*time.Minute)

	// verdictFor is raw + action_kind=qualified: exactly the mix-up this rule is
	// about.
	res := e.submitVerdict(reg, e.verdictFor(reg, policy, "verdict-raw-hold"))
	if res.Accepted {
		t.Fatal("a raw observation must not qualify a policy that requires a hold")
	}
	if res.ReasonCode != "raw_cannot_qualify" {
		t.Fatalf("want raw_cannot_qualify, got %q (%+v)", res.ReasonCode, res.Issues)
	}
	if got := e.activeEpisodes(policy.PolicyKey()); got != 0 {
		t.Fatalf("no alarm may be opened from a raw observation, got %d", got)
	}
}

// TestAQualifiedVerdictShorterThanThePublishedHoldIsRefused: an engine that does
// claim a duration has to show it, and the platform recomputes it instead of
// trusting the label.
func TestAQualifiedVerdictShorterThanThePublishedHoldIsRefused(t *testing.T) {
	e := newEnv(t)
	reg := e.registerExecutor("nodered-short", executorContract(e.dev1))
	policy := e.externalPolicy(serviceTestTenant, reg.ID, 5*time.Minute)

	req := e.verdictFor(reg, policy, "verdict-short")
	req.Mode = string(executor.ModeQualified)
	since := e.now.Add(-2 * time.Minute)
	req.Since = &since

	res := e.submitVerdict(reg, req)
	if res.Accepted {
		t.Fatal("two minutes of evidence must not satisfy a five minute hold")
	}
	if res.ReasonCode != "hold_not_satisfied" {
		t.Fatalf("want hold_not_satisfied, got %q (%+v)", res.ReasonCode, res.Issues)
	}
	if got := e.activeEpisodes(policy.PolicyKey()); got != 0 {
		t.Fatalf("no alarm may be opened from a short hold, got %d", got)
	}
}

// TestAVerdictFromADisabledExecutorIsRefused: disabling an executor is the
// operator's kill switch, and it has to work without re-registering anything.
func TestAVerdictFromADisabledExecutorIsRefused(t *testing.T) {
	e := newEnv(t)
	reg := e.registerExecutor("nodered-off", executorContract(e.dev1))
	policy := e.externalPolicy(serviceTestTenant, reg.ID, 0)

	if err := e.st.SetExecutorEnabled(e.ctx(), serviceTestTenant, reg.ID, false); err != nil {
		t.Fatalf("disable executor: %v", err)
	}

	res := e.submitVerdict(reg, e.verdictFor(reg, policy, "verdict-disabled"))
	if res.Accepted {
		t.Fatal("a disabled executor must not be able to submit anything")
	}
	if res.ReasonCode != "executor_disabled" {
		t.Fatalf("want executor_disabled, got %q (%+v)", res.ReasonCode, res.Issues)
	}
}

// TestAVerdictForANonAlarmRuleIsRefusedCleanly: naming a scene as the policy is
// a caller mistake, and it must read as "no such alarm policy" rather than as an
// internal failure the operator has to investigate.
func TestAVerdictForANonAlarmRuleIsRefusedCleanly(t *testing.T) {
	e := newEnv(t)
	reg := e.registerExecutor("nodered-wrongrule", executorContract(e.dev1))
	scene := e.createNamedRuleIn(serviceTestTenant, "不是告警", e.baseDefinition())

	req := e.verdictFor(reg, scene, "verdict-wrong-kind")
	req.ActionKind = "qualified"
	res := e.submitVerdict(reg, req)
	if res.Accepted {
		t.Fatal("a scene is not an alarm policy and cannot be qualified")
	}
	if res.ReasonCode != "policy_missing" {
		t.Fatalf("want policy_missing, got %q (%+v)", res.ReasonCode, res.Issues)
	}
}
