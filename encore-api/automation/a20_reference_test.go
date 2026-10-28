package automation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A20: references, invalidation and tombstones
// ---------------------------------------------------------------------------
//
// The question behind every test here is the same: when the thing a
// configuration points at disappears, does anything keep firing from it, and
// can the customer still find out why it stopped? A row that silently stops
// matching is worse than an error, because nothing tells anyone.

// thresholdRule is a device-scoped automation whose threshold is a parameter and
// whose condition has to hold for a while. Both halves matter here: the
// parameter is what a binding supplies, and the duration is what creates a
// pending window that could outlive its rule.
func thresholdRule(deviceID, defaultValue string, forDur time.Duration) domain.Definition {
	limit := 100.0
	return domain.Definition{
		Kind:  domain.KindAutomation,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Params: []domain.Param{{
			Key: "limit", Type: domain.ParamNumber, Default: defaultValue,
			Min: floatPtr(0), Max: &limit, Description: "触发阈值", Unit: "℃",
		}},
		Trigger: domain.Trigger{
			For: domain.Duration(forDur),
			Groups: []domain.Group{{Conditions: []domain.Condition{{
				Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
				Operator: domain.OpGt, Value: "{{limit}}", Unit: "℃",
			}}}},
		},
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{deviceID}},
		Steps: []domain.Step{{
			Kind: domain.StepAction, Action: "telemetry", Identifier: "switch", Value: "1",
		}},
	}
}

// sceneCalling is a rule whose only step activates another rule.
//
// It is how the legacy "激活场景" action is stored: the referenced id lives in
// the step's value. A20 has to see that reference, because deleting the target
// would otherwise leave a step that can never succeed.
func sceneCalling(deviceID, targetID string) domain.Definition {
	return domain.Definition{
		Kind:  domain.KindAutomation,
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
		Targets: domain.TargetSpec{DeviceIDs: []string{deviceID}},
		Steps: []domain.Step{{
			Kind: domain.StepActivateScene, Value: targetID,
		}},
	}
}

// createNamedRuleIn stores a rule under an explicit tenant and name, so a test
// can talk about "the rule that references it" by name instead of by id.
func (e *testEnv) createNamedRuleIn(tenant, name string, def domain.Definition) *store.Rule {
	e.t.Helper()
	def.Normalize()
	if err := domain.Validate(&def); err != nil {
		e.t.Fatalf("test rule %q is invalid: %v", name, err)
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: tenant, Name: name, Kind: def.Kind,
		Enabled: true, Definition: def, DefinitionHash: domain.DefinitionHash(def),
	}
	if err := e.st.CreateRule(e.ctx(), rule); err != nil {
		e.t.Fatalf("create rule %q: %v", name, err)
	}
	stored, err := e.st.GetRule(e.ctx(), tenant, rule.ID)
	if err != nil {
		e.t.Fatalf("reload rule %q: %v", name, err)
	}
	return stored
}

// ingestFor reports a reading for an explicit tenant.
func (e *testEnv) ingestFor(tenant, eventID, device, key string, value any) {
	e.t.Helper()
	_, err := e.rt.Ingest(e.ctx(), store.IngestEvent{
		EventID: eventID, TenantID: tenant, DeviceID: device,
		Signal: domain.SignalTelemetry, Key: key, Value: value, Unit: "℃",
		Quality: domain.QualityGood, ObservedAt: e.now,
	})
	if err != nil {
		e.t.Fatalf("ingest: %v", err)
	}
}

// pendingFor counts the open windows of one rule.
func (e *testEnv) pendingFor(tenant, ruleID string) int {
	e.t.Helper()
	var n int
	if err := e.st.DB.QueryRowContext(e.ctx(),
		`SELECT count(*) FROM auto_pending WHERE tenant_id=$1 AND rule_id=$2`,
		tenant, ruleID).Scan(&n); err != nil {
		e.t.Fatalf("count pending: %v", err)
	}
	return n
}

// orphanOf picks this test's binding out of the report.
//
// The service-level endpoints all share one tenant, so a report is a picture of
// the whole tenant, not of this test. Asserting on totals would make the tests
// order-dependent; asserting on the row that belongs to the binding under test
// keeps them independent.
func orphanOf(resp *BindingReconcileResponse, bindingID string) *OrphanBindingResponse {
	for i := range resp.Orphans {
		if resp.Orphans[i].BindingID == bindingID {
			return &resp.Orphans[i]
		}
	}
	return nil
}

