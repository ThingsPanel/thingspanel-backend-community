package executor

import (
	"testing"
	"time"
)

var now = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func validContract() Contract {
	return Contract{
		Engine:        EngineNodeRed,
		InputScope:    Scope{DeviceIDs: []string{"dev-1"}},
		Outputs:       []Capability{CapabilityActionIntent, CapabilityAlarmVerdict},
		TargetScope:   Scope{DeviceIDs: []string{"dev-1", "dev-2"}},
		MaxTargets:    10,
		MaxRatePerMin: 60,
	}
}

func validRegistration(c Contract) Registration {
	return Registration{ID: "exe-1", Epoch: 3, Contract: c, ContractHash: c.Hash(), Enabled: true, LastSeq: 100}
}

func codes(issues []Issue) map[string]bool {
	out := map[string]bool{}
	for _, issue := range issues {
		out[issue.Code] = true
	}
	return out
}

func validEnvelope() Envelope {
	return Envelope{
		ExecutorID:     "exe-1",
		Epoch:          3,
		Seq:            101,
		IssuedAt:       now,
		ExpiresAt:      now.Add(2 * time.Minute),
		IdempotencyKey: "intent-1",
	}
}

func validPolicy() PolicyRef {
	return PolicyRef{
		Revision:       7,
		DefinitionHash: "abc123",
		ForSeconds:     300,
		Enabled:        true,
		AllowsEvent:    true,
		SourceAllowed:  true,
	}
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestAWellFormedContractRegisters(t *testing.T) {
	if issues := validContract().Validate(); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestAnEmptyScopeFailsClosed(t *testing.T) {
	c := validContract()
	c.TargetScope = Scope{}
	got := codes(c.Validate())
	if !got["empty_scope"] {
		t.Fatalf("an empty target scope must be refused, got %v", got)
	}
}

func TestAnUnknownEngineOrCapabilityIsRefused(t *testing.T) {
	c := validContract()
	c.Engine = "kubernetes"
	c.Outputs = []Capability{"shell"}
	got := codes(c.Validate())
	if !got["unsupported_engine"] || !got["unsupported_capability"] {
		t.Fatalf("expected both to be refused, got %v", got)
	}
}

func TestDeclaringTheSameCapabilityTwiceIsRefused(t *testing.T) {
	c := validContract()
	c.Outputs = []Capability{CapabilityActionIntent, CapabilityActionIntent}
	if got := codes(c.Validate()); !got["duplicate_capability"] {
		t.Fatalf("expected duplicate_capability, got %v", got)
	}
}

func TestTheContractHashIgnoresDeclarationOrder(t *testing.T) {
	a := validContract()
	b := validContract()
	b.Outputs = []Capability{CapabilityAlarmVerdict, CapabilityActionIntent}
	b.TargetScope = Scope{DeviceIDs: []string{"dev-2", "dev-1"}}
	if a.Hash() != b.Hash() {
		t.Fatalf("the same contract must hash the same: %s vs %s", a.Hash(), b.Hash())
	}
}

func TestTheContractHashChangesWhenTheScopeWidens(t *testing.T) {
	a := validContract()
	b := validContract()
	b.TargetScope = Scope{DeviceIDs: []string{"dev-1", "dev-2", "dev-3"}}
	if a.Hash() == b.Hash() {
		t.Fatal("widening the target scope must change the contract identity")
	}
}

// ---------------------------------------------------------------------------
// Envelope
// ---------------------------------------------------------------------------

func TestAReplayedSequenceIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	env := validEnvelope()
	env.Seq = reg.LastSeq
	if got := codes(env.Validate(now, reg)); !got["replayed_sequence"] {
		t.Fatalf("expected replayed_sequence, got %v", got)
	}
}

func TestAMessageFromASupersededEpochIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	env := validEnvelope()
	env.Epoch = 2
	if got := codes(env.Validate(now, reg)); !got["superseded_epoch"] {
		t.Fatalf("expected superseded_epoch, got %v", got)
	}
}

func TestAnExpiredMessageIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	env := validEnvelope()
	env.ExpiresAt = now.Add(-time.Second)
	if got := codes(env.Validate(now, reg)); !got["expired"] {
		t.Fatalf("expected expired, got %v", got)
	}
}

func TestAMessageWithoutAnExpiryIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	env := validEnvelope()
	env.ExpiresAt = time.Time{}
	if got := codes(env.Validate(now, reg)); !got["required"] {
		t.Fatalf("expected a required-field issue, got %v", got)
	}
}

func TestAMessageWithoutAnIdempotencyKeyIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	env := validEnvelope()
	env.IdempotencyKey = "  "
	if got := codes(env.Validate(now, reg)); !got["required"] {
		t.Fatalf("expected a required-field issue, got %v", got)
	}
}

func TestAMessageFromADisabledExecutorIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	reg.Enabled = false
	if got := codes(validEnvelope().Validate(now, reg)); !got["executor_disabled"] {
		t.Fatalf("expected executor_disabled, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Verdicts
// ---------------------------------------------------------------------------

func qualifiedVerdict(since time.Time) Verdict {
	v := Verdict{
		Envelope:       validEnvelope(),
		PolicyID:       "pol-1",
		PolicyRevision: 7,
		DefinitionHash: "abc123",
		Mode:           ModeQualified,
		Observed:       true,
		Since:          &since,
		SubjectID:      "dev-1",
		ActionKind:     "qualified",
	}
	return v
}

func TestAQualifiedVerdictThatSatisfiesThePublishedHoldIsAccepted(t *testing.T) {
	reg := validRegistration(validContract())
	since := now.Add(-6 * time.Minute)
	if issues := qualifiedVerdict(since).Validate(now, reg, validPolicy()); len(issues) != 0 {
		t.Fatalf("expected acceptance, got %v", issues)
	}
}

func TestAQualifiedVerdictCannotShortenThePublishedHold(t *testing.T) {
	reg := validRegistration(validContract())
	// The engine claims "qualified" after only 30 seconds, while the published
	// duration is 300. The platform's number wins.
	since := now.Add(-30 * time.Second)
	got := codes(qualifiedVerdict(since).Validate(now, reg, validPolicy()))
	if !got["hold_not_satisfied"] {
		t.Fatalf("expected hold_not_satisfied, got %v", got)
	}
}

func TestAQualifiedVerdictWithoutEvidenceIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	v := qualifiedVerdict(now)
	v.Since = nil
	if got := codes(v.Validate(now, reg, validPolicy())); !got["qualified_without_evidence"] {
		t.Fatalf("expected qualified_without_evidence, got %v", got)
	}
}

func TestARawObservationCannotStandInForAHold(t *testing.T) {
	reg := validRegistration(validContract())
	v := qualifiedVerdict(now)
	v.Mode = ModeRaw
	v.Since = nil
	got := codes(v.Validate(now, reg, validPolicy()))
	if !got["raw_cannot_qualify"] {
		t.Fatalf("expected raw_cannot_qualify, got %v", got)
	}
}

func TestARawObservationWithASinceIsAmbiguousAndRefused(t *testing.T) {
	reg := validRegistration(validContract())
	since := now.Add(-10 * time.Minute)
	v := qualifiedVerdict(since)
	v.Mode = ModeRaw
	got := codes(v.Validate(now, reg, validPolicy()))
	if !got["mixed_verdict_semantics"] {
		t.Fatalf("expected mixed_verdict_semantics, got %v", got)
	}
}

func TestARawObservationIsEnoughWhenThePolicyHasNoHold(t *testing.T) {
	reg := validRegistration(validContract())
	policy := validPolicy()
	policy.ForSeconds = 0
	v := qualifiedVerdict(now)
	v.Mode = ModeRaw
	v.Since = nil
	if issues := v.Validate(now, reg, policy); len(issues) != 0 {
		t.Fatalf("a policy without a hold must accept a raw observation, got %v", issues)
	}
}

func TestAVerdictAgainstAnOlderPolicyRevisionIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	v := qualifiedVerdict(now.Add(-6 * time.Minute))
	v.PolicyRevision = 6
	if got := codes(v.Validate(now, reg, validPolicy())); !got["stale_policy_revision"] {
		t.Fatalf("expected stale_policy_revision, got %v", got)
	}
}

func TestAVerdictWithADifferentDefinitionHashIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	v := qualifiedVerdict(now.Add(-6 * time.Minute))
	v.DefinitionHash = "def456"
	if got := codes(v.Validate(now, reg, validPolicy())); !got["stale_policy_definition"] {
		t.Fatalf("expected stale_policy_definition, got %v", got)
	}
}

func TestAVerdictForAPolicyThatDoesNotAllowEventsIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	policy := validPolicy()
	policy.AllowsEvent = false
	if got := codes(qualifiedVerdict(now.Add(-6*time.Minute)).Validate(now, reg, policy)); !got["policy_rejects_event"] {
		t.Fatalf("expected policy_rejects_event, got %v", got)
	}
}

func TestAVerdictFromAnUnlistedSourceIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	policy := validPolicy()
	policy.SourceAllowed = false
	if got := codes(qualifiedVerdict(now.Add(-6*time.Minute)).Validate(now, reg, policy)); !got["source_denied"] {
		t.Fatalf("expected source_denied, got %v", got)
	}
}

