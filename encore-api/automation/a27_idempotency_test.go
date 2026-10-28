package automation

import (
	"testing"

	"encore.app/automation/runtime"
)

// ---------------------------------------------------------------------------
// A27: an inbound key, so a retry is not a second action
// ---------------------------------------------------------------------------
//
// Every device command and notification has carried a stable key into
// auto_outbox since A10. Nothing on the way in did. So a client that timed out
// on POST /v1/rules/:id/run and retried got a second run — and a run is already
// a command. The three endpoints below are the ones where that matters: they
// create a run, a rule, or a revision.

func (e *testEnv) runCountFor(ruleID string) int {
	e.t.Helper()
	var n int
	if err := e.st.DB.QueryRowContext(e.ctx(),
		`SELECT count(*) FROM auto_runs WHERE rule_id=$1`, ruleID).Scan(&n); err != nil {
		e.t.Fatalf("count runs: %v", err)
	}
	return n
}

func (e *testEnv) outboxCountFor(runID string) int {
	e.t.Helper()
	var n int
	if err := e.st.DB.QueryRowContext(e.ctx(),
		`SELECT count(*) FROM auto_outbox WHERE run_id=$1`, runID).Scan(&n); err != nil {
		e.t.Fatalf("count outbox: %v", err)
	}
	return n
}

// TestARetriedRunIsTheSameRun: the assistant that timed out and tried again
// must not turn the door a second time.
func TestARetriedRunIsTheSameRun(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a27-run-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "门口灯", thresholdRule(dev, "30", 0))

	first, err := e.svc().RunRule(e.ctx(), rule.ID, &RunRuleRequest{DeviceID: dev, RequestID: "run-k1"})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := e.svc().RunRule(e.ctx(), rule.ID, &RunRuleRequest{DeviceID: dev, RequestID: "run-k1"})
	if err != nil {
		t.Fatalf("retry must succeed, not conflict: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("the retry produced a second run: %s vs %s", first.ID, second.ID)
	}
	if n := e.runCountFor(rule.ID); n != 1 {
		t.Fatalf("want 1 run on record, got %d", n)
	}
	if n := e.outboxCountFor(first.ID); n != 1 {
		t.Fatalf("want 1 outbound intent, got %d — the door opened twice", n)
	}
}

// TestADifferentKeyIsADifferentRun: dedupe must not swallow a legitimate second
// press of the button.
func TestADifferentKeyIsADifferentRun(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a27-two-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "门口灯", thresholdRule(dev, "30", 0))

	first, err := e.svc().RunRule(e.ctx(), rule.ID, &RunRuleRequest{DeviceID: dev, RequestID: "run-a"})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := e.svc().RunRule(e.ctx(), rule.ID, &RunRuleRequest{DeviceID: dev, RequestID: "run-b"})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("two different keys collapsed into one run %s", first.ID)
	}
	if n := e.runCountFor(rule.ID); n != 2 {
		t.Fatalf("want 2 runs, got %d", n)
	}
}

// TestARunWithoutAKeyStillRuns: the web page's "run now" button sends no key
// and must keep working exactly as before.
func TestARunWithoutAKeyStillRuns(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a27-nokey-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "门口灯", thresholdRule(dev, "30", 0))

	first, err := e.svc().RunRule(e.ctx(), rule.ID, &RunRuleRequest{DeviceID: dev})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	second, err := e.svc().RunRule(e.ctx(), rule.ID, &RunRuleRequest{DeviceID: dev})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("without a key every press is its own run")
	}
}

