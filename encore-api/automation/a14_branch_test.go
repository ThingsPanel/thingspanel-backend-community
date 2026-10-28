package automation

import (
	"strings"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
)

// ---------------------------------------------------------------------------
// A14: finite if/else, and the guard in front of an action.
//
// The four-valued verdict is the whole point. A branch that treats unknown as
// false says "all windows are closed" when in fact every window sensor is
// offline; a guard that treats unknown as true arms the house while nobody can
// see the doors. Both are the same mistake — collapsing "we do not know" into a
// boolean — and both are tested here.
// ---------------------------------------------------------------------------

func (e *testEnv) branchDefinition() domain.Definition {
	def := e.baseDefinition()
	def.Steps = []domain.Step{{
		Kind: domain.StepBranch,
		Condition: &domain.Group{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "50",
		}}},
		Then: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "1"}},
		Else: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "0"}},
	}}
	return def
}

// true goes to then, false goes to else, unknown and error stop the run with a
// readable reason instead of quietly taking the else path.
func TestBranchHasFourDistinctPaths(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.branchDefinition())

	// true: the run's own trigger value is 35, so the branch must see false...
	e.ingest("ev-a14-branch-false", e.dev1, "temperature", 35.0, "")
	runID := e.runs(rule.ID)[0].ID
	if got := e.run(runID).Status; got != runtime.StatusSucceeded {
		t.Fatalf("a decided branch must finish, got %q", got)
	}
	req, _ := e.cmds.last()
	if req.Value != "0" {
		t.Fatalf("false must take the else path, got value %q", req.Value)
	}
}

// An undetermined branch stops the run and says so. It must not be read as
// "the condition was false".
func TestAnUndeterminedBranchDoesNotFallIntoElse(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Steps = []domain.Step{{
		Kind: domain.StepBranch,
		Condition: &domain.Group{Conditions: []domain.Condition{{
			// A device that has never reported: unknown, not false.
			Source: "device", DeviceID: e.dev2, Signal: domain.SignalTelemetry,
			Key: "temperature", Operator: domain.OpGt, Value: "50",
		}}},
		Then: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "1"}},
		Else: []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "fan", Value: "0"}},
	}}
	rule := e.createRule(def)

	e.ingest("ev-a14-unknown-1", e.dev1, "temperature", 35.0, "")
	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	got := e.run(runs[0].ID)
	if got.Status == runtime.StatusSucceeded {
		t.Fatal("an undetermined branch must not be reported as a completed run")
	}
	if got.ReasonCode != "branch_undetermined" {
		t.Fatalf("the reason must say the branch was undetermined, got %q", got.ReasonCode)
	}
	if e.cmds.count() != 0 {
		t.Fatalf("neither then nor else may run, got %d commands", e.cmds.count())
	}
}

// A guard in front of an action: satisfied runs it, unsatisfied stops it, and
// unknown stops it too — an interlock whose input is missing is not open.
func TestGuardBlocksTheActionWhenItsConditionIsNotTrue(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Steps = []domain.Step{{
		Kind: domain.StepAction, Action: "telemetry", Identifier: "arm", Value: "1",
		ContinueOnFailure: true,
		Guard: &domain.Group{Conditions: []domain.Condition{{
			// The window contact must be closed (value 1) before arming.
			Source: "self", Signal: domain.SignalTelemetry, Key: "window_closed",
			Operator: domain.OpEq, Value: "1",
		}}},
	}}
	rule := e.createRule(def)

	// The rule triggers on temperature, but the window was never reported.
	e.ingest("ev-a14-guard-1", e.dev1, "temperature", 35.0, "")

	runs := e.runs(rule.ID)
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	got := e.run(runs[0].ID)
	if got.Status == runtime.StatusSucceeded {
		t.Fatal("a blocked action must not be reported as a successful run")
	}
	if got.ReasonCode != "guard_blocked" {
		t.Fatalf("the reason must say the guard blocked it, got %q", got.ReasonCode)
	}
	if e.cmds.count() != 0 {
		t.Fatalf("a guarded action must not reach the device, got %d commands", e.cmds.count())
	}
	if statuses := e.stepStatuses(runs[0].ID); statuses[0] != "0:action:skipped" {
		t.Fatalf("the step must be recorded as skipped, got %v", statuses)
	}
}

// With the guard satisfied the action runs, and continue_on_failure does not
// turn the guard into a soft check.
func TestGuardLetsTheActionThroughWhenSatisfied(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Steps = []domain.Step{{
		Kind: domain.StepAction, Action: "telemetry", Identifier: "arm", Value: "1",
		Guard: &domain.Group{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "window_closed",
			Operator: domain.OpEq, Value: "1",
		}}},
	}}
	rule := e.createRule(def)

	e.ingest("ev-a14-guard-ok-1", e.dev1, "window_closed", 1.0, "")
	e.ingest("ev-a14-guard-ok-2", e.dev1, "temperature", 35.0, "")

	var ran bool
	for _, r := range e.runs(rule.ID) {
		if r.Status == runtime.StatusSucceeded {
			ran = true
		}
	}
	if !ran {
		t.Fatalf("a satisfied guard must let the run finish, got %d runs", len(e.runs(rule.ID)))
	}
	if e.cmds.count() == 0 {
		t.Fatal("the guarded action must reach the device once the guard is satisfied")
	}
}

// Deep nesting is rejected at publish time, not at run time.
func TestTooDeepNestingIsRejectedAtPublish(t *testing.T) {
	e := newEnv(t)
	leaf := []domain.Step{{Kind: domain.StepAction, Action: "telemetry", Identifier: "x", Value: "1"}}
	branch := func(inner []domain.Step) domain.Step {
		return domain.Step{
			Kind: domain.StepBranch,
			Condition: &domain.Group{Conditions: []domain.Condition{{
				Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
				Operator: domain.OpGt, Value: "1",
			}}},
			Then: inner,
		}
	}
	four := branch([]domain.Step{branch([]domain.Step{branch([]domain.Step{branch(leaf)})})})

	def := e.baseDefinition()
	def.Steps = []domain.Step{four}
	def.Normalize()
	err := domain.Validate(&def)
	if err == nil {
		t.Fatal("four levels of nesting must be rejected")
	}
	if !containsIssue(err, "分支嵌套") {
		t.Fatalf("the rejection must name the nesting limit, got %v", err)
	}
}

// A guard on a step that does not act is meaningless and is rejected.
func TestGuardIsRejectedOnNonActingSteps(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Steps = []domain.Step{{
		Kind: domain.StepWait, Wait: domain.Duration(time.Minute),
		Guard: &domain.Group{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "x",
			Operator: domain.OpEq, Value: "1",
		}}},
	}}
	def.Normalize()
	if err := domain.Validate(&def); err == nil || !containsIssue(err, "前置检查只能挂在") {
		t.Fatalf("a guard on a wait must be rejected, got %v", err)
	}
}

func containsIssue(err error, code string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), code)
}
