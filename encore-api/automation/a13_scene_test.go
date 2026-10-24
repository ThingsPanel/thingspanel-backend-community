package automation

import (
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/legacy"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A13 — legacy scenes keep working
//
// What these tests guard: the old button still runs the same actions in the same
// order; the alarm action goes through the alarm owner instead of being faked; and
// a state scene stays a one-shot state write with no hidden supervision.
// ---------------------------------------------------------------------------

// storeResolver is the resolver the migration endpoint builds, minus the HTTP
// layer: device and scene existence come from a supplied snapshot, alarm policies
// come from this service.
type storeResolver struct {
	e       *testEnv
	scenes  map[string]*legacy.Scene
	devices map[string]bool
	configs map[string]bool
}

func (s *storeResolver) SceneExists(id string) (bool, error)  { _, ok := s.scenes[id]; return ok, nil }
func (s *storeResolver) DeviceExists(id string) (bool, error) { return s.devices[id], nil }
func (s *storeResolver) DeviceConfigExists(id string) (bool, error) {
	return s.configs[id], nil
}
func (s *storeResolver) Load(id string) (*legacy.Scene, error) { return s.scenes[id], nil }

func (s *storeResolver) AlarmPolicyFor(legacyAlarmID string) (string, string, bool, error) {
	rule, err := s.e.st.GetRuleByLegacyID(s.e.ctx(), s.e.tenant, legacyAlarmID)
	if err != nil {
		return "", "", false, nil
	}
	if rule.Kind != domain.KindAlarm {
		return "", "", false, nil
	}
	return rule.ID, rule.PolicyKey(), true, nil
}

// legacyAlarmPolicy creates an alarm policy that only the event channel may
// qualify, and stamps the id it had in the old console.
func (e *testEnv) legacyAlarmPolicy(legacyID string) *store.Rule {
	e.t.Helper()
	def := domain.Definition{
		Kind:  domain.KindAlarm,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "30", Unit: "℃",
		}}}}},
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{e.dev1}},
		Steps: []domain.Step{{Kind: domain.StepNotify,
			NotificationPolicyID: "np-legacy", Value: "告警"}},
		Alarm: &domain.AlarmSpec{
			// Only the event channel: the old scene action is what raises it, and
			// a detector channel would create a second, competing way in.
			Key: "policy.legacy." + legacyID, Sources: []domain.AlarmSource{domain.AlarmSourceEvent},
			AllowedSources: []string{"automation"}, Level: "H",
		},
	}
	def.Normalize()
	if err := domain.Validate(&def); err != nil {
		e.t.Fatalf("legacy alarm policy is invalid: %v", err)
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: e.tenant, Name: "旧告警 " + legacyID,
		Kind: def.Kind, Enabled: true, Definition: def,
		DefinitionHash: domain.DefinitionHash(def), LegacyID: legacyID,
	}
	if err := e.st.CreateRule(e.ctx(), rule); err != nil {
		e.t.Fatalf("create legacy alarm policy: %v", err)
	}
	stored, err := e.st.GetRule(e.ctx(), e.tenant, rule.ID)
	if err != nil {
		e.t.Fatalf("reload: %v", err)
	}
	return stored
}

