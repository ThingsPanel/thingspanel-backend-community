package automation

import (
	"errors"
	"testing"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A30: the old id has to resolve to exactly one rule, or say why it does not
// ---------------------------------------------------------------------------
//
// The compat adapter reads a mapping it did not write. Two failure modes matter
// and neither is visible from the adapter's side:
//
//   - the old id is not there yet (migration has not run for this tenant);
//   - the old id is there twice (a migration defect, or a re-import).
//
// The first must be a clear "not migrated", the second must be reported as
// ambiguous. Picking the earliest row silently — which is what the migration
// helper does on purpose — turns "activate the hallway scene" into "run some
// other rule" the moment the mapping is not one-to-one.

// ruleWithLegacy creates a stored rule carrying the id it had in the old console.
func (e *testEnv) ruleWithLegacy(name, legacyID string, kind domain.RuleKind) *store.Rule {
	e.t.Helper()
	def := domain.Definition{
		Kind:  kind,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeRule,
		Targets: domain.TargetSpec{
			DeviceIDs: []string{e.dev1},
		},
		Steps: []domain.Step{{
			Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1",
		}},
	}
	if kind == domain.KindAutomation {
		def.Trigger = domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "30",
		}}}}}
	}
	def.Normalize()
	if err := domain.Validate(&def); err != nil {
		e.t.Fatalf("test rule %q is invalid: %v", name, err)
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: serviceTestTenant, Name: name, Kind: def.Kind,
		Enabled: true, Definition: def, DefinitionHash: domain.DefinitionHash(def),
		LegacyID: legacyID,
	}
	if err := e.st.CreateRule(e.ctx(), rule); err != nil {
		e.t.Fatalf("create rule %q: %v", name, err)
	}
	stored, err := e.st.GetRule(e.ctx(), serviceTestTenant, rule.ID)
	if err != nil {
		e.t.Fatalf("reload rule %q: %v", name, err)
	}
	return stored
}

func TestALegacyIdResolvesToTheRuleItWasMigratedInto(t *testing.T) {
	e := newEnv(t)
	rule := e.ruleWithLegacy("门厅灯光", "legacy-scene-a30", domain.KindScene)

	ref, err := e.svc().ResolveLegacyRef(e.ctx(), "legacy-scene-a30")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ref.RuleID != rule.ID {
		t.Fatalf("resolved to %q, want %q", ref.RuleID, rule.ID)
	}
	if ref.Ambiguous {
		t.Fatal("one mapping is not ambiguous")
	}
	if ref.Kind != string(domain.KindScene) {
		t.Fatalf("the kind tells the adapter which old object this was, got %q", ref.Kind)
	}
	if !ref.Enabled {
		t.Fatal("the adapter renders the old Y/N switch from this field")
	}
	if len(ref.Candidates) != 1 || ref.Candidates[0] != rule.ID {
		t.Fatalf("candidates must list the one rule, got %v", ref.Candidates)
	}
}

func TestTheSameLegacyIdOnTwoRulesIsReportedNotPicked(t *testing.T) {
	e := newEnv(t)
	first := e.ruleWithLegacy("第一条", "legacy-dup-a30", domain.KindScene)
	second := e.ruleWithLegacy("第二条", "legacy-dup-a30", domain.KindScene)

	ref, err := e.svc().ResolveLegacyRef(e.ctx(), "legacy-dup-a30")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !ref.Ambiguous {
		t.Fatal("two rules claiming one old id must be reported as ambiguous")
	}
	if len(ref.Candidates) != 2 {
		t.Fatalf("both candidates must be listed, got %v", ref.Candidates)
	}
	seen := map[string]bool{ref.Candidates[0]: true, ref.Candidates[1]: true}
	if !seen[first.ID] || !seen[second.ID] {
		t.Fatalf("candidates %v do not name both rules", ref.Candidates)
	}
	// RuleID still names one of them so the response is renderable, but the
	// adapter refuses on Ambiguous and never runs it.
	if ref.RuleID != first.ID && ref.RuleID != second.ID {
		t.Fatalf("RuleID %q is neither candidate", ref.RuleID)
	}
}

func TestAnUnknownLegacyIdSaysItIsMissingNotEmpty(t *testing.T) {
	e := newEnv(t)

	_, err := e.svc().ResolveLegacyRef(e.ctx(), "never-migrated-a30")
	if err == nil {
		t.Fatal("an unmigrated id must be an error, not an empty response")
	}
	details := wireDetail(t, err)
	if details["code"] != "legacy_ref_missing" {
		t.Fatalf("the adapter branches on this code, got %v", details["code"])
	}
	if details["field"] != "legacy_id" {
		t.Fatalf("the field names what was wrong, got %v", details["field"])
	}
}

func TestAnEmptyLegacyIdIsAParameterError(t *testing.T) {
	e := newEnv(t)

	_, err := e.svc().ResolveLegacyRef(e.ctx(), "   ")
	if err == nil {
		t.Fatal("an empty id must be refused")
	}
	var target interface{ Error() string }
	if !errors.As(err, &target) {
		t.Fatalf("unexpected error type: %T", err)
	}
}

// TestTheListTotalCountsEveryMatchNotThePage is the defect A30 found through the
// App: the dashboard asks for one row just to print a badge, so a total equal to
// the page size reads "1" for every tenant.
func TestTheListTotalCountsEveryMatchNotThePage(t *testing.T) {
	e := newEnv(t)
	// The service-layer tests share one tenant, so the assertion is a delta: a
	// total that merely equals the page size is the defect, whatever the base is.
	base, err := e.svc().ListRules(e.ctx(), &ListRulesRequest{Limit: 1})
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	for i := 0; i < 3; i++ {
		e.ruleWithLegacy("规则 "+string(rune('A'+i)), "", domain.KindScene)
	}

	res, err := e.svc().ListRules(e.ctx(), &ListRulesRequest{Limit: 1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Rules) != 1 {
		t.Fatalf("limit=1 must return one row, got %d", len(res.Rules))
	}
	if res.Total != base.Total+3 {
		t.Fatalf("total must count every match: %d -> %d, want %d (page size is %d)",
			base.Total, res.Total, base.Total+3, len(res.Rules))
	}
	if res.Total <= len(res.Rules) {
		t.Fatalf("total (%d) must exceed the page (%d)", res.Total, len(res.Rules))
	}
}

func TestTheCountFollowsTheSameFilterAsThePage(t *testing.T) {
	e := newEnv(t)
	base, err := e.svc().ListRules(e.ctx(), &ListRulesRequest{Kind: "scene", Limit: 1})
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	e.ruleWithLegacy("场景一", "", domain.KindScene)
	e.ruleWithLegacy("场景二", "", domain.KindScene)
	e.ruleWithLegacy("联动一", "", domain.KindAutomation)

	res, err := e.svc().ListRules(e.ctx(), &ListRulesRequest{Kind: "scene", Limit: 1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if res.Total != base.Total+2 {
		t.Fatalf("the count must apply the same kind filter as the page: %d -> %d, want %d",
			base.Total, res.Total, base.Total+2)
	}
	if len(res.Rules) != 1 || res.Rules[0].Kind != "scene" {
		t.Fatalf("the page must be scenes only, got %+v", res.Rules)
	}
}
