package automation

import (
	"encoding/json"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A19: template bindings, inheritance and per-device overrides
// ---------------------------------------------------------------------------
//
// These tests are about one question: when two devices share a template, does
// changing one of them change the other? Everything else here exists to make
// that answer checkable from the outside — from a run record, from a pending
// deadline, from the device page.

// paramRule builds an automation whose threshold is a parameter.
//
// It is deliberately the same shape a customer would get from the template
// library: one condition, one action, one number they may want different per
// device.
func paramRule(e *testEnv, defaultValue string) domain.Definition {
	limit := 100.0
	return domain.Definition{
		Kind:  domain.KindAutomation,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Params: []domain.Param{{
			Key: "limit", Type: domain.ParamNumber, Default: defaultValue,
			Min: floatPtr(0), Max: &limit, Description: "触发阈值", Unit: "℃",
		}},
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "{{limit}}", Unit: "℃",
		}}}}},
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

func floatPtr(f float64) *float64 { return &f }

// bind attaches a rule to a scope with the given values.
func (e *testEnv) bind(rule *store.Rule, scopeType, scopeID string, params map[string]string) *store.Binding {
	e.t.Helper()
	b := &store.Binding{
		ID: runtime.NewID(), TenantID: rule.TenantID, RuleID: rule.ID,
		Revision: rule.Revision, Params: params, Enabled: true,
	}
	switch scopeType {
	case "device":
		b.DeviceID = scopeID
		b.Source = store.BindingSourceDevice
	default:
		b.DeviceConfigID = scopeID
		b.Source = store.BindingSourceTemplate
	}
	b.BindingKey = store.BindingKey(b.TenantID, b.RuleID, b.DeviceID, b.DeviceConfigID)
	if err := e.st.UpsertBinding(e.ctx(), b); err != nil {
		e.t.Fatalf("bind: %v", err)
	}
	e.rt.InvalidateBindings(rule.TenantID, b.DeviceID)
	return b
}

// snapshotOf decodes a run snapshot.
func snapshotOf(t *testing.T, run *store.Run) map[string]any {
	t.Helper()
	var s map[string]any
	if err := json.Unmarshal(run.Snapshot, &s); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	return s
}

