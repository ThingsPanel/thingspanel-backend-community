package automation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"encore.dev/beta/errs"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
)

// ---------------------------------------------------------------------------
// A26: the code has to leave the process
// ---------------------------------------------------------------------------
//
// Encore keeps `Meta` inside the application and returns `Details` to callers.
// The first version of every machine-readable code in this package lived in
// Meta, so the web page and the CLI received the sentence and never the code —
// a caller could read "预览已过期" and still have no way to branch on it. These
// tests exist because the Go-level assertions on Meta could not see the
// difference: they pass whether or not the caller ever gets the value.

// wireDetail returns the error as a client would receive it, decoded.
func wireDetail(t *testing.T, err error) map[string]any {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("not an errs.Error: %v", err)
	}
	raw, merr := json.Marshal(e)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	var wire struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if uerr := json.Unmarshal(raw, &wire); uerr != nil {
		t.Fatalf("unmarshal: %v (%s)", uerr, raw)
	}
	if wire.Details == nil {
		t.Fatalf("details is null — the code never reaches the caller: %s", raw)
	}
	return wire.Details
}

// TestARejectedDefinitionCarriesItsFieldOutOfProcess: the web form marks the
// input, the CLI sets exit code 4, and neither parses Chinese to do it.
func TestARejectedDefinitionCarriesItsFieldOutOfProcess(t *testing.T) {
	e := newEnv(t)
	def := domain.Definition{Kind: domain.KindAutomation, Mode: domain.ModeSingle, Scope: domain.ScopeDevice}

	_, err := e.svc().CreateRule(e.ctx(), &CreateRuleRequest{Name: "空的", Definition: def})
	if err == nil {
		t.Fatal("an empty definition must be refused")
	}
	if code := errs.Code(err); code != errs.InvalidArgument {
		t.Fatalf("want invalid_argument, got %v", code)
	}
	d := wireDetail(t, err)
	if d["code"] != "invalid_definition" {
		t.Fatalf("want invalid_definition, got %v", d)
	}
	fields, ok := d["fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("field-level issues must survive serialisation: %v", d)
	}
	first, _ := fields[0].(map[string]any)
	if first["field"] == "" {
		t.Fatalf("a field error without a field name is not machine-readable: %v", first)
	}
}

// TestAStalePreviewIsIdentifiableByCode: a caller that cannot tell "stale" from
// "expired" cannot decide whether to re-preview or to give up.
func TestAStalePreviewIsIdentifiableByCode(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a26-err-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "会被改的规则", thresholdRule(dev, "30", 0))

	preview, err := e.svc().PreviewRuleChange(e.ctx(), rule.ID, &ChangePreviewRequest{
		Definition: thresholdRule(dev, "40", 0),
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	// Somebody saves first, so the token describes a world that no longer exists.
	if _, err := e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Name: "别人改过", Definition: thresholdRule(dev, "35", 0), Version: rule.Version,
	}); err != nil {
		t.Fatalf("intervening save: %v", err)
	}
	_, err = e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Definition: thresholdRule(dev, "40", 0), Version: rule.Version + 1,
		PreviewToken: preview.Token,
	})
	if err == nil {
		t.Fatal("a stale preview must be refused")
	}
	if d := wireDetail(t, err); d["code"] != "preview_stale" {
		t.Fatalf("want preview_stale on the wire, got %v", d)
	}
}

// TestARefusedDeleteNamesTheReferencersOutOfProcess: "谁在引用" is the whole
// answer; a message that only says "有引用" leaves the user hunting.
func TestARefusedDeleteNamesTheReferencersOutOfProcess(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a26-ref-" + runtime.NewID()[:8])
	target := e.createNamedRuleIn(serviceTestTenant, "被引用的规则", thresholdRule(dev, "30", 0))
	caller := e.createNamedRuleIn(serviceTestTenant, "调用者", sceneCalling(dev, target.ID))

	_, err := e.svc().DeleteRule(e.ctx(), target.ID, &DeleteRuleRequest{})
	if err == nil {
		t.Fatal("a referenced rule must not be deleted without force")
	}
	d := wireDetail(t, err)
	if d["code"] != "referenced_by_rules" {
		t.Fatalf("want referenced_by_rules, got %v", d)
	}
	ids, ok := d["referencing_rule_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != caller.ID {
		t.Fatalf("the referencer must be named: %v", d["referencing_rule_ids"])
	}
}

// TestABrokenBindingEditSaysWhichField: the editor needs to point at the
// binding, not print a paragraph.
func TestABrokenBindingEditSaysWhichField(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a26-bind-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "有覆盖值的规则", thresholdRule(dev, "30", 0))
	e.bind(rule, "device", dev, map[string]string{"limit": "55"})

	narrow := thresholdRule(dev, "5", 0)
	narrow.Params[0].Max = floatPtr(10)

	_, err := e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{
		Definition: narrow, Version: rule.Version,
	})
	if err == nil {
		t.Fatal("the save must be refused")
	}
	d := wireDetail(t, err)
	if d["code"] != "binding_incompatible" {
		t.Fatalf("want binding_incompatible, got %v", d)
	}
	fields, _ := d["fields"].([]any)
	if len(fields) == 0 {
		t.Fatalf("the field list is what makes this actionable: %v", d)
	}
	if msg, _ := fields[0].(map[string]any)["message"].(string); !strings.Contains(msg, "limit") {
		t.Fatalf("the message should name the parameter: %v", fields[0])
	}
}

// TestTheSimulatedActionsReachTheCaller: the runtime knew exactly what it would
// do, and the response type was dropping that half. A test that calls the
// runtime directly cannot see it — which is why this one goes through the
// service method the HTTP endpoint calls.
func TestTheSimulatedActionsReachTheCaller(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a26-sim-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "会动手的规则", thresholdRule(dev, "30", 0))

	res, err := e.svc().SimulateRule(e.ctx(), rule.ID, &SimulateRequest{
		Samples: []SimSampleInput{{
			At: "0s", DeviceID: dev, Signal: "telemetry", Key: "temperature", Value: "40",
		}},
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if res.SideEffects != 0 {
		t.Fatalf("a simulation must not have side effects, got %d", res.SideEffects)
	}
	if res.WouldRun != 1 {
		t.Fatalf("the sample is above the threshold, want 1 run, got %d", res.WouldRun)
	}
	if len(res.Actions) == 0 {
		t.Fatal("the caller must be told what would happen, not just that it would")
	}
	if res.Actions[0].Outcome != "would_execute" || res.Actions[0].Detail == "" {
		t.Fatalf("each action needs an outcome and a readable detail: %+v", res.Actions[0])
	}
	if res.EndsAt == "" {
		t.Fatal("the virtual clock position should be reported")
	}
}
