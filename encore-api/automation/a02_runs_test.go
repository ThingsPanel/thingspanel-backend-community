package automation

import (
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A02 remainder: the run log has to be a listing, not a sample
// ---------------------------------------------------------------------------
//
// The old endpoint returned the latest fifty runs and nothing else. A page that
// shows twenty of them could not say whether there were more, and "what failed
// yesterday" could not be asked at all. These tests are about the four things a
// pager needs: a count, a window, a status filter, and a refusal when the filter
// is misspelled.

// runTimes creates n finished runs, one minute apart, and returns their start
// times in the order they were created.
func (e *testEnv) runTimes(rule *store.Rule, device string, n int) []time.Time {
	e.t.Helper()
	out := make([]time.Time, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, e.now)
		if _, err := e.rt.RunNow(e.ctx(), rule, device); err != nil {
			e.t.Fatalf("run: %v", err)
		}
		e.advance(time.Minute)
	}
	return out
}

// TestTheRunLogReportsATotalNotJustAPage is the pager's only defence against
// "next" that never ends.
func TestTheRunLogReportsATotalNotJustAPage(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a02-1-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "会跑很多次的规则", thresholdRule(dev, "30", 0))
	e.runTimes(rule, dev, 5)

	page, err := e.svc().ListRuleRuns(e.ctx(), rule.ID, &ListRunsRequest{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 5 {
		t.Fatalf("want a total of 5, got %d", page.Total)
	}
	if len(page.Runs) != 2 {
		t.Fatalf("want one page of 2, got %d", len(page.Runs))
	}
	if page.Limit != 2 || page.Offset != 0 {
		t.Fatalf("the response must echo the window it used: %+v", page)
	}

	second, err := e.svc().ListRuleRuns(e.ctx(), rule.ID, &ListRunsRequest{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(second.Runs) != 2 {
		t.Fatalf("want 2 on the second page, got %d", len(second.Runs))
	}
	if second.Runs[0].ID == page.Runs[0].ID {
		t.Fatal("the second page repeated the first page's rows")
	}
}

// TestTheRunLogCanBeBoundedByTime: "what happened between these two moments" is
// the question an incident review asks.
func TestTheRunLogCanBeBoundedByTime(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a02-2-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "有时间窗的规则", thresholdRule(dev, "30", 0))
	times := e.runTimes(rule, dev, 4)

	from := times[1].Format(time.RFC3339)
	to := times[3].Format(time.RFC3339)
	page, err := e.svc().ListRuleRuns(e.ctx(), rule.ID, &ListRunsRequest{
		After: from, Before: to, Limit: 50,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("want the two runs inside the window, got %d", page.Total)
	}
	for _, r := range page.Runs {
		if r.StartedAt.Before(times[1]) || !r.StartedAt.Before(times[3]) {
			t.Fatalf("run outside the window: %s", r.StartedAt)
		}
	}
}

// TestTheRunLogFiltersByStatusSet: "what went wrong" is several statuses at once.
func TestTheRunLogFiltersByStatusSet(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a02-3-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "有成功有失败的规则", thresholdRule(dev, "30", 0))
	e.runTimes(rule, dev, 2)

	// One failed run, written directly: the point here is the filter, not the
	// path that produces a failure.
	if _, err := e.st.DB.ExecContext(e.ctx(), `
        INSERT INTO auto_runs (id, tenant_id, rule_id, revision, definition_hash, scope_key,
                               generation, status, started_at, finished_at)
        VALUES ($1,$2,$3,1,'h',$4,1,'failed',now(),now())`,
		runtime.NewID(), serviceTestTenant, rule.ID, "scope-failed-"+rule.ID); err != nil {
		t.Fatalf("insert failed run: %v", err)
	}

	ok, err := e.svc().ListRuleRuns(e.ctx(), rule.ID, &ListRunsRequest{Status: "succeeded", Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if ok.Total != 2 {
		t.Fatalf("want the two successful runs, got %d", ok.Total)
	}

	bad, err := e.svc().ListRuleRuns(e.ctx(), rule.ID, &ListRunsRequest{Status: "failed,expired", Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if bad.Total != 1 {
		t.Fatalf("want the one failed run, got %d", bad.Total)
	}
}

// TestAMisspelledStatusIsRefused: filtering on "faild" returns nothing, and an
// empty run log reads as "nothing failed".
func TestAMisspelledStatusIsRefused(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a02-4-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "状态拼错的规则", thresholdRule(dev, "30", 0))
	e.runTimes(rule, dev, 1)

	_, err := e.svc().ListRuleRuns(e.ctx(), rule.ID, &ListRunsRequest{Status: "faild"})
	if err == nil {
		t.Fatal("a misspelled status must be refused")
	}
	if code := errs.Code(err); code != errs.InvalidArgument {
		t.Fatalf("want invalid_argument, got %v (%v)", code, err)
	}
	if !strings.Contains(err.Error(), "faild") {
		t.Fatalf("the refusal should repeat the value back: %q", err.Error())
	}
}

// TestTheOldCallWithoutFiltersStillWorks: the legacy page calls this endpoint
// with no query string at all, and must keep getting its list.
func TestTheOldCallWithoutFiltersStillWorks(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a02-5-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "旧页面调用的规则", thresholdRule(dev, "30", 0))
	e.runTimes(rule, dev, 3)

	page, err := e.svc().ListRuleRuns(e.ctx(), rule.ID, nil)
	if err != nil {
		t.Fatalf("list without a request: %v", err)
	}
	if len(page.Runs) != 3 || page.Total != 3 {
		t.Fatalf("want the three runs, got %d of %d", len(page.Runs), page.Total)
	}
	if page.Limit <= 0 {
		t.Fatal("a default window has to be reported, not implied")
	}
}
