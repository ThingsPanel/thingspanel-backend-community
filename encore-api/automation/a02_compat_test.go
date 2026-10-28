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
// A02 blockers: keep the six legacy entry points working on the new backend.
//
// The old UI never showed "all rules of the tenant". Every entry point was a
// context — a device tab, a template tab, an alarm tab, a search box — and the
// old scene object had no trigger at all, because the user pressing "激活" *was*
// the trigger. Losing either of those turns a working page into an empty or
// broken one, so both are fixed here and locked down by tests.
// ---------------------------------------------------------------------------

// manualScene is the shape of the legacy `scene` object: actions only, no
// condition, no enable switch, executed on demand.
func (e *testEnv) manualScene() domain.Definition {
	return domain.Definition{
		Kind:  domain.KindScene,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeRule,
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{e.dev1}},
		Steps: []domain.Step{{
			Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1",
		}},
	}
}

// ---------------------------------------------------------------------------
// Listing by context
// ---------------------------------------------------------------------------

// TestDeviceTabListsOnlyItsOwnRules: the device automation tab must show the
// rules that belong to that device, not the whole tenant.
func TestDeviceTabListsOnlyItsOwnRules(t *testing.T) {
	e := newEnv(t)

	def1 := e.baseDefinition()
	def1.Targets = domain.TargetSpec{DeviceIDs: []string{e.dev1}}
	rule1 := e.createRule(def1)

	def2 := e.baseDefinition()
	def2.Targets = domain.TargetSpec{DeviceIDs: []string{e.dev2}}
	rule2 := e.createRule(def2)

	got, err := e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{DeviceID: e.dev1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != rule1.ID {
		t.Fatalf("want only rule1 for dev1, got %d rules", len(got))
	}
	got, err = e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{DeviceID: e.dev2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != rule2.ID {
		t.Fatalf("want only rule2 for dev2, got %d rules", len(got))
	}
	// A device that is only *watched* by a condition also owns the rule.
	watched := e.baseDefinition()
	watched.Targets = domain.TargetSpec{DeviceIDs: []string{e.dev2}}
	watched.Trigger.Groups[0].Conditions[0].Source = "device"
	watched.Trigger.Groups[0].Conditions[0].DeviceID = e.dev1
	rule3 := e.createRule(watched)
	got, err = e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{DeviceID: e.dev1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := map[string]bool{}
	for _, r := range got {
		ids[r.ID] = true
	}
	if !ids[rule3.ID] {
		t.Fatal("a rule whose condition watches the device must appear in its tab")
	}
	if ids[rule2.ID] {
		t.Fatal("a rule for another device must not appear")
	}
}

// TestTemplateTabListsOnlyItsOwnRules: same contract for the device-template
// tabs, which filter by device_config_id.
func TestTemplateTabListsOnlyItsOwnRules(t *testing.T) {
	e := newEnv(t)

	byTemplate := e.baseDefinition()
	byTemplate.Targets = domain.TargetSpec{DeviceConfigIDs: []string{"cfg-1"}}
	ruleT := e.createRule(byTemplate)

	byDevice := e.baseDefinition()
	byDevice.Targets = domain.TargetSpec{DeviceIDs: []string{e.dev1}}
	e.createRule(byDevice)

	got, err := e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{DeviceConfigID: "cfg-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != ruleT.ID {
		t.Fatalf("want only the template rule, got %d rules", len(got))
	}
}

// TestAlarmTabListsOnlyRulesWithAlarmSteps: the legacy "告警规则" tab was
// "automations that contain an alarm action", not a separate object type. If the
// filter is missing, the tab either shows every automation or nothing.
func TestAlarmTabListsOnlyRulesWithAlarmSteps(t *testing.T) {
	e := newEnv(t)

	alarmDef := e.baseDefinition()
	alarmDef.Kind = domain.KindAlarm
	alarmDef.Steps = []domain.Step{{Kind: domain.StepNotify,
		NotificationPolicyID: "np-1", Value: "告警"}}
	policy := e.createRule(alarmDef)

	withAlarm := e.baseDefinition()
	withAlarm.Steps = []domain.Step{
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
		{Kind: domain.StepAlarm, AlarmPolicyID: policy.ID},
	}
	ruleA := e.createRule(withAlarm)

	plain := e.baseDefinition()
	e.createRule(plain)

	got, err := e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{HasAlarmStep: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != ruleA.ID {
		t.Fatalf("want only the rule with an alarm step, got %d rules", len(got))
	}

	// An alarm step nested inside a branch must still count, otherwise a rule
	// silently disappears from the tab as soon as the author adds a condition.
	nested := e.baseDefinition()
	nested.Steps = []domain.Step{{
		Kind: domain.StepBranch,
		Condition: &domain.Group{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "0",
		}}},
		Then: []domain.Step{{Kind: domain.StepAlarm, AlarmPolicyID: policy.ID}},
	}}
	ruleN := e.createRule(nested)

	got, err = e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{HasAlarmStep: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := map[string]bool{}
	for _, r := range got {
		ids[r.ID] = true
	}
	if !ids[ruleN.ID] {
		t.Fatal("an alarm step nested in a branch must still be found")
	}
}

// TestNameFilterIsCaseInsensitiveSubstring backs the search box.
func TestNameFilterIsCaseInsensitiveSubstring(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: e.tenant, Name: "三楼主卧 HighTemp",
		Kind: def.Kind, Enabled: true, Definition: def,
		DefinitionHash: domain.DefinitionHash(def),
	}
	if err := e.st.CreateRule(e.ctx(), rule); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{Name: "hightemp"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != rule.ID {
		t.Fatalf("want 1 match for a case-insensitive substring, got %d", len(got))
	}
	got, err = e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{Name: "主卧"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 match for a Chinese substring, got %d", len(got))
	}
}

// TestEnabledFilterIsTriState: an empty value means "any", because the old lists
// showed enabled and disabled rules together with a switch on each card.
func TestEnabledFilterIsTriState(t *testing.T) {
	e := newEnv(t)
	on := e.createRule(e.baseDefinition())
	off := e.createRule(e.baseDefinition())
	if _, err := e.st.SetEnabled(e.ctx(), e.tenant, off.ID, false, off.Version); err != nil {
		t.Fatalf("disable: %v", err)
	}

	any, _ := e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{})
	if len(any) != 2 {
		t.Fatalf("empty filter must return both, got %d", len(any))
	}
	yes := true
	enabled, _ := e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{Enabled: &yes})
	if len(enabled) != 1 || enabled[0].ID != on.ID {
		t.Fatalf("want only the enabled rule, got %d", len(enabled))
	}
	no := false
	disabled, _ := e.st.ListRules(e.ctx(), e.tenant, store.RuleFilter{Enabled: &no})
	if len(disabled) != 1 || disabled[0].ID != off.ID {
		t.Fatalf("want only the disabled rule, got %d", len(disabled))
	}
}

// ---------------------------------------------------------------------------
// Manual scenes
// ---------------------------------------------------------------------------

// TestManualSceneNeedsNoTrigger: the legacy scene had no condition. Requiring one
// would force the author to invent it and would change what an existing scene
// means, so the requirement is relaxed for kind=scene and only there.
func TestManualSceneNeedsNoTrigger(t *testing.T) {
	e := newEnv(t)
	def := e.manualScene()
	if err := domain.Validate(&def); err != nil {
		t.Fatalf("a manual scene must be publishable without a trigger: %v", err)
	}
	rule := e.createRule(def)
	if rule.ID == "" {
		t.Fatal("scene not stored")
	}

	// Everything else still needs a trigger.
	automation := e.manualScene()
	automation.Kind = domain.KindAutomation
	if err := domain.Validate(&automation); err == nil {
		t.Fatal("an automation without a trigger must still be rejected")
	}
	policy := e.manualScene()
	policy.Kind = domain.KindAlarm
	if err := domain.Validate(&policy); err == nil {
		t.Fatal("an alarm policy without a trigger must still be rejected")
	}
}

// TestManualRunExecutesTheScene is the "激活" button: no trigger is evaluated,
// the steps run, and the run is marked manual so the detail view can say so.
func TestManualRunExecutesTheScene(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.manualScene())

	run, err := e.rt.RunNow(e.ctx(), rule, "")
	if err != nil {
		t.Fatalf("run now: %v", err)
	}
	if run.Status != runtime.StatusSucceeded {
		t.Fatalf("want succeeded, got %q (%s)", run.Status, run.Reason)
	}
	if e.cmds.count() != 1 {
		t.Fatalf("want 1 command, got %d", e.cmds.count())
	}
	req, ok := e.cmds.last()
	if !ok || req.Identifier != "switch" || req.Value != "1" {
		t.Fatalf("wrong command: %+v", req)
	}
	// The snapshot must say "manual" so a manual run is distinguishable from a
	// timer-driven one (both have an empty trigger_event_id).
	stored := e.run(run.ID)
	var snap map[string]any
	if err := json.Unmarshal(stored.Snapshot, &snap); err != nil {
		t.Fatalf("snapshot is not valid json: %v", err)
	}
	if manual, _ := snap["manual"].(bool); !manual {
		t.Fatalf("snapshot must mark the run manual, got %s", stored.Snapshot)
	}
}