func TestATemplateRuleRunsOnEveryDeviceOfTheTemplate(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(paramRule(e, "30"))
	e.bind(rule, "device_config", "cfg-1", map[string]string{"limit": "30"})

	// Both devices belong to cfg-1 and both report above the threshold.
	e.ingest("ev-a19-1", e.dev1, "temperature", 40.0, "")
	e.ingest("ev-a19-2", e.dev2, "temperature", 41.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 2 {
		t.Fatalf("want one run per device, got %d", len(runs))
	}
	devices := map[string]bool{}
	for _, run := range runs {
		devices[run.DeviceID] = true
		// Each run must be its own scope: a template rule that shares one scope
		// across devices would make the second device cancel the first.
		if run.ScopeKey != runtime.ScopeKey(&rule.Definition, rule.ID, run.DeviceID) {
			t.Fatalf("device %s shares a scope with another device: %s", run.DeviceID, run.ScopeKey)
		}
		s := snapshotOf(t, run)
		if s["binding_source"] != store.BindingSourceTemplate {
			t.Fatalf("run does not record that it came from a template binding: %v", s["binding_source"])
		}
		params, _ := s["binding_params"].(map[string]any)
		if params["limit"] != "30" {
			t.Fatalf("run did not freeze the parameter values: %v", s["binding_params"])
		}
	}
	if !devices[e.dev1] || !devices[e.dev2] {
		t.Fatalf("the template rule did not run on both devices: %v", devices)
	}
}

func TestOneDevicesOverrideDoesNotChangeTheOther(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(paramRule(e, "30"))
	e.bind(rule, "device_config", "cfg-1", map[string]string{"limit": "30"})
	// The second device is noisier and gets its own threshold.
	e.bind(rule, "device", e.dev2, map[string]string{"limit": "60"})

	// 40 is above dev1's 30 and below dev2's 60.
	e.ingest("ev-a19-3", e.dev1, "temperature", 40.0, "")
	e.ingest("ev-a19-4", e.dev2, "temperature", 40.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want exactly one run (dev1 only), got %d", len(runs))
	}
	if runs[0].DeviceID != e.dev1 {
		t.Fatalf("the wrong device triggered: %s", runs[0].DeviceID)
	}

	// The override is not a one-way door: crossing 60 does trigger.
	e.ingest("ev-a19-5", e.dev2, "temperature", 65.0, "")
	if n := len(e.runs(rule.ID)); n != 2 {
		t.Fatalf("the overridden device never triggered at its own threshold, runs=%d", n)
	}
}

func TestTheDevicePageShowsWhereEachValueCameFrom(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(paramRule(e, "30"))
	e.bind(rule, "device_config", "cfg-1", map[string]string{"limit": "30"})
	e.bind(rule, "device", e.dev2, map[string]string{"limit": "60"})

	bound, err := e.st.BindingsForDevice(e.ctx(), e.tenant, e.dev2, "cfg-1")
	if err != nil {
		t.Fatalf("bindings: %v", err)
	}
	if len(bound) != 2 {
		t.Fatalf("want the template row and the device row, got %d", len(bound))
	}
	// Template first, device second: the order *is* the merge order.
	if bound[0].DeviceID != "" || bound[1].DeviceID != e.dev2 {
		t.Fatalf("bindings are not ordered template-then-device: %+v", bound)
	}

	effective, err := e.st.EffectiveBindings(e.ctx(), e.tenant, e.dev2, "cfg-1")
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if len(effective) != 1 {
		t.Fatalf("want one rule after merging, got %d", len(effective))
	}
	if effective[0].Source != store.BindingSourceDevice {
		t.Fatalf("want the device row to win, got %q", effective[0].Source)
	}
	if effective[0].Params["limit"] != "60" {
		t.Fatalf("want the device value to win, got %v", effective[0].Params)
	}
}

func TestADetachedBindingStopsFutureTriggersButKeepsItsRecord(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(paramRule(e, "30"))
	b := e.bind(rule, "device_config", "cfg-1", map[string]string{"limit": "30"})

	e.ingest("ev-a19-6", e.dev1, "temperature", 40.0, "")
	before := len(e.runs(rule.ID))

	if err := e.st.DetachBinding(e.ctx(), e.tenant, b.ID); err != nil {
		t.Fatalf("detach: %v", err)
	}
	e.rt.InvalidateBindings(e.tenant, "")

	// The condition still matches (self/temperature), so the rule is still a
	// candidate. Without a binding it falls back to the default value, which is
	// the honest behaviour: detaching an override returns the device to the
	// shared rule, it does not delete the rule.
	e.ingest("ev-a19-7", e.dev1, "temperature", 40.0, "")
	after := len(e.runs(rule.ID))
	if after != before+1 {
		t.Fatalf("detaching the template binding must not delete the rule itself (runs %d -> %d)", before, after)
	}

	rows, err := e.st.ListBindingsForRule(e.ctx(), e.tenant, rule.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Enabled || rows[0].DetachedAt == nil {
		t.Fatalf("a detach must keep the row with a date, got %+v", rows)
	}
}

func TestBindingTheSameScopeTwiceDoesNotCreateASecondRule(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(paramRule(e, "30"))
	e.bind(rule, "device_config", "cfg-1", map[string]string{"limit": "30"})
	e.bind(rule, "device_config", "cfg-1", map[string]string{"limit": "35"})

	rows, err := e.st.ListBindingsForRule(e.ctx(), e.tenant, rule.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("binding the same scope twice produced %d rows", len(rows))
	}
	if rows[0].Params["limit"] != "35" {
		t.Fatalf("the second attach did not update the values: %v", rows[0].Params)
	}

	// And only one run per sample, not two.
	e.ingest("ev-a19-8", e.dev1, "temperature", 40.0, "")
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("a re-attached binding fired %d times", n)
	}
}

func TestABadParameterValueIsRefusedBeforeItIsStored(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(paramRule(e, "30"))

	if _, err := rule.Definition.Values(map[string]string{"limit": "很热"}); err == nil {
		t.Fatalf("a non-numeric threshold was accepted")
	}
	if _, err := rule.Definition.Values(map[string]string{"limit": "300"}); err == nil {
		t.Fatalf("a threshold above the declared maximum was accepted")
	}
	if _, err := rule.Definition.Values(map[string]string{"limt": "30"}); err == nil {
		t.Fatalf("a misspelled parameter name was accepted instead of being reported")
	}
}

func TestAPendingWindowKeepsTheValuesItOpenedWith(t *testing.T) {
	e := newEnv(t)
	def := paramRule(e, "30")
	def.Trigger.For = domain.Duration(5 * time.Minute)
	rule := e.createRule(def)
	e.bind(rule, "device_config", "cfg-1", map[string]string{"limit": "30"})

	// 40 opens the window under the 30 threshold.
	e.ingest("ev-a19-9", e.dev1, "temperature", 40.0, "")
	if len(e.runs(rule.ID)) != 0 {
		t.Fatalf("the sustained rule fired before its duration elapsed")
	}

	// Halfway through, somebody raises the threshold to 100. The condition is
	// no longer true at 40 — but the window was opened at 30, and moving the
	// finish line under a waiting customer is exactly what must not happen.
	e.bind(rule, "device_config", "cfg-1", map[string]string{"limit": "100"})

	e.advance(5*time.Minute + time.Second)
	e.tick()

	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("the window did not fire with the values it opened with (runs=%d)", len(runs))
	}
	params, _ := snapshotOf(t, runs[0])["binding_params"].(map[string]any)
	if params["limit"] != "30" {
		t.Fatalf("the run was decided with the edited value, not the frozen one: %v", params)
	}
}

