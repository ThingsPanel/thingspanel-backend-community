package blueprint

import (
	"strings"
	"testing"
	"time"

	"encore.app/automation/domain"
)

// shipped lists the four templates this release actually delivers. Everything
// else in the registry during a test run is a synthetic version registered by a
// test, so assertions about "the product's templates" use this list rather than
// "whatever is in the registry".
func shipped() []string {
	return []string{
		keyHighTemperature,
		keyDoorLeftOpen,
		keyDeviceOffline,
		keyMotionLightOff,
	}
}

// minimal returns the smallest set of values a customer must supply: a device
// and nothing else that has no default.
func minimal(key string) Values {
	switch key {
	case keyMotionLightOff:
		return Values{"device": "dev-1"}
	default:
		return Values{"device": "dev-1"}
	}
}

func TestTheFourTemplatesOnlyNeedADeviceAndParameters(t *testing.T) {
	for _, key := range shipped() {
		bp := Latest(key)
		if bp == nil {
			t.Fatalf("template %s is missing", key)
		}
		inst, issues := Instantiate(key, 0, minimal(key))
		if inst == nil {
			t.Fatalf("%s: want an instance from a device id alone, got issues %+v", key, issues)
		}
		if HasError(issues) {
			t.Fatalf("%s: unexpected blocking issues %+v", key, issues)
		}
		// The definition must pass the same gate a hand-written rule passes. If
		// this ever fails, the template is producing something the runtime would
		// reject — which is the whole risk of having a second way to author.
		if err := domain.Validate(inst.Definition); err != nil {
			t.Fatalf("%s: produced an invalid definition: %v", key, err)
		}
	}
}

func TestATemplateNeverShipsWithARealDeviceOrGroup(t *testing.T) {
	for _, key := range shipped() {
		bp := Latest(key)
		for _, p := range bp.Params {
			if p.Type == ParamDevice && p.Default != "" {
				t.Errorf("%s.%s: a template must not pre-fill a device (%q)", key, p.Key, p.Default)
			}
			if p.Type == ParamPolicy && p.Default != "" {
				t.Errorf("%s.%s: a template must not pre-fill a notification group (%q)", key, p.Key, p.Default)
			}
			if p.Required && p.Default != "" && p.Type == ParamDevice {
				t.Errorf("%s.%s: required device with a default is a contradiction", key, p.Key)
			}
		}
		// And the produced definition must not name any device either.
		inst, _ := Instantiate(key, 0, minimal(key))
		if inst == nil {
			t.Fatalf("%s: no instance", key)
		}
		for _, id := range inst.Definition.Targets.DeviceIDs {
			if id != "dev-1" {
				t.Errorf("%s: definition targets %q, which did not come from the caller", key, id)
			}
		}
	}
}

func TestTheAlarmTemplatesCarryNoSteps(t *testing.T) {
	for _, key := range []string{keyHighTemperature, keyDoorLeftOpen, keyDeviceOffline} {
		inst, issues := Instantiate(key, 0, minimal(key))
		if inst == nil {
			t.Fatalf("%s: %+v", key, issues)
		}
		if inst.Definition.Kind != domain.KindAlarm {
			t.Fatalf("%s: want an alarm policy, got %s", key, inst.Definition.Kind)
		}
		if len(inst.Definition.Steps) != 0 {
			t.Fatalf("%s: an alarm policy has no actions to run, got %d steps", key, len(inst.Definition.Steps))
		}
		if inst.Definition.Alarm == nil || len(inst.Definition.Alarm.Sources) == 0 {
			t.Fatalf("%s: alarm policy must declare its intake channel", key)
		}
	}
}