func TestAnExecutorWithoutTheVerdictCapabilityIsRefused(t *testing.T) {
	c := validContract()
	c.Outputs = []Capability{CapabilityActionIntent}
	reg := validRegistration(c)
	reg.ContractHash = c.Hash()
	if got := codes(qualifiedVerdict(now.Add(-6*time.Minute)).Validate(now, reg, validPolicy())); !got["capability_denied"] {
		t.Fatalf("expected capability_denied, got %v", got)
	}
}

func TestAVerdictWithAnUnknownActionKindIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	v := qualifiedVerdict(now.Add(-6 * time.Minute))
	v.ActionKind = "maybe"
	if got := codes(v.Validate(now, reg, validPolicy())); !got["unsupported_kind"] {
		t.Fatalf("expected unsupported_kind, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Action intents
// ---------------------------------------------------------------------------

func validIntent() Intent {
	return Intent{
		Envelope: validEnvelope(),
		RuleID:   "rule-1",
		Revision: 4,
		DeviceID: "dev-1",
	}
}

func TestAnIntentInsideTheScopeIsAccepted(t *testing.T) {
	reg := validRegistration(validContract())
	rule := RuleRef{ID: "rule-1", Revision: 4, Enabled: true}
	if issues := validIntent().Validate(now, reg, rule, "", nil); len(issues) != 0 {
		t.Fatalf("expected acceptance, got %v", issues)
	}
}

func TestAnIntentOutsideTheTargetScopeIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	rule := RuleRef{ID: "rule-1", Revision: 4, Enabled: true}
	in := validIntent()
	in.DeviceID = "dev-9"
	got := codes(in.Validate(now, reg, rule, "", nil))
	if !got["out_of_scope"] {
		t.Fatalf("expected out_of_scope, got %v", got)
	}
}

func TestAnIntentForADisabledRuleIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	rule := RuleRef{ID: "rule-1", Revision: 4, Enabled: false}
	if got := codes(validIntent().Validate(now, reg, rule, "", nil)); !got["rule_disabled"] {
		t.Fatalf("expected rule_disabled, got %v", got)
	}
}

func TestAnIntentAgainstAnOlderRuleRevisionIsRefused(t *testing.T) {
	reg := validRegistration(validContract())
	rule := RuleRef{ID: "rule-1", Revision: 5, Enabled: true}
	if got := codes(validIntent().Validate(now, reg, rule, "", nil)); !got["stale_rule_revision"] {
		t.Fatalf("expected stale_rule_revision, got %v", got)
	}
}

func TestAnIntentTouchingTooManyDevicesIsRefused(t *testing.T) {
	c := validContract()
	c.MaxTargets = 2
	reg := validRegistration(c)
	reg.ContractHash = c.Hash()
	rule := RuleRef{ID: "rule-1", Revision: 4, Enabled: true}
	in := validIntent()
	in.Targets = []string{"dev-1", "dev-2", "dev-1"}
	got := codes(in.Validate(now, reg, rule, "", nil))
	if !got["too_many_targets"] {
		t.Fatalf("expected too_many_targets, got %v", got)
	}
}

func TestAnExecutorWithoutTheIntentCapabilityIsRefused(t *testing.T) {
	c := validContract()
	c.Outputs = []Capability{CapabilityAlarmVerdict}
	reg := validRegistration(c)
	reg.ContractHash = c.Hash()
	rule := RuleRef{ID: "rule-1", Revision: 4, Enabled: true}
	if got := codes(validIntent().Validate(now, reg, rule, "", nil)); !got["capability_denied"] {
		t.Fatalf("expected capability_denied, got %v", got)
	}
}

func TestALabelScopeAdmitsOnlyDevicesCarryingEveryLabel(t *testing.T) {
	scope := Scope{Labels: []string{"line-a", "critical"}}
	if !scope.Allows("dev-x", "", []string{"critical", "line-a", "new"}) {
		t.Fatal("a device with both labels must be allowed")
	}
	if scope.Allows("dev-x", "", []string{"line-a"}) {
		t.Fatal("a device missing one label must not be allowed")
	}
	if scope.Allows("dev-x", "", nil) {
		t.Fatal("a device with no labels must not be allowed by a label scope")
	}
}

func TestADeviceConfigScopeAdmitsEveryDeviceOfThatTemplate(t *testing.T) {
	scope := Scope{DeviceConfigIDs: []string{"cfg-1"}}
	if !scope.Allows("dev-x", "cfg-1", nil) {
		t.Fatal("a device of the template must be allowed")
	}
	if scope.Allows("dev-x", "cfg-2", nil) {
		t.Fatal("a device of another template must not be allowed")
	}
}
