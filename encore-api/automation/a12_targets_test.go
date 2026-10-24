package automation

import (
	"fmt"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A12: who the rule acts on.
//
// Two properties matter and they pull in opposite directions:
//
//   - the target set must be dynamic, so a lamp installed tomorrow is covered
//     without editing every rule;
//   - a run that already started must not change its targets underneath it, or
//     the execution record describes a set of devices that never existed.
//
// The answer is a snapshot frozen at admission plus a re-check at execution.
// These tests pin both halves, and the three ways a batch can quietly look
// successful when it was not: no target, no available target, partial failure.
// ---------------------------------------------------------------------------

// withTargets builds a rule that acts on a target spec instead of the trigger
// device.
func (e *testEnv) withTargets(spec domain.TargetSpec) domain.Definition {
	def := e.baseDefinition()
	def.Targets = spec
	return def
}

func (e *testEnv) addDevice(id, label, deviceType, cfg string, caps ...string) {
	e.t.Helper()
	e.dir.devices[id] = ports.Device{
		ID: id, TenantID: e.tenant, Name: id, Label: label,
		DeviceConfigID: cfg, DeviceType: deviceType, Online: 1, Capabilities: caps,
	}
}

func (e *testEnv) targets(runID string) map[string]*store.Target {
	e.t.Helper()
	list, err := e.st.ListTargets(e.ctx(), runID)
	if err != nil {
		e.t.Fatalf("list targets: %v", err)
	}
	out := map[string]*store.Target{}
	for i := range list {
		out[list[i].DeviceID] = &list[i]
	}
	return out
}

// A device that gains the label after admission joins the next run, not the one
// already in flight.
func TestTargetsAreFrozenAtAdmission(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.withTargets(domain.TargetSpec{Labels: []string{"floor-1"}}))

	e.ingest("ev-a12-freeze-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	first := e.targets(runs[0].ID)
	if len(first) != 2 {
		t.Fatalf("the first run must snapshot the two labelled devices, got %v", keys(first))
	}

	// A third lamp is installed and labelled.
	late := e.dev1 + "-late"
	e.addDevice(late, "floor-1", "sensor", "cfg-1")
	e.advance(time.Minute)
	e.ingest("ev-a12-freeze-2", e.dev1, "temperature", 36.0, "")

	second := e.targets(e.runIDByTrigger(rule.ID, "ev-a12-freeze-2"))
	if len(second) != 3 {
		t.Fatalf("the next run must include the new device, got %v", keys(second))
	}
	if _, ok := first[late]; ok {
		t.Fatal("a started run must not gain targets after admission")
	}
	if _, ok := second[late]; !ok {
		t.Fatal("the new device must join the next run")
	}
}

// No targets at all is a skipped step: there was nothing to do and nothing
// failed. Every target excluded is a failure: the rule was asked to act and
// could not.
func TestNoTargetsIsSkippedButAllExcludedIsAFailure(t *testing.T) {
	e := newEnv(t)

	empty := e.createRule(e.withTargets(domain.TargetSpec{Labels: []string{"nobody-has-this"}}))
	e.ingest("ev-a12-empty-1", e.dev1, "temperature", 35.0, "")
	emptyRun := e.runs(empty.ID)[0]
	if got := e.run(emptyRun.ID).Status; got != runtime.StatusSucceeded {
		t.Fatalf("a rule with no matching device must not fail, got %q", got)
	}
	if statuses := e.stepStatuses(emptyRun.ID); statuses[0] != "0:action:skipped" {
		t.Fatalf("the step must be skipped, got %v", statuses)
	}

	// Every device is gone before the run executes.
	gone := e.createRule(e.withTargets(domain.TargetSpec{DeviceIDs: []string{"ghost-1", "ghost-2"}}))
	e.ingest("ev-a12-gone-1", e.dev1, "temperature", 35.0, "")
	goneRun := e.runs(gone.ID)[0]
	if got := e.run(goneRun.ID).Status; got == runtime.StatusSucceeded {
		t.Fatal("every target excluded must not be reported as success")
	}
	if statuses := e.stepStatuses(goneRun.ID); statuses[0] != "0:action:failed" {
		t.Fatalf("the step must fail when no target can be acted on, got %v", statuses)
	}
	if e.cmds.count() != 0 {
		t.Fatalf("no command may be sent to a device that does not exist, got %d", e.cmds.count())
	}
}

// A device deleted or moved between admission and execution is excluded with a
// reason, and the rest of the batch still runs.
func TestADeviceRemovedBeforeExecutionIsExcludedWithAReason(t *testing.T) {
	e := newEnv(t)
	// The wait is what opens the window: the snapshot is taken now, the action
	// runs a minute later, and the device disappears in between.
	def := e.withTargets(domain.TargetSpec{DeviceIDs: []string{e.dev1, e.dev2}})
	def.Steps = []domain.Step{
		{Kind: domain.StepWait, Wait: domain.Duration(time.Minute)},
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
	}
	rule := e.createRule(def)

	e.ingest("ev-a12-removed-1", e.dev1, "temperature", 35.0, "")
	runID := e.runs(rule.ID)[0].ID
	if e.cmds.count() != 0 {
		t.Fatalf("nothing may be sent before the wait elapses, got %d", e.cmds.count())
	}

	// The device is moved away while the run waits.
	delete(e.dir.devices, e.dev2)
	e.advance(2 * time.Minute)
	e.tick()

	targets := e.targets(runID)
	if targets[e.dev2] == nil {
		t.Fatalf("the frozen target must still be recorded, got %v", keys(targets))
	}
	if targets[e.dev2].Status != "excluded" || targets[e.dev2].ReasonCode != "device_unavailable" {
		t.Fatalf("a vanished device must be excluded with a reason, got %+v", targets[e.dev2])
	}
	if targets[e.dev1].Status != "accepted" {
		t.Fatalf("the remaining device must still be acted on, got %+v", targets[e.dev1])
	}
	if e.cmds.count() != 1 {
		t.Fatalf("only the reachable device may be commanded, got %d", e.cmds.count())
	}
	if got := e.run(runID).Status; got != runtime.StatusFailed {
		t.Fatalf("a batch that could not reach one of its devices must not be a success, got %q", got)
	}
}

// 200 targets, one unavailable: the batch is not a success and the failure is
// attributable to one device.
func TestOneBadTargetInABatchIsVisible(t *testing.T) {
	e := newEnv(t)
	var ids []string
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("%s-batch-%02d", e.dev1, i)
		e.addDevice(id, "batch", "sensor", "cfg-1")
		ids = append(ids, id)
	}
	ids = append(ids, "ghost-batch")
	rule := e.createRule(e.withTargets(domain.TargetSpec{DeviceIDs: ids, MaxTargets: 50}))

	e.ingest("ev-a12-batch-1", e.dev1, "temperature", 35.0, "")
	runID := e.runs(rule.ID)[0].ID

	targets := e.targets(runID)
	accepted, excluded := 0, 0
	for _, t := range targets {
		switch t.Status {
		case "accepted":
			accepted++
		case "excluded":
			excluded++
		}
	}
	if accepted != 20 || excluded != 1 {
		t.Fatalf("want 20 accepted and 1 excluded, got %d/%d", accepted, excluded)
	}
	if e.cmds.count() != 20 {
		t.Fatalf("only the existing devices may be commanded, got %d", e.cmds.count())
	}
	// Not "succeeded": a device in the batch was skipped. Not "failed" either:
	// the twenty devices that exist did take the action.
	if got := e.run(runID).Status; got != runtime.StatusPartial {
		t.Fatalf("one skipped target must make the run partial, got %q", got)
	}
}

