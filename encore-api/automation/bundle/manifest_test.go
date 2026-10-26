package bundle

import (
	"strings"
	"testing"
	"time"

	"encore.app/automation/domain"
)

// ---------------------------------------------------------------------------
// A21 unit tests: the manifest as a value
// ---------------------------------------------------------------------------
//
// These do not touch the database. They pin down the three properties the
// service layer relies on: the hash is stable, the security promise is checked
// rather than asserted, and a reference survives a round trip through the
// package form.

func baseDef() domain.Definition {
	d := domain.Definition{
		Kind:  domain.KindAutomation,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "30", Unit: "℃",
		}}}}},
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute), MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{"dev:role-a"}},
		Steps: []domain.Step{{
			Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1",
		}},
	}
	d.Normalize()
	return d
}

func manifestWith(rules ...Rule) *Manifest {
	m := &Manifest{
		ContractVersion: ContractVersion,
		BundleKey:       "b", Version: "1.0.0",
		Resources: Resources{Rules: rules},
	}
	m.ComputeHash()
	return m
}

func oneRule(key string, def domain.Definition) Rule {
	return Rule{ResourceKey: key, Name: "规则", Kind: def.Kind, Revision: 1,
		Definition: def, Hash: Hash(def)}
}

func hasCode(issues []domain.Issue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// TestTheSameConfigurationProducesTheSameHash: two exports of the same rules
// must be byte-identical, otherwise "already imported" never matches.
func TestTheSameConfigurationProducesTheSameHash(t *testing.T) {
	def := baseDef()
	a := manifestWith(oneRule("rule:a", def), oneRule("rule:b", def))
	b := manifestWith(oneRule("rule:b", def), oneRule("rule:a", def))
	a.Sort()
	b.Sort()
	a.ComputeHash()
	b.ComputeHash()
	if a.ContentHash != b.ContentHash {
		t.Fatalf("order changed the hash: %s vs %s", a.ContentHash, b.ContentHash)
	}

	c := manifestWith(oneRule("rule:a", def), oneRule("rule:b", def))
	c.Sort()
	c.ComputeHash()
	if a.ContentHash != c.ContentHash {
		t.Fatalf("two exports of the same rules disagree: %s vs %s", a.ContentHash, c.ContentHash)
	}
}

// TestMetadataDoesNotChangeTheContentHash: fixing a typo in a description is not
// a new version of the configuration.
func TestMetadataDoesNotChangeTheContentHash(t *testing.T) {
	def := baseDef()
	a := manifestWith(oneRule("rule:a", def))
	b := manifestWith(oneRule("rule:a", def))
	b.Version = "9.9.9"
	b.BundleKey = "renamed"
	b.ComputeHash()
	if a.ContentHash != b.ContentHash {
		t.Fatalf("metadata leaked into the content hash: %s vs %s", a.ContentHash, b.ContentHash)
	}
}

// TestAnUndeclaredRoleIsRefused: the package has to say what it needs.
func TestAnUndeclaredRoleIsRefused(t *testing.T) {
	def := baseDef()
	r := oneRule("rule:a", def)
	r.Bindings = []Binding{{ScopeRole: "dev:ghost"}}
	m := manifestWith(r)
	if issues := Validate(m); !hasCode(issues, "undeclared_role") {
		t.Fatalf("want undeclared_role, got %v", issues)
	}
}

// TestAnInternalReferenceThatDoesNotCloseIsRefused: a step pointing outside the
// package becomes a step that can never succeed.
func TestAnInternalReferenceThatDoesNotCloseIsRefused(t *testing.T) {
	def := baseDef()
	def.Steps = []domain.Step{{Kind: domain.StepActivateScene, Value: "rule:missing"}}
	def.Normalize()
	m := manifestWith(oneRule("rule:a", def))
	if issues := Validate(m); !hasCode(issues, "unresolved_reference") {
		t.Fatalf("want unresolved_reference, got %v", issues)
	}
}

// TestTheSecurityPromiseIsCheckedNotAsserted: a definition that carries a
// webhook token must be caught even though no field says "secret".
func TestTheSecurityPromiseIsCheckedNotAsserted(t *testing.T) {
	def := baseDef()
	def.Steps[0].Value = "https://hooks.example.com/x?access_key=AKIA123"
	m := manifestWith(oneRule("rule:a", def))
	if issues := Validate(m); !hasCode(issues, "secret_in_definition") {
		t.Fatalf("want secret_in_definition, got %v", issues)
	}
}

// TestATelemetryKeyIsNotASecret: the scan must not refuse the word "key".
func TestATelemetryKeyIsNotASecret(t *testing.T) {
	def := baseDef()
	def.Steps[0].Identifier = "key"
	def.Steps[0].Value = "on"
	m := manifestWith(oneRule("rule:a", def))
	if issues := Validate(m); hasCode(issues, "secret_in_definition") {
		t.Fatalf("a telemetry key must not be read as a credential: %v", issues)
	}
}

// TestARequiredSlotBlocksAndAnOptionalOneDoesNot: choosing not to notify is a
// decision; forgetting to choose is not.
func TestARequiredSlotBlocksAndAnOptionalOneDoesNot(t *testing.T) {
	def := baseDef()
	def.Steps = []domain.Step{{Kind: domain.StepNotify,
		NotificationPolicyID: SlotValue("oncall"), Value: "高温"}}
	m := manifestWith(oneRule("rule:a", def))
	m.Resources.NotifySlots = []NotifySlot{
		{SlotKey: "oncall", Label: "值班群", Required: true},
		{SlotKey: "audit", Label: "审计", Required: false},
	}
	if issues := Validate(m); len(issues) > 0 {
		t.Fatalf("the manifest itself is well formed: %v", issues)
	}

	plan := Preview(m, &Target{})
	if plan.OK {
		t.Fatal("an unanswered required slot must block the import")
	}
	if len(plan.MissingSlots) != 1 || plan.MissingSlots[0] != "oncall" {
		t.Fatalf("want oncall missing, got %v", plan.MissingSlots)
	}
	if len(plan.DeclinedSlots) != 1 || plan.DeclinedSlots[0] != "audit" {
		t.Fatalf("an unanswered optional slot is a decline, got %v", plan.DeclinedSlots)
	}

	ok := Preview(m, &Target{Slots: map[string]string{"oncall": "pol-1"}})
	if !ok.OK {
		t.Fatalf("choosing the required slot must unblock it: %v", ok.Issues)
	}
}

// TestDecliningANotifySlotRemovesTheStep: a notify step with no target has
// nothing left to do, and leaving it would fail validation later.
func TestDecliningANotifySlotRemovesTheStep(t *testing.T) {
	def := baseDef()
	def.Steps = []domain.Step{
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
		{Kind: domain.StepNotify, NotificationPolicyID: SlotValue("oncall"), Value: "高温"},
	}
	out := RewriteSlots(def, func(v string) string { return "" })
	if len(out.Steps) != 1 || out.Steps[0].Kind != domain.StepAction {
		t.Fatalf("the notify step should be gone, got %+v", out.Steps)
	}
	if err := domain.Validate(&out); err != nil {
		t.Fatalf("the result must still publish: %v", err)
	}
}

// TestReferencesSurviveARoundTrip: export rewrites ids to keys, import rewrites
// keys to ids, and the body in between must be identical to what came out.
func TestReferencesSurviveARoundTrip(t *testing.T) {
	def := baseDef()
	def.Steps = []domain.Step{
		{Kind: domain.StepAlarm, AlarmPolicyID: "row-id-1", DeviceRef: "target", Value: "qualified"},
		{Kind: domain.StepActivateScene, Value: "row-id-2"},
	}
	toKey := map[string]string{"row-id-1": "rule:alarm", "row-id-2": "rule:scene"}
	exported := RewriteRefs(def, func(id string) string { return toKey[id] })
	got := DefinitionRefs(exported)
	if len(got) != 2 || got[0].To != "rule:alarm" || got[1].To != "rule:scene" {
		t.Fatalf("export did not rewrite every reference: %+v", got)
	}

	toID := map[string]string{"rule:alarm": "local-1", "rule:scene": "local-2"}
	imported := RewriteRefs(exported, func(k string) string { return toID[k] })
	if imported.Steps[0].AlarmPolicyID != "local-1" || imported.Steps[1].Value != "local-2" {
		t.Fatalf("import did not restore the references: %+v", imported.Steps)
	}
}

// TestAnUnknownContractVersionIsRefused: a package written by a newer build must
// be rejected, not half-understood.
func TestAnUnknownContractVersionIsRefused(t *testing.T) {
	m := manifestWith(oneRule("rule:a", baseDef()))
	m.ContractVersion = "99"
	if issues := Validate(m); !hasCode(issues, "unsupported_contract") {
		t.Fatalf("want unsupported_contract, got %v", issues)
	}
}

// TestAResourceKeyIsRequiredAndUnique: two rules with the same key would make
// the second import silently overwrite the first.
func TestAResourceKeyIsRequiredAndUnique(t *testing.T) {
	def := baseDef()
	m := manifestWith(oneRule("rule:same", def), oneRule("rule:same", def))
	if issues := Validate(m); !hasCode(issues, "duplicate_resource_key") {
		t.Fatalf("want duplicate_resource_key, got %v", issues)
	}

	m2 := manifestWith(oneRule("", def))
	if issues := Validate(m2); !hasCode(issues, "missing_resource_key") {
		t.Fatalf("want missing_resource_key, got %v", issues)
	}
}

// TestAPreviewWithoutAnExistingRuleCreates: the baseline action.
func TestAPreviewWithoutAnExistingRuleCreates(t *testing.T) {
	def := baseDef()
	m := manifestWith(oneRule("rule:a", def))
	plan := Preview(m, &Target{})
	if !plan.OK || plan.CreateCount != 1 || plan.UpdateCount != 0 {
		t.Fatalf("want one create and no issues, got %+v (%v)", plan, plan.Issues)
	}
	if plan.Resources[0].Action != ActionCreate {
		t.Fatalf("want create, got %q", plan.Resources[0].Action)
	}
}

// TestAPreviewWithTheSameHashSkipsAndWithADifferentOneUpdates.
func TestAPreviewWithTheSameHashSkipsAndWithADifferentOneUpdates(t *testing.T) {
	def := baseDef()
	r := oneRule("rule:a", def)
	m := manifestWith(r)

	same := Preview(m, &Target{Existing: map[string]*Existing{
		"rule:a": {RuleID: "local-1", Hash: r.Hash},
	}})
	if same.SkipCount != 1 || same.UpdateCount != 0 {
		t.Fatalf("an identical rule must be skipped: %+v", same.Resources)
	}

	differ := Preview(m, &Target{Existing: map[string]*Existing{
		"rule:a": {RuleID: "local-1", Hash: "sha256:other"},
	}})
	if differ.UpdateCount != 1 || differ.SkipCount != 0 {
		t.Fatalf("a changed rule must be updated: %+v", differ.Resources)
	}
}

// TestAnUnprovenCapabilityIsNotARefusal: refusing on knowledge we do not have is
// how a good package gets rejected by a working environment.
func TestAnUnprovenCapabilityIsNotARefusal(t *testing.T) {
	def := baseDef()
	m := manifestWith(oneRule("rule:a", def))
	m.Dependencies.Roles = []Role{{Key: "dev:role-a", Kind: RoleDevice, Required: true}}
	m.Dependencies.Capabilities = []CapabilityDep{{RoleKey: "dev:role-a", Capability: "switch"}}

	unknown := Preview(m, &Target{Roles: map[string][]string{"dev:role-a": {"d1"}}})
	if !unknown.OK {
		t.Fatalf("with no capability source the plan must not refuse: %v", unknown.Issues)
	}
	proven := Preview(m, &Target{
		Roles: map[string][]string{"dev:role-a": {"d1"}},
		CanDo: func(string, string) bool { return false },
	})
	if proven.OK || !hasCode(proven.Issues, "capability_missing") {
		t.Fatalf("a device proven unable must block: %v", proven.Issues)
	}
}

// TestASlotReferenceIsRecognisable keeps the prefix honest: a stored definition
// that still holds a slot has to be findable, or an import can publish a rule
// pointing at a policy that does not exist.
func TestASlotReferenceIsRecognisable(t *testing.T) {
	if k, ok := SlotOf(SlotValue("oncall")); !ok || k != "oncall" {
		t.Fatalf("slot reference not recognised: %q %v", k, ok)
	}
	if _, ok := SlotOf("pol-real"); ok {
		t.Fatal("a real policy id must not look like a slot")
	}
	def := baseDef()
	def.Steps = []domain.Step{{Kind: domain.StepNotify,
		NotificationPolicyID: SlotValue("oncall"), Value: "x"}}
	def.Alarm = &domain.AlarmSpec{NotifyPolicyID: SlotValue("audit")}
	slots := DefinitionSlots(def)
	if strings.Join(slots, ",") != "audit,oncall" {
		t.Fatalf("want audit,oncall got %v", slots)
	}
}
