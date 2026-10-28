package automation

import (
	"context"
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
)

// ---------------------------------------------------------------------------
// A25: side-effect-free simulation and change preview
// ---------------------------------------------------------------------------
//
// Two questions. First: can a customer see what a rule would do without the
// rule doing it? Second: can they see what an edit would touch before saving?
//
// The tests for the first question are only convincing if the adapters would
// make noise when called, so the simulation tests below run against adapters
// that fail the test if they are ever reached.

// explodingCommands fails the test if the simulation reaches a device.
type explodingCommands struct{ t *testing.T }

func (f *explodingCommands) Dispatch(context.Context, ports.CommandRequest) (ports.CommandResult, error) {
	f.t.Error("the simulation called the command adapter")
	return ports.CommandResult{}, nil
}

func (f *explodingCommands) Status(context.Context, string) (string, error) {
	f.t.Error("the simulation called the command adapter")
	return "", nil
}

func (f *explodingCommands) ActivateScene(context.Context, string, string) (string, string, string, error) {
	f.t.Error("the simulation called the scene adapter")
	return "", "", "", nil
}

// explodingNotifier fails the test if the simulation reaches the notification core.
type explodingNotifier struct{ t *testing.T }

func (f *explodingNotifier) Submit(context.Context, ports.NotificationRequest) (ports.NotificationResult, error) {
	f.t.Error("the simulation called the notification adapter")
	return ports.NotificationResult{}, nil
}

// simRuntime wires a runtime whose outbound ports are tripwires.
func (e *testEnv) simRuntime() *runtime.Runtime {
	e.t.Helper()
	return runtime.New(e.st, &explodingCommands{t: e.t}, &explodingNotifier{t: e.t},
		e.dir, func() time.Time { return e.now })
}

