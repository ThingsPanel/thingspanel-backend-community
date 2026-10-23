package automation

import (
	"context"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// A07: timed triggers.
//
// The defect these tests guard against is not "the job did not run". It is "the
// job ran twice", "the job ran four hours late", and "the job could not be
// deleted". Each test below fails on one of those, on the real store, through
// the same Tick the service endpoint calls.

// createSchedule stores a validated schedule for a rule, failing the test if the
// spec is not schedulable. It uses the same constructor the API uses.
//
// The row is removed on cleanup: a due schedule from an earlier test would be
// picked up by this test's tick, because a worker deliberately scans every
// tenant and not just one.
func (e *testEnv) createSchedule(ruleID string, spec domain.ScheduleSpec) *store.Schedule {
	e.t.Helper()
	sc, err := runtime.ScheduleFromSpec(e.tenant, ruleID, runtime.NewID(), spec, e.now)
	if err != nil {
		e.t.Fatalf("schedule spec is not schedulable: %v", err)
	}
	if err := e.st.CreateSchedule(e.ctx(), sc); err != nil {
		e.t.Fatalf("create schedule: %v", err)
	}
	e.t.Cleanup(func() {
		_, _ = e.st.DB.ExecContext(context.Background(),
			`DELETE FROM auto_schedule_fires WHERE schedule_id=$1`, sc.ID)
		_, _ = e.st.DB.ExecContext(context.Background(),
			`DELETE FROM auto_schedules WHERE id=$1`, sc.ID)
	})
	stored, err := e.st.GetSchedule(e.ctx(), e.tenant, sc.ID)
	if err != nil {
		e.t.Fatalf("read back schedule: %v", err)
	}
	return stored
}

// fires reads the occurrence history of a schedule.
func (e *testEnv) fires(scheduleID string) []*store.Fire {
	e.t.Helper()
	out, err := e.st.ListFires(e.ctx(), scheduleID, 100)
	if err != nil {
		e.t.Fatalf("list fires: %v", err)
	}
	return out
}

// fireStates flattens the history into a state list for readable assertions.
func (e *testEnv) fireStates(scheduleID string) []string {
	e.t.Helper()
	var out []string
	for _, f := range e.fires(scheduleID) {
		out = append(out, f.State)
	}
	return out
}

// forceDue rewinds the due time, which is how a test replays the same instant a
// second time. It is the only way to exercise the duplicate path deterministically.
func (e *testEnv) forceDue(scheduleID string, due time.Time) {
	e.t.Helper()
	if err := e.st.AdvanceSchedule(e.ctx(), scheduleID, &due, e.now); err != nil {
		e.t.Fatalf("force due: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The happy path
// ---------------------------------------------------------------------------

// A due occurrence runs the rule exactly once and its control command leaves in
// the same pass.
func TestScheduleFiresExactlyOnce(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})

	due := *sc.NextFireAt
	e.now = due
	res, err := e.rt.Tick(e.ctx())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}

	if res.ScheduleMaterialized != 1 || res.ScheduleExecuted != 1 {
		t.Fatalf("want 1 materialized and 1 executed, got %+v", res)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("want 1 run, got %d", n)
	}
	if n := e.cmds.count(); n != 1 {
		t.Fatalf("want 1 device command, got %d", n)
	}
	fires := e.fires(sc.ID)
	if len(fires) != 1 || fires[0].State != store.FireDone {
		t.Fatalf("want one done occurrence, got %v", e.fireStates(sc.ID))
	}
	if fires[0].RunID == "" {
		t.Fatal("a done occurrence must point at its run")
	}

	// The due time moved on, so the same instant cannot fire again.
	after, err := e.st.GetSchedule(e.ctx(), e.tenant, sc.ID)
	if err != nil {
		t.Fatalf("reread schedule: %v", err)
	}
	if after.NextFireAt == nil || !after.NextFireAt.After(due) {
		t.Fatalf("next fire must move forward, got %v", after.NextFireAt)
	}
}

// A repeated wake-up for an instant that was already materialized must collide
// on (schedule_id, scheduled_for) instead of starting a second run.
func TestRepeatedWakeUpDoesNotFireTwice(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})

	due := *sc.NextFireAt
	e.now = due
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	// Replay the same instant, which is what a second worker or a restarted
	// process sees.
	e.forceDue(sc.ID, due)
	res, err := e.rt.Tick(e.ctx())
	if err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if res.ScheduleDuplicates != 1 {
		t.Fatalf("the repeated instant must be reported as a duplicate, got %+v", res)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("a repeated wake-up must not run twice, got %d runs", n)
	}
	if n := e.cmds.count(); n != 1 {
		t.Fatalf("a repeated wake-up must not send a second command, got %d", n)
	}
	if n := len(e.fires(sc.ID)); n != 1 {
		t.Fatalf("the occurrence must exist once, got %v", e.fireStates(sc.ID))
	}
}