// runsIn lists a rule's runs for an explicit tenant. e.runs is bound to the
// per-test tenant, and the service-level endpoints all run under the fixed one.
func (e *testEnv) runsIn(tenant, ruleID string) []*store.Run {
	e.t.Helper()
	runs, err := e.st.ListRuns(e.ctx(), tenant, ruleID, "", 100)
	if err != nil {
		e.t.Fatalf("list runs: %v", err)
	}
	return runs
}

// storedDeleteReason reads the tombstone reason straight from the row, because
// the whole point of it is that it survives the API that wrote it.
func (e *testEnv) storedDeleteReason(tenant, ruleID string) string {
	e.t.Helper()
	var reason *string
	if err := e.st.DB.QueryRowContext(e.ctx(),
		`SELECT delete_reason FROM auto_rules WHERE tenant_id=$1 AND id=$2`,
		tenant, ruleID).Scan(&reason); err != nil {
		e.t.Fatalf("read delete_reason: %v", err)
	}
	if reason == nil {
		return ""
	}
	return *reason
}

// TestDeletingARuleStopsItsBindingsAndSaysWhyItWent is the core promise of A20:
// deleting configuration stops what would happen next, and leaves an answer to
// "why did this stop".
func TestDeletingARuleStopsItsBindingsAndSaysWhyItWent(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a20-del-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "门口高温", thresholdRule(dev, "30", 0))
	binding := e.bind(rule, "device", dev, map[string]string{"limit": "30"})

	resp, err := e.svc().DeleteRule(e.ctx(), rule.ID, nil)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if resp.RuleName != "门口高温" {
		t.Fatalf("the response should name what was deleted, got %q", resp.RuleName)
	}
	if resp.DetachedBindings != 1 {
		t.Fatalf("want one detached binding, got %d", resp.DetachedBindings)
	}

	after, err := e.st.GetBinding(e.ctx(), serviceTestTenant, binding.ID)
	if err != nil {
		t.Fatalf("the binding row must survive so the device page can explain it: %v", err)
	}
	if after.Enabled {
		t.Fatal("a deleted rule's binding must stop matching")
	}
	if after.DetachedAt == nil {
		t.Fatal("a detached binding must record when it was detached")
	}
	if reason := e.storedDeleteReason(serviceTestTenant, rule.ID); reason != "deleted_by_user" {
		t.Fatalf("want a tombstone reason, got %q", reason)
	}
}

// TestDeletingARuleLeavesNoGhostTimer is the "无幽灵 timer" criterion: a window
// that was open when the rule went away must not fire later.
func TestDeletingARuleLeavesNoGhostTimer(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a20-timer-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "持续高温", thresholdRule(dev, "30", time.Minute))
	e.bind(rule, "device", dev, map[string]string{"limit": "30"})

	// Above the threshold: the window opens and starts waiting for its minute.
	e.ingestFor(serviceTestTenant, "ev-a20-1", dev, "temperature", 40.0)
	if n := e.pendingFor(serviceTestTenant, rule.ID); n != 1 {
		t.Fatalf("want one open window before the delete, got %d", n)
	}

	if _, err := e.svc().DeleteRule(e.ctx(), rule.ID, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := e.pendingFor(serviceTestTenant, rule.ID); n != 0 {
		t.Fatalf("the delete must clear the open window, %d left", n)
	}

	// Well past the deadline, with the device still reporting: nothing may fire.
	e.advance(10 * time.Minute)
	e.ingestFor(serviceTestTenant, "ev-a20-2", dev, "temperature", 41.0)
	e.tick()
	e.advance(10 * time.Minute)
	e.tick()

	if runs := e.runsIn(serviceTestTenant, rule.ID); len(runs) != 0 {
		t.Fatalf("a deleted rule must not run: %d run(s)", len(runs))
	}
}

