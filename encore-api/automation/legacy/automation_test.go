package legacy

import (
	"strings"
	"testing"

	"encore.app/automation/domain"
)

// ---------------------------------------------------------------------------
// The automation mapping
// ---------------------------------------------------------------------------
//
// These tests exist because the first version of this mapping did not exist: A31
// shipped automations as "unsupported" on the claim that the old operator column
// held a number with no consumer. It holds a string and the old evaluator reads
// it. A test that pins the operator is what stops that claim from coming back.

func automationWith(conds ...TriggerCondition) *Automation {
	return &Automation{
		ID: "auto-1", Name: "温度高了开风机", Enabled: true, Conditions: conds,
		Actions: []Action{{
			ID: "a1", Target: "dev-1", Type: TypeSingleDevice,
			ParamType: ParamCommand, Param: "fan", Value: "on",
		}},
	}
}

func condOf(p *AutomationPlan, group, key string) (domain.Condition, bool) {
	for _, g := range p.Definition.Trigger.Groups {
		if g.ID != group {
			continue
		}
		for _, c := range g.Conditions {
			if c.Key == key {
				return c, true
			}
		}
	}
	return domain.Condition{}, false
}

// TestEveryLegacyOperatorSurvives is the direct answer to "the mapping is an
// inference": it is not, and every operator the old evaluator accepts arrives
// unchanged.
func TestEveryLegacyOperatorSurvives(t *testing.T) {
	for _, op := range []string{"=", "!=", ">", "<", ">=", "<=", "between", "in"} {
		r := newStub()
		r.devices["dev-1"] = true
		a := automationWith(TriggerCondition{
			ID: "c1", GroupID: "g1", Type: CondSingleDevice, Source: "dev-1",
			ParamType: ParamTelemetry, Param: "temperature", Operator: op, Value: "30",
		})
		p := BuildAutomation(a, r)
		if !p.OK() {
			t.Fatalf("operator %q: unexpected issues %v", op, p.Issues)
		}
		got, ok := condOf(p, "g1", "temperature")
		if !ok {
			t.Fatalf("operator %q: condition missing", op)
		}
		if got.Operator != op {
			t.Fatalf("operator %q became %q", op, got.Operator)
		}
	}
}

// TestGroupsAreOrOfAnds: group_id is the AND group and the groups are OR-ed. A
// mapping that flattened them into one AND would make a rule fire less often than
// the old engine did, which is the failure nobody reports.
func TestGroupsAreOrOfAnds(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	p := BuildAutomation(automationWith(
		TriggerCondition{ID: "c1", GroupID: "g1", Type: CondSingleDevice, Source: "dev-1",
			ParamType: ParamTelemetry, Param: "temperature", Operator: ">", Value: "30"},
		TriggerCondition{ID: "c2", GroupID: "g1", Type: CondSingleDevice, Source: "dev-1",
			ParamType: ParamTelemetry, Param: "humidity", Operator: ">", Value: "80"},
		TriggerCondition{ID: "c3", GroupID: "g2", Type: CondSingleDevice, Source: "dev-1",
			ParamType: ParamTelemetry, Param: "smoke", Operator: "=", Value: "1"},
	), r)
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	if len(p.Definition.Trigger.Groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(p.Definition.Trigger.Groups))
	}
	if n := len(p.Definition.Trigger.Groups[0].Conditions); n != 2 {
		t.Fatalf("first group should keep its 2 AND conditions, got %d", n)
	}
	if n := len(p.Definition.Trigger.Groups[1].Conditions); n != 1 {
		t.Fatalf("second group should keep its 1 condition, got %d", n)
	}
}