// Two workers scanning at the same instant must not both take the same schedule.
func TestDueScheduleIsLeasedToOneWorker(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})
	due := *sc.NextFireAt

	first, err := e.st.ClaimDueSchedules(e.ctx(), due, 10, "worker-a", runtime.ScheduleLease)
	if err != nil {
		t.Fatalf("claim a: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("the first worker must get the schedule, got %d", len(first))
	}
	second, err := e.st.ClaimDueSchedules(e.ctx(), due, 10, "worker-b", runtime.ScheduleLease)
	if err != nil {
		t.Fatalf("claim b: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("the second worker must not get a leased schedule, got %d", len(second))
	}

	// Once the lease expires the schedule is claimable again, so a dead worker
	// cannot block it for ever.
	later := due.Add(runtime.ScheduleLease + time.Second)
	third, err := e.st.ClaimDueSchedules(e.ctx(), later, 10, "worker-b", runtime.ScheduleLease)
	if err != nil {
		t.Fatalf("claim c: %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("an expired lease must be claimable, got %d", len(third))
	}
}

// ---------------------------------------------------------------------------
// Crash recovery
// ---------------------------------------------------------------------------

// A crash between recording the occurrence and executing it must leave work that
// the next pass picks up, not a lost job and not a fake "done".
func TestClaimedOccurrenceIsExecutedAfterACrash(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})
	due := *sc.NextFireAt

	// Reproduce the crash: the occurrence row exists, nothing ran, and the due
	// time has already moved on.
	if _, err := e.st.InsertFire(e.ctx(), &store.Fire{
		ID: runtime.NewID(), ScheduleID: sc.ID, TenantID: e.tenant, RuleID: rule.ID,
		ScheduledFor: due, State: store.FireClaimed,
	}); err != nil {
		t.Fatalf("insert claimed occurrence: %v", err)
	}
	next := due.Add(5 * time.Minute)
	if err := e.st.AdvanceSchedule(e.ctx(), sc.ID, &next, due); err != nil {
		t.Fatalf("advance: %v", err)
	}

	e.now = due
	res, err := e.rt.Tick(e.ctx())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if res.ScheduleExecuted != 1 {
		t.Fatalf("the crashed occurrence must be executed, got %+v", res)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("want 1 run, got %d", n)
	}
	fires := e.fires(sc.ID)
	if len(fires) != 1 || fires[0].State != store.FireDone {
		t.Fatalf("want one done occurrence, got %v", e.fireStates(sc.ID))
	}

	// A second pass has nothing left to do.
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("recovery must not duplicate the run, got %d", n)
	}
	if n := e.cmds.count(); n != 1 {
		t.Fatalf("recovery must not duplicate the command, got %d", n)
	}
}

