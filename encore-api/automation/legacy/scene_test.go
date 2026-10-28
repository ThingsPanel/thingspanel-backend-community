package legacy

import (
	"strings"
	"testing"

	"encore.app/automation/domain"
)

// stubResolver answers from literal sets so the tests describe a snapshot instead
// of mocking behaviour.
type stubResolver struct {
	scenes  map[string]bool
	devices map[string]bool
	configs map[string]bool
	alarms  map[string][2]string // legacy id -> {policy id, policy key}
	graph   map[string]*Scene
}

func newStub() *stubResolver {
	return &stubResolver{
		scenes: map[string]bool{}, devices: map[string]bool{}, configs: map[string]bool{},
		alarms: map[string][2]string{}, graph: map[string]*Scene{},
	}
}

func (s *stubResolver) SceneExists(id string) (bool, error)        { return s.scenes[id], nil }
func (s *stubResolver) DeviceExists(id string) (bool, error)       { return s.devices[id], nil }
func (s *stubResolver) DeviceConfigExists(id string) (bool, error) { return s.configs[id], nil }
func (s *stubResolver) Load(id string) (*Scene, error)             { return s.graph[id], nil }

func (s *stubResolver) AlarmPolicyFor(id string) (string, string, bool, error) {
	v, ok := s.alarms[id]
	if !ok {
		return "", "", false, nil
	}
	return v[0], v[1], true, nil
}

func hasIssue(p *Plan, code string) bool {
	for _, i := range p.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// TestOneDeviceStateWriteIsAStateScene: the simplest old scene — one device, one
// telemetry value — must not be turned into an ordered flow, because that would
// add semantics it never had.
func TestOneDeviceStateWriteIsAStateScene(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	scene := &Scene{ID: "scene-1", Name: "开灯", Actions: []Action{{
		ID: "a1", Target: "dev-1", Type: TypeSingleDevice,
		ParamType: ParamTelemetry, Param: "switch", Value: "1",
	}}}

	p := Build(scene, r)
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	if p.Shape != ShapeStateScene {
		t.Fatalf("want state_scene, got %q", p.Shape)
	}
	if len(p.Definition.Steps) != 1 {
		t.Fatalf("want one step, got %d", len(p.Definition.Steps))
	}
	step := p.Definition.Steps[0]
	if step.Kind != domain.StepAction || step.Action != "telemetry" ||
		step.Identifier != "switch" || step.Value != "1" || step.DeviceRef != "dev-1" {
		t.Fatalf("wrong step: %+v", step)
	}
	if p.Definition.Kind != domain.KindScene {
		t.Fatalf("the entry point must stay a scene, got %q", p.Definition.Kind)
	}
	if len(p.Definition.Trigger.Groups) != 0 {
		t.Fatal("a scene must not gain a trigger")
	}
}

// TestOrderedActionsBecomeASequenceInTheOriginalOrder is the defect this task
// exists to prevent: "open the valve then start the pump" is not a set.
func TestOrderedActionsBecomeASequenceInTheOriginalOrder(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	r.devices["dev-2"] = true
	scene := &Scene{ID: "scene-2", Actions: []Action{
		{ID: "a1", Target: "dev-1", Type: TypeSingleDevice, ParamType: ParamTelemetry, Param: "valve", Value: "1"},
		{ID: "a2", Target: "dev-2", Type: TypeSingleDevice, ParamType: ParamTelemetry, Param: "pump", Value: "1"},
	}}

	p := Build(scene, r)
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	if p.Shape != ShapeSequence {
		t.Fatalf("want sequence, got %q", p.Shape)
	}
	if len(p.Definition.Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(p.Definition.Steps))
	}
	if p.Definition.Steps[0].Identifier != "valve" || p.Definition.Steps[1].Identifier != "pump" {
		t.Fatalf("the order must be preserved: %+v", p.Definition.Steps)
	}
	if p.Mapping[0].LegacyActionID != "a1" || p.Mapping[1].LegacyActionID != "a2" {
		t.Fatalf("the mapping must keep the legacy order: %+v", p.Mapping)
	}
}

// TestACommandActionIsNotAStateScene: sending a command twice is not the same as
// setting a value twice, so it cannot be classified as a state write.
func TestACommandActionIsNotAStateScene(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	scene := &Scene{ID: "s", Actions: []Action{{
		ID: "a1", Target: "dev-1", Type: TypeSingleDevice,
		ParamType: ParamCommand, Param: "restart", Value: "1",
	}}}
	p := Build(scene, r)
	if p.Shape != ShapeSequence {
		t.Fatalf("a command action must be a sequence, got %q", p.Shape)
	}
	if p.Definition.Steps[0].Action != "command" {
		t.Fatalf("want a command step, got %+v", p.Definition.Steps[0])
	}
}