// TestADeletedRulesRunsStillExplainThemselves: the delete stops the future and
// keeps the past. A run that happened while the rule existed must still say
// what it did and under which parameter values.
func TestADeletedRulesRunsStillExplainThemselves(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a20-hist-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "会留痕的规则", thresholdRule(dev, "30", 0))
	e.bind(rule, "device", dev, map[string]string{"limit": "30"})

	e.ingestFor(serviceTestTenant, "ev-a20-h1", dev, "temperature", 45.0)
	before := e.runsIn(serviceTestTenant, rule.ID)
	if len(before) != 1 {
		t.Fatalf("want the rule to have run once, got %d", len(before))
	}

	if _, err := e.svc().DeleteRule(e.ctx(), rule.ID, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}

	after := e.runsIn(serviceTestTenant, rule.ID)
	if len(after) != 1 {
		t.Fatalf("a delete must not rewrite history: %d run(s) left", len(after))
	}
	if after[0].ID != before[0].ID || after[0].Status != before[0].Status {
		t.Fatalf("the run changed identity or verdict: %+v -> %+v", before[0], after[0])
	}
	params, _ := snapshotOf(t, after[0])["binding_params"].(map[string]any)
	if params["limit"] != "30" {
		t.Fatalf("the run must still say which values it ran under: %v", params)
	}
}

// TestDeletingAReferencedRuleIsRefusedAndNamesTheReferencer: "被引用的场景删除
// 不会留下静默失效链接". The delete is refused, and the message names the rule
// that would have been broken — a bare "in use" would leave the operator
// hunting.
func TestDeletingAReferencedRuleIsRefusedAndNamesTheReferencer(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a20-ref-" + runtime.NewID()[:8])
	target := e.createNamedRuleIn(serviceTestTenant, "被调用的场景", thresholdRule(dev, "30", 0))
	caller := e.createNamedRuleIn(serviceTestTenant, "调用它的场景", sceneCalling(dev, target.ID))

	_, err := e.svc().DeleteRule(e.ctx(), target.ID, nil)
	if err == nil {
		t.Fatal("deleting a referenced rule must be refused")
	}
	if code := errs.Code(err); code != errs.FailedPrecondition {
		t.Fatalf("want failed_precondition, got %v (%v)", code, err)
	}
	if !strings.Contains(err.Error(), "调用它的场景") {
		t.Fatalf("the refusal must name the referencer, got %q", err.Error())
	}

	// Refused means refused: the rule is still there and still deletable later.
	if _, gerr := e.st.GetRule(e.ctx(), serviceTestTenant, target.ID); gerr != nil {
		t.Fatalf("a refused delete must leave the rule alone: %v", gerr)
	}
	_ = caller
}

// TestForcedDeleteRecordsThatTheReferenceWasLeftBroken: an operator may still
// choose to break a reference. The difference between "chose to" and "did not
// notice" has to survive in the row.
func TestForcedDeleteRecordsThatTheReferenceWasLeftBroken(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a20-force-" + runtime.NewID()[:8])
	target := e.createNamedRuleIn(serviceTestTenant, "被强删的场景", thresholdRule(dev, "30", 0))
	e.createNamedRuleIn(serviceTestTenant, "仍然指向它的场景", sceneCalling(dev, target.ID))

	resp, err := e.svc().DeleteRule(e.ctx(), target.ID, &DeleteRuleRequest{Force: true})
	if err != nil {
		t.Fatalf("forced delete: %v", err)
	}
	if !resp.Forced {
		t.Fatal("the response must admit that a live reference was overridden")
	}
	if reason := e.storedDeleteReason(serviceTestTenant, target.ID); !strings.Contains(reason, "forced") {
		t.Fatalf("the tombstone must record the override, got %q", reason)
	}
}

// TestAnotherTenantCannotDeleteThisTenantsRule: 越租户旧任务被拒. The tenant comes
// from the auth layer, so a rule id from elsewhere must look like it does not
// exist rather than like something that can be deleted.
func TestAnotherTenantCannotDeleteThisTenantsRule(t *testing.T) {
	e := newEnv(t)
	// e.tenant is deliberately not the tenant the service runs under.
	foreign := e.createRule(thresholdRule(e.dev1, "30", 0))

	_, err := e.svc().DeleteRule(e.ctx(), foreign.ID, nil)
	if err == nil {
		t.Fatal("a rule from another tenant must not be deletable")
	}
	if code := errs.Code(err); code != errs.NotFound {
		t.Fatalf("want not_found, got %v (%v)", code, err)
	}
	if _, gerr := e.st.GetRule(e.ctx(), e.tenant, foreign.ID); gerr != nil {
		t.Fatalf("the foreign rule must be untouched: %v", gerr)
	}
}