// TestManualRunRejectsAlarmPolicy: an alarm policy has no actions to execute, so
// "run now" on it would produce a run that can only fail. Reject it up front.
func TestManualRunRejectsAlarmPolicy(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Kind = domain.KindAlarm
	def.Steps = []domain.Step{{Kind: domain.StepNotify,
		NotificationPolicyID: "np-1", Value: "告警"}}
	policy := e.createRule(def)

	if _, err := e.rt.RunNow(e.ctx(), policy, ""); err == nil {
		t.Fatal("run-now on an alarm policy must be rejected")
	}
}

// TestManualRunStillHonoursTheRunMode: pressing the button twice is exactly the
// re-entry single/restart/queued/parallel describe, so the second press must be
// recorded as ignored rather than silently doing nothing.
func TestManualRunStillHonoursTheRunMode(t *testing.T) {
	e := newEnv(t)
	def := e.manualScene()
	def.Steps = []domain.Step{
		{Kind: domain.StepWait, Wait: domain.Duration(time.Minute)},
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
	}
	rule := e.createRule(def)

	first, err := e.rt.RunNow(e.ctx(), rule, "")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if first.Status != runtime.StatusRunning {
		t.Fatalf("want the first run to be waiting, got %q", first.Status)
	}
	second, err := e.rt.RunNow(e.ctx(), rule, "")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second.Status != runtime.StatusIgnored {
		t.Fatalf("want the second press to be ignored by single mode, got %q", second.Status)
	}
	if second.ReasonCode != "mode_single_busy" {
		t.Fatalf("want a readable reason, got %q", second.ReasonCode)
	}
}