func TestTheMotionTemplateIsAnActionFlowThatRestarts(t *testing.T) {
	inst, issues := Instantiate(keyMotionLightOff, 0, Values{"device": "dev-1", "hold": "90s"})
	if inst == nil {
		t.Fatalf("%+v", issues)
	}
	def := inst.Definition
	if def.Kind != domain.KindAutomation {
		t.Fatalf("want an automation, got %s", def.Kind)
	}
	// "single" would refuse the second person walking in and still switch the
	// light off at the first deadline. The template exists to avoid that.
	if def.Mode != domain.ModeRestart {
		t.Fatalf("want restart mode so a second detection re-arms the timer, got %s", def.Mode)
	}
	if len(def.Steps) != 3 {
		t.Fatalf("want on / wait / off, got %d steps", len(def.Steps))
	}
	if def.Steps[1].Kind != domain.StepWait || def.Steps[1].Wait.Std() != 90*time.Second {
		t.Fatalf("want the wait to follow the hold parameter, got %s", def.Steps[1].Wait.Std())
	}
	if def.Alarm != nil {
		t.Fatalf("an action flow must not pretend to be an alarm")
	}
}

func TestEveryCapabilityFollowsTheChosenMetric(t *testing.T) {
	inst, issues := Instantiate(keyHighTemperature, 0, Values{"device": "dev-1", "metric": "temp_c"})
	if inst == nil {
		t.Fatalf("%+v", issues)
	}
	found := false
	for _, c := range inst.Capabilities {
		if c.Kind == "telemetry" && c.Key == "temp_c" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want the required capability to name the metric the customer picked, got %+v",
			inst.Capabilities)
	}
}

func TestAnUnknownParameterIsRejected(t *testing.T) {
	_, issues := Instantiate(keyHighTemperature, 0, Values{"device": "dev-1", "thresold": "40"})
	if !hasCode(issues, "unknown_param") {
		t.Fatalf("want unknown_param for a typo, got %+v", issues)
	}
}

func TestARequiredParameterCannotBeOmitted(t *testing.T) {
	_, issues := Instantiate(keyHighTemperature, 0, Values{})
	if !hasCode(issues, "missing_param") {
		t.Fatalf("want missing_param for the device, got %+v", issues)
	}
}

func TestANumberOutOfRangeIsRejected(t *testing.T) {
	_, issues := Instantiate(keyHighTemperature, 0, Values{"device": "dev-1", "threshold": "500"})
	if !hasCode(issues, "out_of_range") {
		t.Fatalf("want out_of_range, got %+v", issues)
	}
}

func TestADurationMustBeADuration(t *testing.T) {
	_, issues := Instantiate(keyHighTemperature, 0, Values{"device": "dev-1", "duration": "5 minutes"})
	if !hasCode(issues, "invalid_duration") {
		t.Fatalf("want invalid_duration, got %+v", issues)
	}
	// "0" is a valid Go duration of zero, which is not a wait at all.
	_, issues = Instantiate(keyHighTemperature, 0, Values{"device": "dev-1", "duration": "0s"})
	if !hasCode(issues, "invalid_duration") {
		t.Fatalf("want invalid_duration for a zero wait, got %+v", issues)
	}
}

func TestTheFreshnessWindowOutlivesTheWait(t *testing.T) {
	// A trust window shorter than the wait makes the wait impossible to finish:
	// the evidence goes stale before the condition has held long enough. That was
	// a real defect (A08), so the template must not reintroduce it.
	inst, issues := Instantiate(keyHighTemperature, 0, Values{"device": "dev-1", "duration": "30m"})
	if inst == nil {
		t.Fatalf("%+v", issues)
	}
	wait := inst.Definition.Trigger.For.Std()
	if inst.Definition.Freshness.MaxAge.Std() <= wait {
		t.Fatalf("max_age %s does not outlive the wait %s",
			inst.Definition.Freshness.MaxAge.Std(), wait)
	}
}

func TestANewVersionDoesNotTouchAnExistingInstance(t *testing.T) {
	inst, issues := Instantiate(keyHighTemperature, 1, Values{"device": "dev-1"})
	if inst == nil {
		t.Fatalf("%+v", issues)
	}
	if latest := Latest(keyHighTemperature); latest.Version < 2 {
		t.Fatalf("this test needs a newer version to exist, latest is %d", latest.Version)
	}
	// The instance keeps the values it was created with. Nothing about publishing
	// v2 reaches into it.
	if got := inst.Values["duration"]; got != "5m" {
		t.Fatalf("the v1 instance changed its own duration to %q", got)
	}
	if got := inst.Definition.Trigger.For.Std(); got != 5*time.Minute {
		t.Fatalf("the v1 instance changed its own wait to %s", got)
	}
}

