package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// A parameter is a hole in a rule body. These tests pin the two things that go
// wrong with holes: a value that escapes into the JSON structure, and a hole
// nobody declared.

func paramDefinition() Definition {
	limit := 100.0
	return Definition{
		Kind:  KindAutomation,
		Mode:  ModeSingle,
		Scope: ScopeDevice,
		Params: []Param{{
			Key: "limit", Type: ParamNumber, Default: "30", Min: float64Ptr(0), Max: &limit,
		}},
		Trigger: Trigger{Groups: []Group{{Conditions: []Condition{{
			Source: "self", Signal: SignalTelemetry, Key: "temperature",
			Operator: OpGt, Value: "{{limit}}", Unit: "℃",
		}}}}},
		Freshness: Freshness{
			MaxAge: Duration(10 * time.Minute),
			MaxGap: Duration(10 * time.Minute),
		},
		Targets: TargetSpec{DeviceIDs: []string{"d1"}},
		Steps: []Step{{
			Kind: StepAction, Action: "telemetry", Identifier: "switch", Value: "1",
		}},
	}
}

func float64Ptr(f float64) *float64 { return &f }

// hasIssueCode checks the machine readable code, not the Chinese text: the text
// is for people, the code is what a client switches on.
func hasIssueCode(err error, code string) bool {
	var ve *ValidationError
	if !errors.As(err, &ve) {
		return false
	}
	for _, i := range ve.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

func TestAParameterFillsTheBodyWithoutChangingItsShape(t *testing.T) {
	def := paramDefinition()
	if err := Validate(&def); err != nil {
		t.Fatalf("the template is not publishable: %v", err)
	}
	got, err := Materialize(def, map[string]string{"limit": "45"})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if v := got.Trigger.Groups[0].Conditions[0].Value; v != "45" {
		t.Fatalf("want the threshold to be 45, got %q", v)
	}
	// The declared slots stay on the body: the stored rule is still a template,
	// only the materialized copy carries the number.
	if len(got.Params) != 1 {
		t.Fatalf("materializing dropped the parameter declaration")
	}
}

func TestAParameterValueCannotEscapeItsString(t *testing.T) {
	def := paramDefinition()
	// A string parameter is the interesting case: a quote in the value must be
	// escaped, not close the JSON string and inject a field.
	def.Params = []Param{{Key: "label", Type: ParamString, Default: "ok"}}
	def.Trigger.Groups[0].Conditions[0].Value = "{{label}}"
	def.Steps[0].Value = "{{label}}"

	got, err := Materialize(def, map[string]string{"label": `a"b\c`})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if got.Steps[0].Value != `a"b\c` {
		t.Fatalf("the value did not round-trip: %q", got.Steps[0].Value)
	}
}

func TestAParameterCanDriveADuration(t *testing.T) {
	def := paramDefinition()
	def.Params = []Param{{Key: "hold", Type: ParamDuration, Default: "5m"}}
	def.Trigger.For = Duration(0)
	// The duration field is a string in JSON, so the same substitution works.
	raw, err := Substitute([]byte(`{"for":"{{hold}}"}`), map[string]string{"hold": "90s"})
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	if string(raw) != `{"for":"90s"}` {
		t.Fatalf("duration was not substituted: %s", raw)
	}
	var parsed struct {
		For Duration `json:"for"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if parsed.For.Std() != 90*time.Second {
		t.Fatalf("want 90s, got %s", parsed.For.Std())
	}
}

func TestAnUndeclaredPlaceholderIsRefusedAtPublishTime(t *testing.T) {
	def := paramDefinition()
	def.Trigger.Groups[0].Conditions[0].Value = "{{threshold}}"
	err := Validate(&def)
	if err == nil {
		t.Fatalf("a body referencing an undeclared parameter was accepted")
	}
	if !hasIssueCode(err, "undeclared_param") {
		t.Fatalf("the wrong issue was reported: %v", err)
	}
}

func TestADeclaredButUnusedParameterIsRefused(t *testing.T) {
	def := paramDefinition()
	def.Params = append(def.Params, Param{Key: "spare", Type: ParamNumber, Default: "1"})
	err := Validate(&def)
	if err == nil {
		t.Fatalf("a form field that changes nothing was accepted")
	}
	if !hasIssueCode(err, "unused_param") {
		t.Fatalf("the wrong issue was reported: %v", err)
	}
}

func TestABindingCannotSetAParameterTheRuleDoesNotHave(t *testing.T) {
	def := paramDefinition()
	if _, err := def.Values(map[string]string{"limt": "30"}); err == nil {
		t.Fatalf("a misspelled parameter was accepted")
	}
}

func TestAParameterValueOutsideItsDeclaredRangeIsRefused(t *testing.T) {
	def := paramDefinition()
	if _, err := def.Values(map[string]string{"limit": "-1"}); err == nil {
		t.Fatalf("a negative threshold was accepted")
	}
	if _, err := def.Values(map[string]string{"limit": "101"}); err == nil {
		t.Fatalf("a threshold above the maximum was accepted")
	}
}

func TestAParameterPlaceholderWithoutAnEndIsRefused(t *testing.T) {
	if _, err := Substitute([]byte(`{"v":"{{limit}"}`), map[string]string{"limit": "1"}); err == nil {
		t.Fatalf("an unterminated placeholder was accepted")
	}
}

func TestAValueMissingFromTheBindingIsReported(t *testing.T) {
	if _, err := Substitute([]byte(`{"v":"{{limit}}"}`), map[string]string{}); err == nil {
		t.Fatalf("a placeholder with no value was accepted")
	}
}