// TestAnExplicitDeviceTriggerIsNotSelf: "device" pins the device that must
// report. Reading it as "self" would let the rule fire from any device in the
// target set, which is a wider rule than the customer wrote.
func TestAnExplicitDeviceTriggerIsNotSelf(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	r.devices["dev-2"] = true
	a := automationWith(TriggerCondition{
		ID: "c1", GroupID: "g1", Type: CondSingleDevice, Source: "dev-2",
		ParamType: ParamTelemetry, Param: "temperature", Operator: ">", Value: "30",
	})
	p := BuildAutomation(a, r)
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	got, _ := condOf(p, "g1", "temperature")
	if got.Source != "device" || got.DeviceID != "dev-2" {
		t.Fatalf("want source=device device_id=dev-2, got source=%q device_id=%q", got.Source, got.DeviceID)
	}
	// The triggering device has to be a target, otherwise there is no run subject.
	found := false
	for _, id := range p.Definition.Targets.DeviceIDs {
		if id == "dev-2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("触发设备应该在目标集里，实际 %v", p.Definition.Targets.DeviceIDs)
	}
}

// TestAConfigTriggerBecomesATargetSet: "any device of this config" is a target
// set plus a condition on "self", which is the only reading that keeps the run
// subject equal to the device that reported.
func TestAConfigTriggerBecomesATargetSet(t *testing.T) {
	r := newStub()
	r.configs["cfg-1"] = true
	r.devices["dev-1"] = true
	a := &Automation{
		ID: "auto-cfg", Name: "整类设备", Enabled: true,
		Conditions: []TriggerCondition{{
			ID: "c1", GroupID: "g1", Type: CondDeviceConfig, Source: "cfg-1",
			ParamType: ParamTelemetry, Param: "temperature", Operator: ">", Value: "30",
		}},
		Actions: []Action{{
			ID: "a1", Target: "cfg-1", Type: TypeDeviceConfig,
			ParamType: ParamCommand, Param: "fan", Value: "on",
		}},
	}
	p := BuildAutomation(a, r)
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	got, _ := condOf(p, "g1", "temperature")
	if got.Source != "self" {
		t.Fatalf("整类设备条件应该是 self，实际 %q", got.Source)
	}
	if len(p.Definition.Targets.DeviceConfigIDs) != 1 || p.Definition.Targets.DeviceConfigIDs[0] != "cfg-1" {
		t.Fatalf("目标集应该是那个设备配置，实际 %v", p.Definition.Targets.DeviceConfigIDs)
	}
	if len(p.Definition.Targets.DeviceIDs) != 0 {
		t.Fatalf("整类设备触发的规则不应该混入单个设备目标，实际 %v", p.Definition.Targets.DeviceIDs)
	}
}

// TestAnActionOnAnotherConfigIsRefused: the action's "整类设备" fans out to the
// run target set, so an action on a config other than the trigger's would drive
// devices the trigger never selected.
func TestAnActionOnAnotherConfigIsRefused(t *testing.T) {
	r := newStub()
	r.configs["cfg-1"] = true
	r.configs["cfg-2"] = true
	a := &Automation{
		ID: "auto-x", Name: "跨配置", Enabled: true,
		Conditions: []TriggerCondition{{
			ID: "c1", GroupID: "g1", Type: CondDeviceConfig, Source: "cfg-1",
			ParamType: ParamTelemetry, Param: "temperature", Operator: ">", Value: "30",
		}},
		Actions: []Action{{
			ID: "a1", Target: "cfg-2", Type: TypeDeviceConfig,
			ParamType: ParamCommand, Param: "fan", Value: "on",
		}},
	}
	p := BuildAutomation(a, r)
	if p.OK() {
		t.Fatal("动作作用在另一个设备配置上必须拒绝")
	}
	if !hasAutomationIssue(p, "action_config_differs_from_trigger") {
		t.Fatalf("原因码应该是 action_config_differs_from_trigger，实际 %v", p.Issues)
	}
}

// TestStatusConditionsBecomeTheOnlineSignal: the old engine read login state and
// ignored the stored operator; the new one reads "online" with 1/0.
func TestStatusConditionsBecomeTheOnlineSignal(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	p := BuildAutomation(automationWith(TriggerCondition{
		ID: "c1", GroupID: "g1", Type: CondSingleDevice, Source: "dev-1",
		ParamType: ParamStatus, Param: "OFF-LINE", Value: "",
	}), r)
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	got, _ := condOf(p, "g1", "online")
	if got.Signal != domain.SignalStatus || got.Operator != domain.OpEq || got.Value != "0" {
		t.Fatalf("下线条件应该映射成 online=0，实际 signal=%q operator=%q value=%q",
			got.Signal, got.Operator, got.Value)
	}
}