func TestTheDiffShowsWhatActuallyChanged(t *testing.T) {
	d, issues := DiffVersions(keyHighTemperature, 1, 2)
	if d == nil {
		t.Fatalf("%+v", issues)
	}
	if !containsChange(d.DefaultsChanged, "duration", "5m", "3m") {
		t.Fatalf("want the duration default change, got %+v", d.DefaultsChanged)
	}
	if !containsChange(d.RangesChanged, "threshold", "-50..200", "-50..120") {
		t.Fatalf("want the narrowed threshold range, got %+v", d.RangesChanged)
	}
	if !d.DefinitionChanged {
		t.Fatalf("the produced definition did change; saying otherwise would hide a behaviour change")
	}

	// The door template gained an optional parameter whose default is "no
	// change", so the diff must say exactly that: a new parameter, same body.
	dd, issues := DiffVersions(keyDoorLeftOpen, 1, 2)
	if dd == nil {
		t.Fatalf("%+v", issues)
	}
	if len(dd.AddedParams) != 1 || dd.AddedParams[0] != "recovery_grace" {
		t.Fatalf("want the added recovery_grace parameter, got %+v", dd.AddedParams)
	}
	if dd.DefinitionChanged {
		t.Fatalf("with the default empty grace the produced definition is unchanged")
	}
}

func TestAnOverrideOutOfTheNewRangeBlocksTheUpgrade(t *testing.T) {
	inst, issues := Instantiate(keyHighTemperature, 1, Values{"device": "dev-1", "threshold": "150"})
	if inst == nil {
		t.Fatalf("%+v", issues)
	}
	up := PlanUpgrade(*inst, 2, nil)
	if !up.Blocked {
		t.Fatalf("150 °C is outside the v2 range; the upgrade must stop, got %+v", up.Instance)
	}
	if !hasCode(up.Issues, "out_of_range") {
		t.Fatalf("want the range itself named as the reason, got %+v", up.Issues)
	}
	if up.Diff == nil || len(up.Diff.RangesChanged) == 0 {
		t.Fatalf("the blocked upgrade must still show the range change, got %+v", up.Diff)
	}
}

func TestAnUpgradeKeepsTheCustomersChoicesAndTakesNewDefaults(t *testing.T) {
	inst, issues := Instantiate(keyHighTemperature, 1, Values{"device": "dev-1", "threshold": "40"})
	if inst == nil {
		t.Fatalf("%+v", issues)
	}
	up := PlanUpgrade(*inst, 2, nil)
	if up.Blocked {
		t.Fatalf("40 °C is inside both ranges, want a clean upgrade, got %+v", up.Issues)
	}
	if got := up.Instance.Values["threshold"]; got != "40" {
		t.Fatalf("the customer's threshold was overwritten with %q", got)
	}
	if got := up.Instance.Values["duration"]; got != "3m" {
		t.Fatalf("an untouched value should follow the new default, got %q", got)
	}
	if up.Instance.BlueprintVersion != 2 {
		t.Fatalf("want the instance pinned to v2, got %d", up.Instance.BlueprintVersion)
	}
}

func TestUpgradingBackwardsIsRefused(t *testing.T) {
	inst, _ := Instantiate(keyHighTemperature, 2, Values{"device": "dev-1"})
	if inst == nil {
		t.Fatal("no instance")
	}
	up := PlanUpgrade(*inst, 1, nil)
	if !up.Blocked || !hasCode(up.Issues, "not_an_upgrade") {
		t.Fatalf("want not_an_upgrade, got blocked=%v issues=%+v", up.Blocked, up.Issues)
	}
}