// TestADeviceConfigActionTargetsTheExpandedSet: the old "single kind of device"
// action must keep fanning out, and the new runtime freezes that membership at
// acceptance time (A12) instead of resolving it on every execution.
func TestADeviceConfigActionTargetsTheExpandedSet(t *testing.T) {
	r := newStub()
	r.configs["cfg-1"] = true
	scene := &Scene{ID: "s", Actions: []Action{{
		ID: "a1", Target: "cfg-1", Type: TypeDeviceConfig,
		ParamType: ParamTelemetry, Param: "switch", Value: "0",
	}}}
	p := Build(scene, r)
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	if len(p.Definition.Targets.DeviceConfigIDs) != 1 ||
		p.Definition.Targets.DeviceConfigIDs[0] != "cfg-1" {
		t.Fatalf("the config must be a target: %+v", p.Definition.Targets)
	}
	if p.Definition.Steps[0].DeviceRef != "target" {
		t.Fatalf("a config action must fan out over the target set, got %q",
			p.Definition.Steps[0].DeviceRef)
	}
}

// TestAnAlarmActionMustResolveToAnExistingPolicy: the old form stored an
// alarm_config id. If that policy was not migrated, the scene must fail to
// migrate instead of silently dropping the alarm or creating a second one.
func TestAnAlarmActionMustResolveToAnExistingPolicy(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true

	scene := &Scene{ID: "s", Actions: []Action{
		{ID: "a1", Target: "dev-1", Type: TypeSingleDevice, ParamType: ParamTelemetry, Param: "switch", Value: "1"},
		{ID: "a2", Target: "alarm-legacy-1", Type: TypeRaiseAlarm},
	}}

	missing := Build(scene, r)
	if missing.OK() {
		t.Fatal("an unresolved alarm reference must fail the migration")
	}
	if !hasIssue(missing, "action_unresolved") {
		t.Fatalf("want action_unresolved, got %v", missing.Issues)
	}
	if !strings.Contains(missing.Issues[0].Message, "alarm-legacy-1") {
		t.Fatalf("the issue must name the reference: %v", missing.Issues)
	}

	r.alarms["alarm-legacy-1"] = [2]string{"pol-1", "policy.overheat"}
	ok := Build(scene, r)
	if !ok.OK() {
		t.Fatalf("unexpected issues: %v", ok.Issues)
	}
	alarmStep := ok.Definition.Steps[1]
	if alarmStep.Kind != domain.StepAlarm || alarmStep.AlarmPolicyID != "pol-1" ||
		alarmStep.Value != "qualified" {
		t.Fatalf("wrong alarm step: %+v", alarmStep)
	}
	if !strings.Contains(ok.Mapping[1].Note, "policy.overheat") {
		t.Fatalf("the mapping must name the policy key: %+v", ok.Mapping[1])
	}
}

// TestAServiceActionFailsExplicitly: the old webhook action has no owner in this
// release. Reporting it is the only honest answer; pretending to migrate it would
// produce a scene that quietly does nothing.
func TestAServiceActionFailsExplicitly(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	scene := &Scene{ID: "s", Actions: []Action{
		{ID: "a1", Target: "dev-1", Type: TypeSingleDevice, ParamType: ParamTelemetry, Param: "switch", Value: "1"},
		{ID: "a2", Target: "http://hook", Type: TypeService},
	}}
	p := Build(scene, r)
	if p.OK() {
		t.Fatal("a service action must fail the migration")
	}
	found := false
	for _, i := range p.Issues {
		if strings.Contains(i.Message, "type=40") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the issue must say which action type is unsupported: %v", p.Issues)
	}
}

// TestContinueOnFailureIsCarriedAndReported: the new default stops on a critical
// failure, so every legacy exception has to be listed rather than assumed.
func TestContinueOnFailureIsCarriedAndReported(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	r.devices["dev-2"] = true
	scene := &Scene{ID: "s", Actions: []Action{
		{ID: "a1", Target: "dev-1", Type: TypeSingleDevice, ParamType: ParamTelemetry,
			Param: "switch", Value: "1", ContinueOnFailure: true},
		{ID: "a2", Target: "dev-2", Type: TypeSingleDevice, ParamType: ParamTelemetry,
			Param: "switch", Value: "1"},
	}}
	p := Build(scene, r)
	if !p.OK() {
		t.Fatalf("unexpected issues: %v", p.Issues)
	}
	if !p.Definition.Steps[0].ContinueOnFailure {
		t.Fatal("the flag must be carried onto the step")
	}
	if p.Definition.Steps[1].ContinueOnFailure {
		t.Fatal("a step without the flag must not gain it")
	}
	if len(p.ContinuedSteps) != 1 || p.ContinuedSteps[0] != "a1" {
		t.Fatalf("the exception must be reported: %+v", p.ContinuedSteps)
	}
}