// Label matching is explicit: AND needs every label, OR needs one.
func TestLabelMatchAllAndAny(t *testing.T) {
	e := newEnv(t)
	// One device carrying two labels, which is how the community label field is
	// actually filled in.
	e.addDevice(e.dev1+"-both", "floor-2, lighting", "sensor", "cfg-1")

	all := e.createRule(e.withTargets(domain.TargetSpec{
		Labels: []string{"floor-2", "lighting"}, LabelMatch: "and",
	}))
	e.ingest("ev-a12-label-and", e.dev1, "temperature", 35.0, "")
	if got := len(e.targets(e.runs(all.ID)[0].ID)); got != 1 {
		t.Fatalf("AND must select only the device with both labels, got %d", got)
	}

	any := e.createRule(e.withTargets(domain.TargetSpec{
		Labels: []string{"floor-1", "lighting"}, LabelMatch: "or",
	}))
	e.ingest("ev-a12-label-or", e.dev1, "temperature", 36.0, "")
	if got := len(e.targets(e.runs(any.ID)[0].ID)); got != 3 {
		t.Fatalf("OR must select floor-1 devices plus the lighting one, got %d", got)
	}
}

// Every instance of a device template is a target, which is the "按配置类设备"
// selection the old console offered.
func TestDeviceConfigTargetsCoverEveryInstance(t *testing.T) {
	e := newEnv(t)
	third := e.dev1 + "-cfg2"
	e.addDevice(third, "floor-1", "sensor", "cfg-2")

	rule := e.createRule(e.withTargets(domain.TargetSpec{DeviceConfigIDs: []string{"cfg-1"}}))
	e.ingest("ev-a12-cfg-1", e.dev1, "temperature", 35.0, "")

	targets := e.targets(e.runs(rule.ID)[0].ID)
	if len(targets) != 2 {
		t.Fatalf("cfg-1 has two instances, got %v", keys(targets))
	}
	if _, ok := targets[third]; ok {
		t.Fatal("a device of another configuration must not be selected")
	}
}

// A device of another tenant must not be resolved, and its details must not leak
// into the run record.
func TestADeviceOfAnotherTenantIsNotResolved(t *testing.T) {
	e := newEnv(t)
	foreign := "foreign-" + e.dev1
	e.dir.devices[foreign] = ports.Device{
		ID: foreign, TenantID: "other-tenant", Name: "别人的设备",
		Label: "floor-1", DeviceConfigID: "cfg-1", DeviceType: "sensor", Online: 1,
	}
	rule := e.createRule(e.withTargets(domain.TargetSpec{DeviceIDs: []string{foreign, e.dev1}}))

	e.ingest("ev-a12-tenant-1", e.dev1, "temperature", 35.0, "")
	targets := e.targets(e.runs(rule.ID)[0].ID)

	if t1 := targets[foreign]; t1 == nil || t1.Status != "excluded" {
		t.Fatalf("a foreign device must be excluded, got %+v", t1)
	}
	if t1 := targets[foreign]; t1.ReasonCode != "device_not_found" {
		t.Fatalf("the reason must not reveal that the device exists elsewhere, got %q", t1.ReasonCode)
	}
	if e.cmds.count() != 1 {
		t.Fatalf("only the own device may be commanded, got %d", e.cmds.count())
	}
}

// runIDByTrigger finds the run a given trigger produced. ListRuns returns newest
// first, so indexing into it would silently compare the wrong run.
func (e *testEnv) runIDByTrigger(ruleID, triggerID string) string {
	e.t.Helper()
	for _, r := range e.runs(ruleID) {
		if r.TriggerEventID == triggerID {
			return r.ID
		}
	}
	e.t.Fatalf("no run for trigger %s", triggerID)
	return ""
}

func keys(m map[string]*store.Target) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