// TestTheReconciliationReportsOrphansWithoutTouchingThem: opening a page must
// not delete anything. The dry run is the default on purpose.
func TestTheReconciliationReportsOrphansWithoutTouchingThem(t *testing.T) {
	e := newEnv(t)
	gone := "dev-a20-gone-" + runtime.NewID()[:8]
	rule := e.createNamedRuleIn(serviceTestTenant, "指向已删设备", thresholdRule(gone, "30", 0))
	binding := e.bind(rule, "device", gone, map[string]string{"limit": "30"})

	resp, err := e.svc().ReconcileBindingRefs(e.ctx(), nil)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	row := orphanOf(resp, binding.ID)
	if row == nil {
		t.Fatalf("the report must name the orphan binding %s: %+v", binding.ID, resp.Orphans)
	}
	if row.DeviceID != gone || row.RuleName != "指向已删设备" {
		t.Fatalf("the report must identify what it found: %+v", row)
	}
	if row.Detached || resp.Applied {
		t.Fatal("a dry run must not change anything")
	}
	still, err := e.st.GetBinding(e.ctx(), serviceTestTenant, binding.ID)
	if err != nil || !still.Enabled {
		t.Fatalf("a dry run must leave the binding enabled: %v %+v", err, still)
	}
}

// TestTheReconciliationDetachesWhenAskedTo is the other half: the same call with
// apply=true actually stops them.
func TestTheReconciliationDetachesWhenAskedTo(t *testing.T) {
	e := newEnv(t)
	gone := "dev-a20-gone2-" + runtime.NewID()[:8]
	rule := e.createNamedRuleIn(serviceTestTenant, "指向已删设备2", thresholdRule(gone, "30", 0))
	binding := e.bind(rule, "device", gone, map[string]string{"limit": "30"})

	resp, err := e.svc().ReconcileBindingRefs(e.ctx(), &ReconcileBindingsRequest{Apply: true})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	row := orphanOf(resp, binding.ID)
	if row == nil || !row.Detached {
		t.Fatalf("the applied reconcile must detach binding %s: %+v", binding.ID, resp.Orphans)
	}
	after, err := e.st.GetBinding(e.ctx(), serviceTestTenant, binding.ID)
	if err != nil {
		t.Fatalf("the row must survive: %v", err)
	}
	if after.Enabled || after.DetachedAt == nil {
		t.Fatalf("an applied reconcile must detach: %+v", after)
	}
}

// TestTheReconciliationRefusesToGuessWhenTheDirectoryFails: "device is gone" and
// "directory is down" are different facts. Treating the second as the first
// would detach every binding in the tenant during a directory outage.
func TestTheReconciliationRefusesToGuessWhenTheDirectoryFails(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a20-ok-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "正常绑定", thresholdRule(dev, "30", 0))
	binding := e.bind(rule, "device", dev, map[string]string{"limit": "30"})

	e.dir.err = errors.New("directory timeout")
	resp, err := e.svc().ReconcileBindingRefs(e.ctx(), &ReconcileBindingsRequest{Apply: true})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if resp.Detached != 0 || len(resp.Orphans) != 0 {
		t.Fatalf("a failed directory must not produce orphans: %+v", resp)
	}
	found := false
	for _, i := range resp.Issues {
		if i.Code == "directory_unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the caller has to be told why nothing happened: %v", resp.Issues)
	}
	after, err := e.st.GetBinding(e.ctx(), serviceTestTenant, binding.ID)
	if err != nil || !after.Enabled {
		t.Fatalf("nothing may be detached while the directory is unavailable: %v %+v", err, after)
	}
}