// TestStatusAnyTransitionIsRefused: "ALL" means "any transition", and the new
// model has no such condition. Guessing "on change" would fire on the opposite
// edge as well.
func TestStatusAnyTransitionIsRefused(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	p := BuildAutomation(automationWith(TriggerCondition{
		ID: "c1", GroupID: "g1", Type: CondSingleDevice, Source: "dev-1",
		ParamType: ParamStatus, Param: "ALL",
	}), r)
	if p.OK() {
		t.Fatal("状态「全部」必须拒绝")
	}
	if !hasAutomationIssue(p, "status_any_transition_unsupported") {
		t.Fatalf("原因码不对：%v", p.Issues)
	}
}

// TestAMissingDeviceIsAnIssueNotAnEmptyTarget: an id that no longer resolves must
// stop the row, not produce a rule pointing at nothing.
func TestAMissingTriggerDeviceIsAnIssue(t *testing.T) {
	r := newStub()
	p := BuildAutomation(automationWith(TriggerCondition{
		ID: "c1", GroupID: "g1", Type: CondSingleDevice, Source: "gone",
		ParamType: ParamTelemetry, Param: "temperature", Operator: ">", Value: "30",
	}), r)
	if p.OK() {
		t.Fatal("引用不存在的设备必须拒绝")
	}
	if !hasAutomationIssue(p, "trigger_device_missing") {
		t.Fatalf("原因码不对：%v", p.Issues)
	}
}

// TestAnEmptyOperatorDefaultsToEquals: the old engine treats a nil operator as
// "=", so the migration must do the same rather than reject the row.
func TestAnEmptyOperatorDefaultsToEquals(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	p := BuildAutomation(automationWith(TriggerCondition{
		ID: "c1", GroupID: "g1", Type: CondSingleDevice, Source: "dev-1",
		ParamType: ParamTelemetry, Param: "temperature", Value: "30",
	}), r)
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	got, _ := condOf(p, "g1", "temperature")
	if got.Operator != domain.OpEq {
		t.Fatalf("空运算符应该按旧引擎的行为补成 =，实际 %q", got.Operator)
	}
}

func hasAutomationIssue(p *AutomationPlan, code string) bool {
	for _, i := range p.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// TestAlarmConfigWithoutALevelIsRefused: the column is nullable, and defaulting
// it would change how the alarm is filtered and displayed.
func TestAlarmConfigWithoutALevelIsRefused(t *testing.T) {
	p := BuildAlarmConfig(&AlarmConfig{ID: "ac-1", Name: "没等级"}, nil)
	if p.OK() {
		t.Fatal("没有等级的告警配置必须拒绝")
	}
	if !hasAutomationIssue(p, "alarm_level_missing") {
		t.Fatalf("原因码不对：%v", p.Issues)
	}
}

// TestAlarmConfigKeepsItsCallers: the allowed_sources list is derived from the
// old data, so an alarm that a scene raises stays raiseable by that scene.
func TestAlarmConfigKeepsItsCallers(t *testing.T) {
	p := BuildAlarmConfig(&AlarmConfig{ID: "ac-1", Name: "高温", Level: "H"},
		[]string{"scene:sc-1", "scene:sc-1", "automation:au-1"})
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	// "automation" is the source a rule reports when its alarm step fires, so it
	// is always allowed; the derived ids ride along and are de-duplicated.
	got := strings.Join(p.Definition.Alarm.AllowedSources, ",")
	if got != "automation,automation:au-1,scene:sc-1" {
		t.Fatalf("调用方应该去重、排序并包含 automation，实际 %q", got)
	}
	if len(p.Definition.Trigger.Groups) != 0 {
		t.Fatalf("事件型策略不应该有触发组，实际 %d 组", len(p.Definition.Trigger.Groups))
	}
}