// TestARetriedCreateMakesOneRule: a client that never saw the response must not
// come back with two rules that look identical in the list.
func TestARetriedCreateMakesOneRule(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a27-create-" + runtime.NewID()[:8])
	before := e.ruleCount()

	req := &CreateRuleRequest{
		Name: "重试出来的规则", Definition: thresholdRule(dev, "30", 0), RequestID: "create-k1",
	}
	first, err := e.svc().CreateRule(e.ctx(), req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := e.svc().CreateRule(e.ctx(), req)
	if err != nil {
		t.Fatalf("retry must succeed: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("the retry made a second rule: %s vs %s", first.ID, second.ID)
	}
	if n := e.ruleCount(); n != before+1 {
		t.Fatalf("want exactly one new rule, got %d new", n-before)
	}
}

// TestARetriedSaveDoesNotLookLikeSomebodyElseEditedIt: without the key the
// retry fails the version check, and the user is told a colleague changed the
// rule when nobody did.
func TestARetriedSaveDoesNotLookLikeSomebodyElseEditedIt(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a27-save-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "会被改的规则", thresholdRule(dev, "30", 0))

	req := &UpdateRuleRequest{
		Name: "改过的规则", Definition: thresholdRule(dev, "40", 0),
		Version: rule.Version, RequestID: "save-k1",
	}
	first, err := e.svc().UpdateRule(e.ctx(), rule.ID, req)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if first.Version != rule.Version+1 {
		t.Fatalf("want version %d, got %d", rule.Version+1, first.Version)
	}
	second, err := e.svc().UpdateRule(e.ctx(), rule.ID, req)
	if err != nil {
		t.Fatalf("the retry must not read as a conflict: %v", err)
	}
	if second.Version != first.Version || second.Revision != first.Revision {
		t.Fatalf("the retry published another revision: %d/%d vs %d/%d",
			first.Version, first.Revision, second.Version, second.Revision)
	}
}

// TestAFailedRequestIsNotRemembered: caching a failure would make the retry
// impossible, which is the opposite of what the key is for.
func TestAFailedRequestIsNotRemembered(t *testing.T) {
	e := newEnv(t)
	missing := "rule-that-does-not-exist-" + runtime.NewID()[:8]

	if _, err := e.svc().RunRule(e.ctx(), missing, &RunRuleRequest{RequestID: "fail-k1"}); err == nil {
		t.Fatal("running a missing rule must fail")
	}
	if _, ok, _ := e.st.LookupRequest(e.ctx(), serviceTestTenant, "fail-k1"); ok {
		t.Fatal("a failed request must not be cached, or the retry can never succeed")
	}
}

// TestACreateAfterADeleteIsNotAnsweredByAGhost: create → delete → create the
// same thing again carries the same arguments and therefore the same key. The
// cached answer must not be replayed once the rule behind it is gone, or the
// API reports success while the rule list stays empty.
func TestACreateAfterADeleteIsNotAnsweredByAGhost(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a27-ghost-" + runtime.NewID()[:8])
	req := &CreateRuleRequest{
		Name: "删了又建的规则", Definition: thresholdRule(dev, "30", 0), RequestID: "ghost-k1",
	}

	first, err := e.svc().CreateRule(e.ctx(), req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := e.svc().DeleteRule(e.ctx(), first.ID, &DeleteRuleRequest{Force: true}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	again, err := e.svc().CreateRule(e.ctx(), req)
	if err != nil {
		t.Fatalf("create after delete: %v", err)
	}
	if again.ID == first.ID {
		t.Fatal("the deleted rule was replayed; the caller would see success and no rule")
	}
	if _, err := e.svc().GetRule(e.ctx(), again.ID); err != nil {
		t.Fatalf("the new rule must be readable: %v", err)
	}
}

// TestAKeyFromOneTenantCannotReplayAnothersAnswer: the key is scoped by tenant.
// A key that is only a hash of the arguments is shared between two customers
// who typed the same thing, and the second would receive the first one's rule.
func TestAKeyFromOneTenantCannotReplayAnothersAnswer(t *testing.T) {
	e := newEnv(t)
	key := "shared-" + runtime.NewID()[:8]

	if err := e.st.SaveRequest(e.ctx(), "tenant-a", key, "run_rule", []byte(`{"id":"run-a"}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, ok, err := e.st.LookupRequest(e.ctx(), "tenant-a", key); err != nil || !ok {
		t.Fatalf("the same tenant must find its own answer: ok=%v err=%v", ok, err)
	}
	if _, ok, err := e.st.LookupRequest(e.ctx(), "tenant-b", key); err != nil || ok {
		t.Fatalf("another tenant must not see it: ok=%v err=%v", ok, err)
	}
}