// TestAGhostWindowIsClearedOnlyForTheRuleThatLostItsBinding: the cleanup must be
// as narrow as the problem. Dropping every deadline of the device would turn a
// reconciliation into data loss for the rules that are still fine.
func TestAGhostWindowIsClearedOnlyForTheRuleThatLostItsBinding(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a20-two-" + runtime.NewID()[:8])
	kept := e.createNamedRuleIn(serviceTestTenant, "仍然有效的规则", thresholdRule(dev, "30", time.Minute))
	broken := e.createNamedRuleIn(serviceTestTenant, "失去绑定的规则", thresholdRule(dev, "50", time.Minute))
	e.bind(kept, "device", dev, map[string]string{"limit": "30"})
	brokenBinding := e.bind(broken, "device", dev, map[string]string{"limit": "50"})

	// One reading above both thresholds opens both windows.
	e.ingestFor(serviceTestTenant, "ev-a20-two", dev, "temperature", 60.0)
	if e.pendingFor(serviceTestTenant, kept.ID) != 1 || e.pendingFor(serviceTestTenant, broken.ID) != 1 {
		t.Fatal("both rules should be waiting")
	}

	// The second rule loses its binding, which is what a template change looks
	// like from the sweep's point of view.
	if err := e.st.DetachBinding(e.ctx(), serviceTestTenant, brokenBinding.ID); err != nil {
		t.Fatalf("detach: %v", err)
	}

	resp, err := e.svc().ReconcileBindingRefs(e.ctx(), &ReconcileBindingsRequest{Apply: true})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var cleared []string
	for _, g := range resp.Ghosts {
		if g.RuleID == broken.ID {
			if g.Reason != "binding_gone" {
				t.Fatalf("want binding_gone, got %q", g.Reason)
			}
			if !g.PendingCleared {
				t.Fatal("an applied reconcile must clear the window it reported")
			}
			cleared = append(cleared, g.RuleID)
		}
		if g.RuleID == kept.ID {
			t.Fatalf("the rule that still has a binding must not be reported: %+v", g)
		}
	}
	if len(cleared) != 1 {
		t.Fatalf("want exactly the broken rule cleared, got %v", cleared)
	}
	if n := e.pendingFor(serviceTestTenant, kept.ID); n != 1 {
		t.Fatalf("the healthy rule's deadline must survive, %d left", n)
	}
}

// TestARevisionThatBreaksABindingIsBlockedBeforeItIsSaved: editing a rule must
// not silently orphan the values three devices were using. The editor asks
// first, and the answer says which scope is the problem.
func TestARevisionThatBreaksABindingIsBlockedBeforeItIsSaved(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a20-rev-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "要改的规则", thresholdRule(dev, "30", 0))
	e.bind(rule, "device", dev, map[string]string{"limit": "50"})

	// The new body narrows the parameter to at most 10. The stored 50 no longer
	// fits, so publishing would leave this device running on a value the editor
	// cannot even represent.
	narrow := thresholdRule(dev, "5", 0)
	narrow.Params[0].Max = floatPtr(10)

	resp, err := e.svc().CheckRuleRevision(e.ctx(), rule.ID, &RevisionCheckRequest{Definition: narrow})
	if err != nil {
		t.Fatalf("revision check: %v", err)
	}
	if !resp.Blocked || resp.Compatible {
		t.Fatalf("the narrowed body must be reported as blocked: %+v", resp)
	}
	if resp.CheckedBindings != 1 {
		t.Fatalf("want one binding checked, got %d", resp.CheckedBindings)
	}
	found := false
	for _, i := range resp.Issues {
		if i.Code == "binding_incompatible" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want binding_incompatible, got %v", resp.Issues)
	}

	// A widening edit is fine, and must say so instead of warning out of habit.
	wide := thresholdRule(dev, "5", 0)
	ok, err := e.svc().CheckRuleRevision(e.ctx(), rule.ID, &RevisionCheckRequest{Definition: wide})
	if err != nil {
		t.Fatalf("revision check: %v", err)
	}
	if !ok.Compatible || ok.Blocked {
		t.Fatalf("a compatible body must be allowed: %+v", ok)
	}
}

// TestALegacySceneActivationKeepsItsSceneID: the migration used to put the
// referenced scene id in device_ref, where neither the validator nor the
// executor looks. This pins the field down, because a silently empty scene_id
// is exactly the "静默失效链接" A20 exists to prevent.
func TestALegacySceneActivationKeepsItsSceneID(t *testing.T) {
	def := sceneCalling("dev-1", "scene-42")
	if err := domain.Validate(&def); err != nil {
		t.Fatalf("an activate_scene step with a value must validate: %v", err)
	}
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"value":"scene-42"`) {
		t.Fatalf("the scene id must survive serialisation: %s", raw)
	}
}