// TestAMigratedSceneKeepsItsOrderAndRaisesTheRealAlarm is the completion
// criterion: a manual run of an old scene with a device action and an alarm keeps
// the original order, and the alarm is produced by the alarm owner.
func TestAMigratedSceneKeepsItsOrderAndRaisesTheRealAlarm(t *testing.T) {
	e := newEnv(t)
	policy := e.legacyAlarmPolicy("alarm-legacy-1")

	res := &storeResolver{
		e: e, scenes: map[string]*legacy.Scene{},
		devices: map[string]bool{e.dev1: true}, configs: map[string]bool{},
	}
	scene := &legacy.Scene{ID: "scene-legacy-1", Name: "开门并告警", Actions: []legacy.Action{
		{ID: "act-1", Target: e.dev1, Type: legacy.TypeSingleDevice,
			ParamType: legacy.ParamTelemetry, Param: "switch", Value: "1"},
		{ID: "act-2", Target: "alarm-legacy-1", Type: legacy.TypeRaiseAlarm},
	}}
	res.scenes[scene.ID] = scene

	plan := legacy.Build(scene, res)
	if !plan.OK() {
		t.Fatalf("the scene must migrate: %v", plan.Issues)
	}
	if plan.Shape != legacy.ShapeSequence {
		t.Fatalf("a device action plus an alarm is a sequence, got %q", plan.Shape)
	}
	if len(plan.Mapping) != 2 || plan.Mapping[0].LegacyActionID != "act-1" ||
		plan.Mapping[1].LegacyActionID != "act-2" {
		t.Fatalf("the mapping must keep the original order: %+v", plan.Mapping)
	}

	rule := e.createRule(plan.Definition)
	before := e.cmds.count()
	run, err := e.rt.RunNow(e.ctx(), rule, e.dev1)
	if err != nil {
		t.Fatalf("run the migrated scene: %v", err)
	}
	if run.Status != runtime.StatusSucceeded {
		t.Fatalf("want succeeded, got %q (%s / %s)", run.Status, run.ReasonCode, run.Reason)
	}

	// The device action happened, in its original position.
	if e.cmds.count() != before+1 {
		t.Fatalf("want one command, got %d", e.cmds.count()-before)
	}
	if req, ok := e.cmds.last(); !ok || req.Identifier != "switch" || req.Value != "1" {
		t.Fatalf("wrong command: %+v", req)
	}
	// "accepted" is the terminal state of a device action: the command owner took
	// it, and that is as much as this service can honestly know. What matters here
	// is that the action ran before the alarm, in the original order.
	if got := e.stepStatuses(run.ID); len(got) != 2 ||
		got[0] != "0:action:accepted" || got[1] != "1:alarm:succeeded" {
		t.Fatalf("the steps must run in order and both take effect: %v", got)
	}

	// The alarm came from the alarm owner, not from the scene writing a row.
	ep := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if ep == nil {
		t.Fatal("the alarm action must raise a real alarm through the alarm owner")
	}
	if ep.Source != "event:automation" {
		t.Fatalf("the alarm must record its authorised channel, got %q", ep.Source)
	}
	if evs := e.eventsOfKind("alarm.qualified"); len(evs) != 1 {
		t.Fatalf("the transition must be announced once, got %d", len(evs))
	}
}

// TestRunningTheSameSceneTwiceRaisesTheAlarmOnce: pressing the button again is a
// second run with a new action id, and the alarm is already active — so the second
// press must not duplicate the alarm.
func TestRunningTheSameSceneTwiceRaisesTheAlarmOnce(t *testing.T) {
	e := newEnv(t)
	policy := e.legacyAlarmPolicy("alarm-legacy-2")
	res := &storeResolver{
		e: e, scenes: map[string]*legacy.Scene{},
		devices: map[string]bool{e.dev1: true}, configs: map[string]bool{},
	}
	scene := &legacy.Scene{ID: "s", Actions: []legacy.Action{
		{ID: "act-1", Target: e.dev1, Type: legacy.TypeSingleDevice,
			ParamType: legacy.ParamTelemetry, Param: "switch", Value: "1"},
		{ID: "act-2", Target: "alarm-legacy-2", Type: legacy.TypeRaiseAlarm},
	}}
	res.scenes["s"] = scene
	plan := legacy.Build(scene, res)
	if !plan.OK() {
		t.Fatalf("issues: %v", plan.Issues)
	}
	rule := e.createRule(plan.Definition)

	if _, err := e.rt.RunNow(e.ctx(), rule, e.dev1); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := e.rt.RunNow(e.ctx(), rule, e.dev1); err != nil {
		t.Fatalf("second run: %v", err)
	}

	eps := e.episodesOf(policy.PolicyKey())
	if len(eps) != 1 {
		t.Fatalf("two presses of the same button must keep one alarm, got %d", len(eps))
	}
	if evs := e.eventsOfKind("alarm.qualified"); len(evs) != 1 {
		t.Fatalf("want one announcement, got %d", len(evs))
	}
}

