package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validDefinition() Definition {
	d := Definition{
		Kind:  KindAutomation,
		Mode:  ModeSingle,
		Scope: ScopeDevice,
		Trigger: Trigger{Groups: []Group{{Conditions: []Condition{{
			Source: "self", Signal: SignalTelemetry, Key: "temperature", Operator: OpGt, Value: "30",
		}}}}},
		Targets: TargetSpec{DeviceIDs: []string{"dev-1"}},
		Steps: []Step{{
			Kind: StepAction, Action: "telemetry", Identifier: "switch", Value: "1",
		}},
	}
	d.Normalize()
	return d
}

func issuesOf(t *testing.T, d Definition) []Issue {
	t.Helper()
	err := Validate(&d)
	if err == nil {
		return nil
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("unexpected error type: %v", err)
	}
	return ve.Issues
}

func hasCode(issues []Issue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// TestValidDefinitionPasses is the baseline: a normal rule is accepted and
// normalization fills the budget.
func TestValidDefinitionPasses(t *testing.T) {
	d := validDefinition()
	if issues := issuesOf(t, d); len(issues) != 0 {
		t.Fatalf("unexpected issues: %+v", issues)
	}
	if d.Budget.MaxSteps == 0 || d.Budget.MaxRunDuration == 0 {
		t.Fatal("normalization must fill the budget")
	}
	if d.Freshness.MaxAge == 0 || d.Freshness.MaxGap == 0 {
		t.Fatal("normalization must fill freshness")
	}
}

func TestRejectsEmptyTrigger(t *testing.T) {
	d := validDefinition()
	d.Trigger.Groups = nil
	if issues := issuesOf(t, d); !hasCode(issues, "empty_trigger") {
		t.Fatalf("want empty_trigger, got %+v", issues)
	}
}

func TestRejectsUnknownOperator(t *testing.T) {
	d := validDefinition()
	d.Trigger.Groups[0].Conditions[0].Operator = "~="
	if issues := issuesOf(t, d); !hasCode(issues, "unsupported_operator") {
		t.Fatalf("want unsupported_operator, got %+v", issues)
	}
}

func TestRejectsEmptyTargets(t *testing.T) {
	d := validDefinition()
	d.Targets = TargetSpec{}
	d.Normalize()
	if issues := issuesOf(t, d); !hasCode(issues, "empty_targets") {
		t.Fatalf("want empty_targets, got %+v", issues)
	}
}

// TestRejectsStepOverBudget proves the finite program is bounded before it can
// be published.
func TestRejectsStepOverBudget(t *testing.T) {
	d := validDefinition()
	d.Budget.MaxSteps = 2
	d.Steps = []Step{
		{Kind: StepAction, Action: "telemetry", Identifier: "a", Value: "1"},
		{Kind: StepAction, Action: "telemetry", Identifier: "b", Value: "1"},
		{Kind: StepAction, Action: "telemetry", Identifier: "c", Value: "1"},
	}
	if issues := issuesOf(t, d); !hasCode(issues, "too_many_steps") {
		t.Fatalf("want too_many_steps, got %+v", issues)
	}
}

// TestRejectsBranchOverDepth bounds nesting so a rule can never become an
// unbounded loop.
func TestRejectsBranchOverDepth(t *testing.T) {
	leaf := []Step{{Kind: StepAction, Action: "telemetry", Identifier: "a", Value: "1"}}
	cond := &Group{Conditions: []Condition{{
		Source: "self", Signal: SignalTelemetry, Key: "t", Operator: OpGt, Value: "1",
	}}}
	d := validDefinition()
	d.Budget.MaxDepth = 1
	d.Steps = []Step{{
		Kind: StepBranch, Condition: cond,
		Then: []Step{{Kind: StepBranch, Condition: cond, Then: leaf, Else: leaf}},
		Else: leaf,
	}}
	if issues := issuesOf(t, d); !hasCode(issues, "too_deep") {
		t.Fatalf("want too_deep, got %+v", issues)
	}
}

// TestAlarmPolicyRejectsDeviceActions is the ownership boundary: the alarm owner
// produces lifecycle transitions only, never device commands.
func TestAlarmPolicyRejectsDeviceActions(t *testing.T) {
	d := validDefinition()
	d.Kind = KindAlarm
	if issues := issuesOf(t, d); !hasCode(issues, "alarm_side_effect") {
		t.Fatalf("want alarm_side_effect, got %+v", issues)
	}
	d.Steps = []Step{{Kind: StepNotify, NotificationPolicyID: "np-1", Value: "告警通知"}}
	if issues := issuesOf(t, d); len(issues) != 0 {
		t.Fatalf("notification-only alarm policy must pass, got %+v", issues)
	}
}

func TestRejectsFreshnessGapOverAge(t *testing.T) {
	d := validDefinition()
	d.Freshness.MaxAge = Duration(30 * time.Second)
	d.Freshness.MaxGap = Duration(60 * time.Second)
	if issues := issuesOf(t, d); !hasCode(issues, "gap_exceeds_age") {
		t.Fatalf("want gap_exceeds_age, got %+v", issues)
	}
}

func TestRejectsNegativeFor(t *testing.T) {
	d := validDefinition()
	d.Trigger.For = Duration(-time.Minute)
	if issues := issuesOf(t, d); !hasCode(issues, "negative_duration") {
		t.Fatalf("want negative_duration, got %+v", issues)
	}
}

// TestRejectsQueuedWithoutBounds: normalization fills a default, but an explicit
// broken queue declaration must still be rejected.
func TestRejectsQueuedWithoutBounds(t *testing.T) {
	d := validDefinition()
	d.Mode = ModeQueued
	d.Queue = &QueueRule{} // explicit but unbounded
	if issues := issuesOf(t, d); !hasCode(issues, "invalid_queue") {
		t.Fatalf("want invalid_queue, got %+v", issues)
	}

	// Omitting it entirely is legal: normalization supplies the documented
	// default rather than leaving the mode unbounded.
	d2 := validDefinition()
	d2.Mode = ModeQueued
	if err := Validate(&d2); err != nil {
		t.Fatalf("default queue must be valid, got %v", err)
	}
	if d2.Queue == nil || d2.Queue.MaxDepth == 0 || d2.Queue.TTL == 0 {
		t.Fatal("normalization must fill the queue default")
	}
}

// TestIssueMessagesAreReadable: every issue must be human readable because it is
// shown in the editor and in CLI output.
func TestIssueMessagesAreReadable(t *testing.T) {
	d := validDefinition()
	d.Trigger.Groups = nil
	err := Validate(&d)
	if err == nil || !strings.Contains(err.Error(), "触发条件") {
		t.Fatalf("want readable message, got %v", err)
	}
}

func TestCountSteps(t *testing.T) {
	leaf := []Step{{Kind: StepAction, Action: "telemetry", Identifier: "a", Value: "1"}}
	cond := &Group{Conditions: []Condition{{
		Source: "self", Signal: SignalTelemetry, Key: "t", Operator: OpGt, Value: "1",
	}}}
	steps := []Step{{Kind: StepBranch, Condition: cond, Then: leaf, Else: leaf}}
	if n := CountSteps(steps); n != 3 {
		t.Fatalf("want 3 got %d", n)
	}
}

// TestDefinitionHashIsStable: the hash is the identity a preview and an external
// deployment compare, so it must not depend on map iteration order.
func TestDefinitionHashIsStable(t *testing.T) {
	d1 := validDefinition()
	d2 := validDefinition()
	if DefinitionHash(d1) != DefinitionHash(d2) {
		t.Fatal("hash must be stable for identical definitions")
	}
	d2.Steps[0].Value = "0"
	if DefinitionHash(d1) == DefinitionHash(d2) {
		t.Fatal("hash must change when the body changes")
	}
}