// ---------------------------------------------------------------------------
// Synthetic versions, used only to exercise paths the shipped templates do not
// happen to hit. They are registered under a test-only key so they cannot be
// mistaken for product templates.
// ---------------------------------------------------------------------------

const keyEphemeral = "test_ephemeral"

func init() {
	base := func(key string, version int, params []Param) *Blueprint {
		return &Blueprint{
			Key: key, Version: version, Name: "测试用模板", Summary: "仅用于单元测试",
			Params: params,
			build: func(v Values) (*domain.Definition, []Issue) {
				return &domain.Definition{
					Kind:  domain.KindAutomation,
					Mode:  domain.ModeSingle,
					Scope: domain.ScopeDevice,
					Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
						Source: "self", Signal: domain.SignalTelemetry, Key: "x",
						Operator: domain.OpEq, Value: "1",
					}}}}},
					Targets: domain.TargetSpec{DeviceIDs: []string{v["device"]}},
					Steps: []domain.Step{{Kind: domain.StepAction, DeviceRef: "target",
						Action: "telemetry", Identifier: "x", Value: "1"}},
				}, nil
			},
		}
	}
	register(base(keyEphemeral, 1, []Param{
		{Key: "device", Label: "设备", Type: ParamDevice, Required: true},
		{Key: "legacy_knob", Label: "旧旋钮", Type: ParamText, Default: "0"},
	}))
	register(base(keyEphemeral, 2, []Param{
		{Key: "device", Label: "设备", Type: ParamDevice, Required: true},
	}))
}

func TestAnOverrideOnARemovedParameterBlocksTheUpgrade(t *testing.T) {
	inst, issues := Instantiate(keyEphemeral, 1, Values{"device": "dev-1", "legacy_knob": "7"})
	if inst == nil {
		t.Fatalf("%+v", issues)
	}
	up := PlanUpgrade(*inst, 2, nil)
	if !up.Blocked {
		t.Fatalf("v2 dropped legacy_knob while the customer set it to 7; want a block")
	}
	if !hasCode(up.Issues, "override_param_removed") {
		t.Fatalf("want override_param_removed, got %+v", up.Issues)
	}
	// The customer's value must be quoted back, otherwise "a parameter is gone"
	// is not enough to decide what to do.
	for _, i := range up.Issues {
		if i.Code == "override_param_removed" && !strings.Contains(i.Message, `"7"`) {
			t.Fatalf("the blocking message should name the lost value, got %q", i.Message)
		}
	}
}

func TestAnUnsetRemovedParameterDoesNotBlock(t *testing.T) {
	inst, issues := Instantiate(keyEphemeral, 1, Values{"device": "dev-1"})
	if inst == nil {
		t.Fatalf("%+v", issues)
	}
	up := PlanUpgrade(*inst, 2, nil)
	if up.Blocked {
		t.Fatalf("the customer never touched legacy_knob, so nothing of theirs is lost: %+v", up.Issues)
	}
	if up.Diff == nil || len(up.Diff.RemovedParams) != 1 {
		t.Fatalf("the removed parameter must still be visible in the diff, got %+v", up.Diff)
	}
}

func TestTheRevisionIsDerivedFromTheDeclaredSurface(t *testing.T) {
	bp := Latest(keyHighTemperature)
	if bp.Revision == "" {
		t.Fatal("every template version needs a fixed revision")
	}
	if want := RevisionFor(bp); want != bp.Revision {
		t.Fatalf("revision %s does not match its declared surface %s", bp.Revision, want)
	}
	// Two versions of the same key must never share a revision: that would make
	// "which template is this instance from" unanswerable.
	seen := map[string]int{}
	for _, key := range shipped() {
		for _, v := range Versions(key) {
			r := Get(key, v).Revision
			if other, ok := seen[r]; ok {
				t.Fatalf("revision %s is shared by versions %d and %d", r, other, v)
			}
			seen[r] = v
		}
	}
}

func hasCode(issues []Issue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

func containsChange(changes []ParamChange, param, from, to string) bool {
	for _, c := range changes {
		if c.Param == param && c.From == from && c.To == to {
			return true
		}
	}
	return false
}