// TestAStateSceneIsOneShotAndImpliesNoSupervision: the old "put it in this state"
// button must stay exactly that — no persistent condition, no timer, no rollback.
func TestAStateSceneIsOneShotAndImpliesNoSupervision(t *testing.T) {
	e := newEnv(t)
	res := &storeResolver{
		e: e, scenes: map[string]*legacy.Scene{},
		devices: map[string]bool{e.dev1: true}, configs: map[string]bool{},
	}
	scene := &legacy.Scene{ID: "s", Actions: []legacy.Action{{
		ID: "act-1", Target: e.dev1, Type: legacy.TypeSingleDevice,
		ParamType: legacy.ParamTelemetry, Param: "switch", Value: "1",
	}}}
	res.scenes["s"] = scene
	plan := legacy.Build(scene, res)
	if !plan.OK() {
		t.Fatalf("issues: %v", plan.Issues)
	}
	if plan.Shape != legacy.ShapeStateScene {
		t.Fatalf("want state_scene, got %q", plan.Shape)
	}
	if len(plan.Definition.Trigger.Groups) != 0 {
		t.Fatal("a state scene must not gain a trigger, which would turn it into supervision")
	}
	if plan.Definition.Trigger.For.Std() != 0 {
		t.Fatal("a state scene must not gain a duration")
	}

	rule := e.createRule(plan.Definition)
	run, err := e.rt.RunNow(e.ctx(), rule, e.dev1)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != runtime.StatusSucceeded {
		t.Fatalf("want succeeded, got %q", run.Status)
	}
	// No waiting means no timer row and no pending condition: the scene is done
	// when the last action is done.
	if timers, err := e.st.DueRunTimers(e.ctx(), e.now.Add(time.Hour), 10); err != nil {
		t.Fatalf("timers: %v", err)
	} else {
		for _, tm := range timers {
			if tm.RunID == run.ID {
				t.Fatalf("a state scene must not leave a wait behind: %+v", tm)
			}
		}
	}
	var pending int
	if err := e.st.DB.QueryRowContext(e.ctx(),
		`SELECT count(*) FROM auto_pending WHERE tenant_id=$1`, e.tenant).Scan(&pending); err != nil {
		t.Fatalf("pending: %v", err)
	}
	if pending != 0 {
		t.Fatalf("a state scene must not open a pending condition, got %d", pending)
	}
}

// TestAnUnresolvedAlarmActionFailsTheMigration is "查不到动作明确失败": the scene
// must not publish with the alarm silently dropped.
func TestAnUnresolvedAlarmActionFailsTheMigration(t *testing.T) {
	e := newEnv(t)
	res := &storeResolver{
		e: e, scenes: map[string]*legacy.Scene{},
		devices: map[string]bool{e.dev1: true}, configs: map[string]bool{},
	}
	scene := &legacy.Scene{ID: "s", Actions: []legacy.Action{
		{ID: "act-1", Target: e.dev1, Type: legacy.TypeSingleDevice,
			ParamType: legacy.ParamTelemetry, Param: "switch", Value: "1"},
		{ID: "act-2", Target: "alarm-never-migrated", Type: legacy.TypeRaiseAlarm},
	}}
	plan := legacy.Build(scene, res)
	if plan.OK() {
		t.Fatal("an alarm reference that was never migrated must fail the plan")
	}
	found := false
	for _, i := range plan.Issues {
		if i.Code == "action_unresolved" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want action_unresolved, got %v", plan.Issues)
	}
}

// TestASceneCycleIsRefusedBeforeMigration: two scenes activating each other must
// be refused at migration time, not discovered by a runaway at 3am.
func TestASceneCycleIsRefusedBeforeMigration(t *testing.T) {
	e := newEnv(t)
	res := &storeResolver{
		e: e, scenes: map[string]*legacy.Scene{},
		devices: map[string]bool{e.dev1: true}, configs: map[string]bool{},
	}
	a := &legacy.Scene{ID: "scene-a", Actions: []legacy.Action{
		{ID: "a1", Target: e.dev1, Type: legacy.TypeSingleDevice,
			ParamType: legacy.ParamTelemetry, Param: "switch", Value: "1"},
		{ID: "a2", Target: "scene-b", Type: legacy.TypeActivateScene},
	}}
	b := &legacy.Scene{ID: "scene-b", Actions: []legacy.Action{
		{ID: "b1", Target: "scene-a", Type: legacy.TypeActivateScene},
	}}
	res.scenes["scene-a"] = a
	res.scenes["scene-b"] = b

	// Every individual action resolves, so only the graph walk can catch this.
	if p := legacy.Build(a, res); !p.OK() {
		t.Fatalf("the rows themselves are fine: %v", p.Issues)
	}
	path, iss := legacy.CheckCycles("scene-a", res)
	if iss == nil || iss.Code != "scene_cycle" {
		t.Fatalf("want scene_cycle, got %v", iss)
	}
	if len(path) != 3 {
		t.Fatalf("want scene-a -> scene-b -> scene-a, got %v", path)
	}
}