// TestTheSimulationNeverCallsTheWriteExits is the criterion "模拟不能调用命令/
// 正式告警/通知的写入出口", checked by making those exits fail loudly.
func TestTheSimulationNeverCallsTheWriteExits(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a25-1-" + runtime.NewID()[:8])
	def := thresholdRule(dev, "30", 0)
	def.Steps = []domain.Step{
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1"},
		{Kind: domain.StepNotify, NotificationPolicyID: "pol-a25", Value: "高温"},
		{Kind: domain.StepAlarm, AlarmPolicyID: "policy-a25", DeviceRef: "target", Value: "qualified"},
	}
	rule := e.createNamedRuleIn(serviceTestTenant, "会写三个出口的规则", def)

	res, err := e.simRuntime().Simulate(e.ctx(), serviceTestTenant, rule.ID, []runtime.SimSample{
		{At: "0s", DeviceID: dev, Signal: "telemetry", Key: "temperature", Value: 40.0},
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if res.SideEffects != 0 {
		t.Fatalf("a simulation must have no side effects, got %d", res.SideEffects)
	}
	if res.WouldRun != 1 {
		t.Fatalf("the rule should have fired once, got %d", res.WouldRun)
	}
	// Every planned action is described, and none of them is a real call.
	if len(res.Actions) < 3 {
		t.Fatalf("every step should have a readable result, got %+v", res.Actions)
	}
	for _, a := range res.Actions {
		if a.Outcome != "would_execute" {
			t.Fatalf("step %s should be planned for execution, got %q", a.Path, a.Outcome)
		}
		if a.Detail == "" {
			t.Fatalf("step %s has no readable result", a.Path)
		}
	}
}

// TestTheSimulationWalksTheBranchItWouldTake: a simulation that lists both
// branches is worse than none, because it is confidently wrong.
func TestTheSimulationWalksTheBranchItWouldTake(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a25-2-" + runtime.NewID()[:8])
	def := thresholdRule(dev, "30", 0)
	def.Steps = []domain.Step{{
		Kind: domain.StepBranch,
		Condition: &domain.Group{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "50", Unit: "℃",
		}}},
		Then: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "on"}},
		Else: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "off"}},
	}}
	rule := e.createNamedRuleIn(serviceTestTenant, "带分支的规则", def)

	hot, err := e.simRuntime().Simulate(e.ctx(), serviceTestTenant, rule.ID, []runtime.SimSample{
		{At: "0s", DeviceID: dev, Signal: "telemetry", Key: "temperature", Value: 60.0},
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if !hasAction(hot.Actions, "fan=on") {
		t.Fatalf("the then branch should be planned: %+v", hot.Actions)
	}
	if hasAction(hot.Actions, "fan=off") {
		t.Fatalf("the else branch must not be planned: %+v", hot.Actions)
	}

	cool, err := e.simRuntime().Simulate(e.ctx(), serviceTestTenant, rule.ID, []runtime.SimSample{
		{At: "0s", DeviceID: dev, Signal: "telemetry", Key: "temperature", Value: 40.0},
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if !hasAction(cool.Actions, "fan=off") || hasAction(cool.Actions, "fan=on") {
		t.Fatalf("with the condition false only the else branch runs: %+v", cool.Actions)
	}
}

// TestTheSimulationStopsAtAGuard: a guard is an interlock, and the simulation
// has to stop where the executor would stop.
func TestTheSimulationStopsAtAGuard(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a25-3-" + runtime.NewID()[:8])
	def := thresholdRule(dev, "30", 0)
	def.Steps = []domain.Step{
		{
			Kind: domain.StepAction, Action: "telemetry", Identifier: "valve", Value: "open",
			Guard: &domain.Group{Conditions: []domain.Condition{{
				Source: "self", Signal: domain.SignalTelemetry, Key: "interlock",
				Operator: domain.OpEq, Value: "released",
			}}},
		},
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "pump", Value: "on"},
	}
	rule := e.createNamedRuleIn(serviceTestTenant, "带互锁的规则", def)

	res, err := e.simRuntime().Simulate(e.ctx(), serviceTestTenant, rule.ID, []runtime.SimSample{
		{At: "0s", DeviceID: dev, Signal: "telemetry", Key: "temperature", Value: 40.0},
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if len(res.Actions) != 1 || res.Actions[0].Outcome != "blocked" {
		t.Fatalf("the guarded step must be blocked and the rest skipped: %+v", res.Actions)
	}
	if hasAction(res.Actions, "pump=on") {
		t.Fatalf("nothing after a blocked guard runs: %+v", res.Actions)
	}
}

// TestTheSimulationAdvancesTheVirtualClockForWaits: without this, a rule that
// waits five minutes looks instantaneous and the budget check is meaningless.
func TestTheSimulationAdvancesTheVirtualClockForWaits(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a25-4-" + runtime.NewID()[:8])
	def := thresholdRule(dev, "30", 0)
	def.Steps = []domain.Step{
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "valve", Value: "open"},
		{Kind: domain.StepWait, Wait: domain.Duration(5 * time.Minute)},
		{Kind: domain.StepAction, Action: "telemetry", Identifier: "valve", Value: "close"},
	}
	rule := e.createNamedRuleIn(serviceTestTenant, "带等待的规则", def)

	res, err := e.simRuntime().Simulate(e.ctx(), serviceTestTenant, rule.ID, []runtime.SimSample{
		{At: "0s", DeviceID: dev, Signal: "telemetry", Key: "temperature", Value: 40.0},
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if res.EndsAt != "5m0s" {
		t.Fatalf("the virtual clock should advance by the wait, got %q", res.EndsAt)
	}
}

// TestTheSimulationAgreesWithTheRealEvaluation is "同一输入与时序在模拟和实际
// 评估逻辑一致": the same sample either fires in both, or in neither.
func TestTheSimulationAgreesWithTheRealEvaluation(t *testing.T) {
	e := newEnv(t)
	def := thresholdRule(e.dev1, "30", 0)
	rule := e.createRule(def)

	e.ingest("ev-a25-parity", e.dev1, "temperature", 40.0, "")
	realRuns := e.runs(rule.ID)
	if len(realRuns) != 1 {
		t.Fatalf("the real path should have run once, got %d", len(realRuns))
	}

	res, err := e.simRuntime().Simulate(e.ctx(), e.tenant, rule.ID, []runtime.SimSample{
		{At: "0s", DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: 40.0},
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if res.WouldRun != len(realRuns) {
		t.Fatalf("simulation says %d runs, reality says %d", res.WouldRun, len(realRuns))
	}

	// And below the threshold, neither fires.
	quiet, err := e.simRuntime().Simulate(e.ctx(), e.tenant, rule.ID, []runtime.SimSample{
		{At: "0s", DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: 10.0},
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if quiet.WouldRun != 0 {
		t.Fatalf("a sample below the threshold must not fire, got %d", quiet.WouldRun)
	}
}

// ---------------------------------------------------------------------------
// Change preview
// ---------------------------------------------------------------------------

// TestThePreviewShowsEveryDeviceTheChangeWouldTouch: a template rule's target
// list says nothing, so a preview that only reads the definition reports "one
// device" while forty are affected.
func TestThePreviewShowsEveryDeviceTheChangeWouldTouch(t *testing.T) {
	e := newEnv(t)
	cfg := "cfg-a25-" + runtime.NewID()[:8]
	d1 := e.templateDevice("dev-a25-a-" + runtime.NewID()[:8])
	d2 := e.templateDevice("dev-a25-b-" + runtime.NewID()[:8])
	for _, id := range []string{d1, d2} {
		dev := e.dir.devices[id]
		dev.DeviceConfigID = cfg
		e.dir.devices[id] = dev
	}

	def := thresholdRule(d1, "30", 0)
	// A template rule names the configuration, not the devices: that is exactly
	// why a preview that only reads the definition reports the wrong scope.
	def.Targets = domain.TargetSpec{DeviceConfigIDs: []string{cfg}}
	rule := e.createNamedRuleIn(serviceTestTenant, "模板规则", def)
	e.bind(rule, "device_config", cfg, map[string]string{"limit": "30"})

	next := thresholdRule(d1, "40", 0)
	next.Targets = domain.TargetSpec{DeviceConfigIDs: []string{cfg}}
	preview, err := e.svc().PreviewRuleChange(e.ctx(), rule.ID, &ChangePreviewRequest{
		Definition: next,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Scope.DeviceCount != 2 {
		t.Fatalf("both devices of the template are affected, got %d (%v)",
			preview.Scope.DeviceCount, preview.Scope.DeviceIDs)
	}
	if !preview.Changed || preview.HashBefore == preview.HashAfter {
		t.Fatal("the preview should report that the body changed")
	}
	if !preview.RequiresConfirmation {
		t.Fatal("a change touching more than one device should ask first")
	}
	if preview.RunPolicy.Mode == "" || preview.RunPolicy.Deadline == "" {
		t.Fatalf("the preview must state the run policy: %+v", preview.RunPolicy)
	}
	if preview.Token == "" || preview.ExpiresAt.IsZero() {
		t.Fatal("a preview without a token cannot be applied safely")
	}
}

// TestASaveWithAStalePreviewIsRefused: "过期计划…不能继续套用旧预览".
func TestASaveWithAStalePreviewIsRefused(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a25-5-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "会被别人改的规则", thresholdRule(dev, "30", 0))

	body := thresholdRule(dev, "40", 0)
	preview, err := e.svc().PreviewRuleChange(e.ctx(), rule.ID, &ChangePreviewRequest{Definition: body})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	// Somebody else saves first, which bumps the version.
	if _, err := e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Name: "被别人改过", Definition: thresholdRule(dev, "35", 0), Version: rule.Version,
	}); err != nil {
		t.Fatalf("intervening save: %v", err)
	}

	_, err = e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Definition: body, Version: rule.Version + 1, PreviewToken: preview.Token,
	})
	if err == nil {
		t.Fatal("a preview computed before somebody else's save must not be applied")
	}
	if code := errs.Code(err); code != errs.FailedPrecondition {
		t.Fatalf("want failed_precondition, got %v (%v)", code, err)
	}
	if meta, _ := errs.Meta(err)["code"].(string); meta != "preview_stale" {
		t.Fatalf("want preview_stale, got %v", errs.Meta(err))
	}
}

// TestASaveWithADifferentBodyThanThePreviewIsRefused: the token is bound to the
// body, not just to the rule.
func TestASaveWithADifferentBodyThanThePreviewIsRefused(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a25-6-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "表单被改过的规则", thresholdRule(dev, "30", 0))

	preview, err := e.svc().PreviewRuleChange(e.ctx(), rule.ID, &ChangePreviewRequest{
		Definition: thresholdRule(dev, "40", 0),
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	_, err = e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Definition: thresholdRule(dev, "45", 0), Version: rule.Version,
		PreviewToken: preview.Token,
	})
	if err == nil {
		t.Fatal("the preview was for a different body and must not be applied")
	}
	if meta, _ := errs.Meta(err)["code"].(string); meta != "preview_stale" {
		t.Fatalf("want preview_stale, got %v", errs.Meta(err))
	}
}

// TestASaveThatWouldOrphanDeviceValuesIsRefused: the plain save does its own
// check, so the customer does not have to remember to preview.
func TestASaveThatWouldOrphanDeviceValuesIsRefused(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a25-7-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "有设备覆盖值的规则", thresholdRule(dev, "30", 0))
	e.bind(rule, "device", dev, map[string]string{"limit": "55"})

	narrow := thresholdRule(dev, "5", 0)
	narrow.Params[0].Max = floatPtr(10)

	_, err := e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Definition: narrow, Version: rule.Version,
	})
	if err == nil {
		t.Fatal("a save that orphans a device's value must be refused")
	}
	if meta, _ := errs.Meta(err)["code"].(string); meta != "binding_incompatible" {
		t.Fatalf("want binding_incompatible, got %v", errs.Meta(err))
	}

	// Saying "yes, I know" works, and the binding is left exactly as it was.
	if _, err := e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Definition: narrow, Version: rule.Version, Force: true,
	}); err != nil {
		t.Fatalf("forced save: %v", err)
	}
	bindings, err := e.st.BindingsForDevice(e.ctx(), serviceTestTenant, dev, "")
	if err != nil {
		t.Fatalf("bindings: %v", err)
	}
	found := false
	for _, b := range bindings {
		if b.RuleID == rule.ID {
			found = true
			if b.Params["limit"] != "55" {
				t.Fatalf("the save must not rewrite the device's value: %v", b.Params)
			}
		}
	}
	if !found {
		t.Fatal("the binding disappeared")
	}
}