// ---------------------------------------------------------------------------
// Service level: the same facts, through the endpoints the pages call
// ---------------------------------------------------------------------------

func (e *testEnv) createRuleIn(tenant string, def domain.Definition) *store.Rule {
	e.t.Helper()
	def.Normalize()
	if err := domain.Validate(&def); err != nil {
		e.t.Fatalf("test rule is invalid: %v", err)
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: tenant, Name: "模板规则", Kind: def.Kind,
		Enabled: true, Definition: def, DefinitionHash: domain.DefinitionHash(def),
	}
	if err := e.st.CreateRule(e.ctx(), rule); err != nil {
		e.t.Fatalf("create rule: %v", err)
	}
	stored, err := e.st.GetRule(e.ctx(), tenant, rule.ID)
	if err != nil {
		e.t.Fatalf("reload rule: %v", err)
	}
	return stored
}

func TestTheDevicePageListsTemplateDeviceAndConditionRules(t *testing.T) {
	e := newEnv(t)
	rule := e.createRuleIn(serviceTestTenant, paramRule(e, "30"))
	// A plain rule that matches by condition, not by binding — the legacy shape.
	plain := paramRule(e, "30")
	plain.Params = nil
	plain.Trigger.Groups[0].Conditions[0].Value = "30"
	conditionRule := e.createRuleIn(serviceTestTenant, plain)

	if _, err := e.svc().AttachBinding(e.ctx(), rule.ID, &AttachBindingRequest{
		ScopeType: "device_config", ScopeID: "cfg-1",
		Params: map[string]string{"limit": "30"},
	}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := e.svc().AttachBinding(e.ctx(), rule.ID, &AttachBindingRequest{
		ScopeType: "device", ScopeID: e.dev2, Params: map[string]string{"limit": "60"},
	}); err != nil {
		t.Fatalf("attach device: %v", err)
	}
	// The service endpoints run under the fixed test tenant, and the fake
	// directory only answers for the tenant that asked. Re-registering the
	// device without a tenant makes it visible to that call, the same way the
	// A18 tests do it.
	e.dir.devices[e.dev2] = ports.Device{ID: e.dev2, Name: "设备2", DeviceConfigID: "cfg-1"}

	got, err := e.svc().GetDeviceAutomation(e.ctx(), e.dev2)
	if err != nil {
		t.Fatalf("device automation: %v", err)
	}
	if got.DeviceConfigID != "cfg-1" {
		t.Fatalf("want the device's template to be resolved, got %q", got.DeviceConfigID)
	}
	var found, overridden, byCondition bool
	for _, r := range got.Rules {
		switch r.RuleID {
		case rule.ID:
			found = true
			if r.Source != store.BindingSourceDevice {
				t.Fatalf("want the device row to be reported as the source, got %q", r.Source)
			}
			if r.Effective["limit"] != "60" {
				t.Fatalf("want the effective value 60, got %v", r.Effective)
			}
			overridden = len(r.Overridden) == 1 && r.Overridden[0] == "limit"
		case conditionRule.ID:
			byCondition = r.Source == "condition"
		}
	}
	if !found {
		t.Fatalf("the bound rule is missing from the device page")
	}
	if !overridden {
		t.Fatalf("the device page did not mark the overridden parameter")
	}
	if !byCondition {
		t.Fatalf("a rule that applies by condition was hidden from the device page")
	}
}

func TestTheTemplatePageCountsAttachedAndDetached(t *testing.T) {
	e := newEnv(t)
	rule := e.createRuleIn(serviceTestTenant, paramRule(e, "30"))
	other := e.createRuleIn(serviceTestTenant, paramRule(e, "40"))

	// A template id of this test's own: bindings under the shared service tenant
	// are visible to every other test, and a count is exactly the assertion that
	// cannot tolerate that.
	cfg := "cfg-" + runtime.NewID()[:8]
	for _, id := range []string{e.dev1 + "-tpl", e.dev2 + "-tpl"} {
		e.dir.devices[id] = ports.Device{ID: id, Name: "模板设备", DeviceConfigID: cfg}
	}

	a, err := e.svc().AttachBinding(e.ctx(), rule.ID, &AttachBindingRequest{
		ScopeType: "device_config", ScopeID: cfg,
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := e.svc().AttachBinding(e.ctx(), other.ID, &AttachBindingRequest{
		ScopeType: "device_config", ScopeID: cfg,
	}); err != nil {
		t.Fatalf("attach other: %v", err)
	}
	if _, err := e.svc().DetachBinding(e.ctx(), a.ID); err != nil {
		t.Fatalf("detach: %v", err)
	}

	got, err := e.svc().GetTemplateBindings(e.ctx(), cfg)
	if err != nil {
		t.Fatalf("template bindings: %v", err)
	}
	if got.Attached != 1 || got.Detached != 1 {
		t.Fatalf("want 1 attached and 1 detached, got %d/%d", got.Attached, got.Detached)
	}
	if got.DeviceCount != 2 {
		t.Fatalf("want the two devices of cfg-1 to be counted, got %d", got.DeviceCount)
	}

	// Detaching twice is the same outcome, not an error: a retried request must
	// not look like a failure.
	if _, err := e.svc().DetachBinding(e.ctx(), a.ID); err != nil {
		t.Fatalf("detaching an already detached binding failed: %v", err)
	}
}

func TestAnInvalidValueIsRefusedAtTheBindingEndpoint(t *testing.T) {
	e := newEnv(t)
	rule := e.createRuleIn(serviceTestTenant, paramRule(e, "30"))

	if _, err := e.svc().AttachBinding(e.ctx(), rule.ID, &AttachBindingRequest{
		ScopeType: "device_config", ScopeID: "cfg-1",
		Params: map[string]string{"limit": "hot"},
	}); err == nil {
		t.Fatalf("the endpoint stored a non-numeric threshold")
	}
	if _, err := e.svc().AttachBinding(e.ctx(), rule.ID, &AttachBindingRequest{
		ScopeType: "nonsense", ScopeID: "cfg-1",
	}); err == nil {
		t.Fatalf("the endpoint accepted an unknown scope type")
	}

	rows, err := e.st.ListBindingsForRule(e.ctx(), serviceTestTenant, rule.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("a refused attach left %d binding rows behind", len(rows))
	}
}