// A crash *during* execution is the harder case: the run already exists, so the
// retry must adopt it rather than execute the body a second time.
func TestCrashDuringExecutionAdoptsTheExistingRun(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})
	due := *sc.NextFireAt
	e.now = due
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("setup: want 1 run, got %d", n)
	}

	// Rewind the occurrence to "a worker started it and died", keeping the run.
	fires := e.fires(sc.ID)
	if _, err := e.st.DB.ExecContext(e.ctx(), `
        UPDATE auto_schedule_fires
           SET state='running', attempt=1, run_id=NULL, lease_owner=NULL, lease_until=NULL
         WHERE id=$1`, fires[0].ID); err != nil {
		t.Fatalf("simulate crash: %v", err)
	}

	res, err := e.rt.Tick(e.ctx())
	if err != nil {
		t.Fatalf("recovery tick: %v", err)
	}
	if res.ScheduleRecovered != 1 {
		t.Fatalf("the retry must be counted as a recovery, got %+v", res)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("the retry must adopt the existing run, got %d runs", n)
	}
	if n := e.cmds.count(); n != 1 {
		t.Fatalf("the retry must not send a second command, got %d", n)
	}
	fires = e.fires(sc.ID)
	if fires[0].State != store.FireDone || fires[0].RunID == "" {
		t.Fatalf("want done with its run id, got %s / %q", fires[0].State, fires[0].RunID)
	}
}

// An occurrence that keeps crashing must stop being retried, with the reason
// recorded, instead of looping for ever.
func TestOccurrenceGivesUpAfterMaxAttempts(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})
	due := *sc.NextFireAt
	fireID := runtime.NewID()
	if _, err := e.st.InsertFire(e.ctx(), &store.Fire{
		ID: fireID, ScheduleID: sc.ID, TenantID: e.tenant, RuleID: rule.ID,
		ScheduledFor: due, State: store.FireRunning,
	}); err != nil {
		t.Fatalf("insert occurrence: %v", err)
	}
	if _, err := e.st.DB.ExecContext(e.ctx(), `
        UPDATE auto_schedule_fires SET attempt=$2 WHERE id=$1`,
		fireID, runtime.MaxFireAttempts); err != nil {
		t.Fatalf("set attempt: %v", err)
	}

	e.now = due
	res, err := e.rt.Tick(e.ctx())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if res.ScheduleFailed != 1 {
		t.Fatalf("want 1 failed, got %+v", res)
	}
	f, err := e.st.GetFire(e.ctx(), fireID)
	if err != nil {
		t.Fatalf("read fire: %v", err)
	}
	if f.State != store.FireFailed || f.Error == "" {
		t.Fatalf("want a failed occurrence with a reason, got %s / %q", f.State, f.Error)
	}
	if n := len(e.runs(rule.ID)); n != 0 {
		t.Fatalf("a given-up occurrence must not run, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Ownership: no ghost jobs
// ---------------------------------------------------------------------------

// Disabling a rule must silence its schedules without destroying them, so
// re-enabling resumes instead of losing the job.
func TestDisabledRuleFiresNothing(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})
	due := *sc.NextFireAt

	fresh, err := e.st.GetRule(e.ctx(), e.tenant, rule.ID)
	if err != nil {
		t.Fatalf("reread rule: %v", err)
	}
	if _, err := e.st.SetEnabled(e.ctx(), e.tenant, rule.ID, false, fresh.Version); err != nil {
		t.Fatalf("disable rule: %v", err)
	}
	e.now = due
	res, err := e.rt.Tick(e.ctx())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if res.ScheduleMaterialized != 0 || res.ScheduleExecuted != 0 {
		t.Fatalf("a disabled rule must fire nothing, got %+v", res)
	}
	if n := len(e.fires(sc.ID)); n != 0 {
		t.Fatalf("a disabled rule must not materialize occurrences, got %v", e.fireStates(sc.ID))
	}
	if n := e.cmds.count(); n != 0 {
		t.Fatalf("a disabled rule must not command a device, got %d", n)
	}

	// Re-enabling picks the schedule back up: the due time is still there.
	reloaded, err := e.st.GetSchedule(e.ctx(), e.tenant, sc.ID)
	if err != nil {
		t.Fatalf("reread schedule: %v", err)
	}
	if reloaded.NextFireAt == nil {
		t.Fatal("disabling a rule must not erase the schedule's due time")
	}
	fresh, err = e.st.GetRule(e.ctx(), e.tenant, rule.ID)
	if err != nil {
		t.Fatalf("reread rule: %v", err)
	}
	if _, err := e.st.SetEnabled(e.ctx(), e.tenant, rule.ID, true, fresh.Version); err != nil {
		t.Fatalf("re-enable rule: %v", err)
	}
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick after enable: %v", err)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("re-enabling must resume the schedule, got %d runs", n)
	}
}