// hasAction reports whether any planned action's detail contains want.
func hasAction(actions []runtime.SimAction, want string) bool {
	for _, a := range actions {
		if a.Outcome == "would_execute" && strings.Contains(a.Detail, want) {
			return true
		}
	}
	return false
}

// TestAPreviewStillAppliesAfterTheClockTicks is the bug the CLI found and the
// tests could not: the token used to be a hash *including* the expiry, and
// every recheck computed a fresh expiry, so the digest never matched and every
// save was refused as stale. A frozen test clock hid it completely.
func TestAPreviewStillAppliesAfterTheClockTicks(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a25-8-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "慢慢看再保存", thresholdRule(dev, "30", 0))
	body := thresholdRule(dev, "40", 0)

	preview, err := e.svc().PreviewRuleChange(e.ctx(), rule.ID, &ChangePreviewRequest{Definition: body})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	// The user reads the page for a minute and a half, then saves.
	e.now = e.now.Add(90 * time.Second)

	if _, err := e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Definition: body, Version: rule.Version, PreviewToken: preview.Token,
	}); err != nil {
		t.Fatalf("a preview taken 90s ago must still apply: %v", err)
	}
}

// TestAnExpiredPreviewIsRefused: the other half — the TTL still means something.
func TestAnExpiredPreviewIsRefused(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a25-9-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "放了太久的预览", thresholdRule(dev, "30", 0))
	body := thresholdRule(dev, "40", 0)

	preview, err := e.svc().PreviewRuleChange(e.ctx(), rule.ID, &ChangePreviewRequest{Definition: body})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	e.now = e.now.Add(previewTTL + time.Minute)

	_, err = e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Definition: body, Version: rule.Version, PreviewToken: preview.Token,
	})
	if err == nil {
		t.Fatal("an expired preview must not be applied")
	}
	if meta, _ := errs.Meta(err)["code"].(string); meta != "preview_expired" {
		t.Fatalf("want preview_expired, got %v", errs.Meta(err))
	}
}