// TestAMissingDeviceIsAnIssueNotAnEmptyTarget: an empty target set would produce a
// rule that publishes fine and then does nothing, which is the worst outcome.
func TestAMissingDeviceIsAnIssueNotAnEmptyTarget(t *testing.T) {
	r := newStub() // no devices at all
	scene := &Scene{ID: "s", Actions: []Action{{
		ID: "a1", Target: "dev-gone", Type: TypeSingleDevice,
		ParamType: ParamTelemetry, Param: "switch", Value: "1",
	}}}
	p := Build(scene, r)
	if p.OK() {
		t.Fatal("a dangling device reference must fail the migration")
	}
	if !hasIssue(p, "action_unresolved") || !hasIssue(p, "no_targets") {
		t.Fatalf("want action_unresolved and no_targets, got %v", p.Issues)
	}
}

// TestASceneActivatingItselfIsACycle: the old engine would happily recurse until
// something broke.
func TestASceneActivatingItselfIsACycle(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	self := &Scene{ID: "s1", Actions: []Action{
		{ID: "a1", Target: "dev-1", Type: TypeSingleDevice, ParamType: ParamTelemetry, Param: "switch", Value: "1"},
		{ID: "a2", Target: "s1", Type: TypeActivateScene},
	}}
	r.scenes["s1"] = true
	r.graph["s1"] = self

	path, iss := CheckCycles("s1", r)
	if iss == nil {
		t.Fatal("a scene activating itself must be reported as a cycle")
	}
	if iss.Code != "scene_cycle" {
		t.Fatalf("want scene_cycle, got %q", iss.Code)
	}
	if len(path) < 2 || path[0] != "s1" || path[len(path)-1] != "s1" {
		t.Fatalf("the path must start and end at the loop: %v", path)
	}
}

// TestAThreeSceneCycleIsReportedWithItsPath: knowing there is a cycle is not
// enough; the operator has to know which edge to cut.
func TestAThreeSceneCycleIsReportedWithItsPath(t *testing.T) {
	r := newStub()
	mk := func(id, next string) *Scene {
		r.scenes[id] = true
		s := &Scene{ID: id, Actions: []Action{{ID: "a", Target: next, Type: TypeActivateScene}}}
		r.graph[id] = s
		return s
	}
	mk("s1", "s2")
	mk("s2", "s3")
	mk("s3", "s1")

	path, iss := CheckCycles("s1", r)
	if iss == nil || iss.Code != "scene_cycle" {
		t.Fatalf("want a cycle, got %v", iss)
	}
	want := []string{"s1", "s2", "s3", "s1"}
	if len(path) != len(want) {
		t.Fatalf("want path %v, got %v", want, path)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Fatalf("want path %v, got %v", want, path)
		}
	}
}

// TestADeepChainIsRejected: a chain long enough to be unreadable is a design
// problem even when it terminates.
func TestADeepChainIsRejected(t *testing.T) {
	r := newStub()
	for i := 0; i < MaxSceneDepth+3; i++ {
		id := "s" + itoa(i)
		next := "s" + itoa(i+1)
		r.scenes[id] = true
		r.graph[id] = &Scene{ID: id, Actions: []Action{{ID: "a", Target: next, Type: TypeActivateScene}}}
	}
	_, iss := CheckCycles("s0", r)
	if iss == nil || iss.Code != "scene_depth_exceeded" {
		t.Fatalf("want scene_depth_exceeded, got %v", iss)
	}
}

// TestAnEmptySceneIsReported: a scene with no actions would publish as a rule that
// does nothing.
func TestAnEmptySceneIsReported(t *testing.T) {
	p := Build(&Scene{ID: "s"}, newStub())
	if p.OK() || !hasIssue(p, "empty_scene") {
		t.Fatalf("want empty_scene, got %v", p.Issues)
	}
}

// TestSummarizeCountsShapesAndIssues keeps the migration dashboard honest about
// what it has and has not converted.
func TestSummarizeCountsShapesAndIssues(t *testing.T) {
	r := newStub()
	r.devices["dev-1"] = true
	state := Build(&Scene{ID: "s1", Actions: []Action{{
		ID: "a1", Target: "dev-1", Type: TypeSingleDevice, ParamType: ParamTelemetry,
		Param: "switch", Value: "1", ContinueOnFailure: true,
	}}}, r)
	broken := Build(&Scene{ID: "s2", Actions: []Action{
		{ID: "a1", Target: "dev-1", Type: TypeSingleDevice, ParamType: ParamTelemetry,
			Param: "switch", Value: "1"},
		{ID: "a2", Target: "gone", Type: TypeSingleDevice, ParamType: ParamTelemetry,
			Param: "switch", Value: "1"},
	}}, r)

	s := Summarize([]*Plan{state, broken})
	if s.Total != 2 || s.StateScene != 1 || s.Sequence != 1 || s.Failed != 1 || s.Continued != 1 {
		t.Fatalf("wrong summary: %+v", s)
	}
	if s.ByIssue["action_unresolved"] != 1 {
		t.Fatalf("issues must be counted by code: %+v", s.ByIssue)
	}
}

func itoa(n int) string {
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