// A deleted schedule must not fire, even if its due time was already reached.
func TestDeletedScheduleNeverFires(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})
	due := *sc.NextFireAt

	if err := e.st.DeleteSchedule(e.ctx(), e.tenant, sc.ID); err != nil {
		t.Fatalf("delete schedule: %v", err)
	}
	e.now = due
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if n := len(e.runs(rule.ID)); n != 0 {
		t.Fatalf("a deleted schedule must not run, got %d", n)
	}
	if n := len(e.fires(sc.ID)); n != 0 {
		t.Fatalf("a deleted schedule must not materialize, got %v", e.fireStates(sc.ID))
	}
	if _, err := e.st.GetSchedule(e.ctx(), e.tenant, sc.ID); err == nil {
		t.Fatal("a deleted schedule must not be readable")
	}
}

// Deleting a rule must cancel its schedules: no schedule may outlive its owner.
func TestDeletingRuleCancelsItsSchedules(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})
	due := *sc.NextFireAt

	if err := e.st.DeleteRule(e.ctx(), e.tenant, rule.ID); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	if err := e.st.CancelSchedulesForRule(e.ctx(), rule.ID); err != nil {
		t.Fatalf("cancel schedules: %v", err)
	}

	e.now = due
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if n := len(e.fires(sc.ID)); n != 0 {
		t.Fatalf("an orphaned schedule must not fire, got %v", e.fireStates(sc.ID))
	}
	cancelled, err := e.st.GetSchedule(e.ctx(), e.tenant, sc.ID)
	if err != nil {
		t.Fatalf("reread schedule: %v", err)
	}
	if cancelled.Enabled || cancelled.NextFireAt != nil {
		t.Fatalf("cancelling must clear the flag and the due time, got %+v", cancelled)
	}
}

// ---------------------------------------------------------------------------
// Missed occurrences
// ---------------------------------------------------------------------------

// The promise that matters most after an outage: a restart must not replay a
// string of historic control commands. With skip, nothing runs and the schedule
// resumes at the next future instant.
func TestMisfireSkipReplaysNoCommands(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "* * * * *", Timezone: "UTC",
		Misfire: domain.MisfireSkip,
	})
	due := *sc.NextFireAt

	// The process was down for half an hour.
	e.now = due.Add(30 * time.Minute)
	res, err := e.rt.Tick(e.ctx())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if res.ScheduleMisfired != 1 {
		t.Fatalf("the missed window must be reported, got %+v", res)
	}
	if n := len(e.runs(rule.ID)); n != 0 {
		t.Fatalf("skip must replay no command, got %d runs", n)
	}
	if n := e.cmds.count(); n != 0 {
		t.Fatalf("skip must send no command, got %d", n)
	}
	fires := e.fires(sc.ID)
	if len(fires) != 1 {
		t.Fatalf("the dropped occurrence must still be recorded, got %v", e.fireStates(sc.ID))
	}
	if fires[0].State != store.FireSkipped || fires[0].Error == "" {
		t.Fatalf("want a skipped occurrence with a reason, got %s / %q", fires[0].State, fires[0].Error)
	}
	if fires[0].Missed != 29 {
		t.Fatalf("want 29 missed occurrences recorded, got %d", fires[0].Missed)
	}
	if len(e.eventsOfKind("schedule.misfired")) != 1 {
		t.Fatal("a missed window must publish one schedule.misfired event")
	}

	resumed, err := e.st.GetSchedule(e.ctx(), e.tenant, sc.ID)
	if err != nil {
		t.Fatalf("reread schedule: %v", err)
	}
	if resumed.NextFireAt == nil || !resumed.NextFireAt.After(e.now) {
		t.Fatalf("must resume in the future, got %v", resumed.NextFireAt)
	}
}

// run_once is for schedules where being late is acceptable, and it still runs at
// most once however long the outage was.
func TestMisfireRunOnceCatchesUpExactlyOnce(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "* * * * *", Timezone: "UTC",
		Misfire: domain.MisfireRunOnce,
	})
	due := *sc.NextFireAt

	e.now = due.Add(30 * time.Minute)
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("run_once must catch up exactly once, got %d runs", n)
	}
	if n := e.cmds.count(); n != 1 {
		t.Fatalf("run_once must send exactly one command, got %d", n)
	}
	states := e.fireStates(sc.ID)
	if len(states) != 2 {
		t.Fatalf("want the dropped occurrence plus one catch-up, got %v", states)
	}
}

// A normal tick a few seconds after the due time is not a missed run.
func TestSlightlyLateTickStillRuns(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC",
	})
	due := *sc.NextFireAt

	e.now = due.Add(3 * time.Second)
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("a tick inside the grace window must still run, got %d runs", n)
	}
	if states := e.fireStates(sc.ID); len(states) != 1 || states[0] != store.FireDone {
		t.Fatalf("want one done occurrence, got %v", states)
	}
}

// ---------------------------------------------------------------------------
// One-off schedules
// ---------------------------------------------------------------------------

// A one-off fires at its instant and then stops. It must not linger as an
// enabled schedule with no due time.
func TestOneOffFiresOnceAndStops(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	runAt := e.now.Add(10 * time.Minute)
	sc := e.createSchedule(rule.ID, domain.ScheduleSpec{
		Kind: domain.ScheduleOnce, RunAt: runAt, Timezone: "UTC",
	})
	if sc.NextFireAt == nil || !sc.NextFireAt.Equal(runAt) {
		t.Fatalf("a one-off must be due at its own instant, got %v", sc.NextFireAt)
	}

	e.now = runAt
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("want 1 run, got %d", n)
	}

	e.now = runAt.Add(time.Hour)
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick later: %v", err)
	}
	if n := len(e.runs(rule.ID)); n != 1 {
		t.Fatalf("a spent one-off must not run again, got %d", n)
	}
	after, err := e.st.GetSchedule(e.ctx(), e.tenant, sc.ID)
	if err != nil {
		t.Fatalf("reread schedule: %v", err)
	}
	if after.Enabled || after.NextFireAt != nil {
		t.Fatalf("a spent one-off must be disabled with no due time, got %+v", after)
	}
}

// ---------------------------------------------------------------------------
// Preview
// ---------------------------------------------------------------------------

// The preview is what the editor shows before saving, so it must agree with the
// runtime's own arithmetic.
func TestPreviewMatchesTheRuntime(t *testing.T) {
	e := newEnv(t)
	spec := domain.ScheduleSpec{Kind: domain.ScheduleCron, Cron: "0 9 * * 1-5", Timezone: "Asia/Shanghai"}
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // a Monday
	times, err := e.rt.PreviewSchedule(spec, from, 3)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(times) != 3 {
		t.Fatalf("want 3 occurrences, got %d", len(times))
	}
	for _, ts := range times {
		local := ts.In(time.FixedZone("CST", 8*3600))
		if local.Hour() != 9 || local.Minute() != 0 {
			t.Fatalf("occurrence must be 09:00 local, got %s", local)
		}
		if wd := local.Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Fatalf("weekday-only schedule produced %s", local)
		}
	}
	// Consecutive occurrences are one day apart across the week.
	if gap := times[1].Sub(times[0]); gap != 24*time.Hour {
		t.Fatalf("want 24h between consecutive weekdays, got %s", gap)
	}
}
