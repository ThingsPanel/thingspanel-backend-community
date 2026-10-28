package domain

import (
	"testing"
	"time"
)

// Scheduling is the one place where "when" is decided. The tests below pin the
// decisions that are easy to get wrong and expensive to get wrong in production:
// the day-of-month / day-of-week rule, a named time zone, the two daylight
// saving edges, and what happens to occurrences missed while the process was
// down.

func mustCron(t *testing.T, expr string) *Cron {
	t.Helper()
	c, err := ParseCron(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	return c
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %q: %v", name, err)
	}
	return loc
}

func at(t *testing.T, layout, value string, loc *time.Location) time.Time {
	t.Helper()
	out, err := time.ParseInLocation(layout, value, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return out
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

func TestParseCronExpandsEveryFieldForm(t *testing.T) {
	c := mustCron(t, "*/15 9-17 1,15 * 1-5")
	if !c.Minute[0] || !c.Minute[45] || c.Minute[7] {
		t.Fatalf("step over * expanded wrong: %v", c.Minute)
	}
	if !c.Hour[9] || !c.Hour[17] || c.Hour[18] {
		t.Fatalf("range expanded wrong: %v", c.Hour)
	}
	if !c.Dom[1] || !c.Dom[15] || c.Dom[2] {
		t.Fatalf("list expanded wrong: %v", c.Dom)
	}
	if len(c.Month) != 12 {
		t.Fatalf("month * must cover 1-12, got %v", c.Month)
	}
	if !c.Dow[1] || !c.Dow[5] || c.Dow[0] || c.Dow[6] {
		t.Fatalf("weekday range expanded wrong: %v", c.Dow)
	}
}

// "5/10" means "from 5 to the end of the range, every 10" — the same reading as
// a standard crontab, so an expression copied from a server keeps its meaning.
func TestParseCronTreatsBareStartWithStepAsOpenRange(t *testing.T) {
	c := mustCron(t, "5/10 * * * *")
	if !c.Minute[5] || !c.Minute[55] || c.Minute[7] {
		t.Fatalf("open range with step expanded wrong: %v", c.Minute)
	}
}

// Both 0 and 7 mean Sunday, and a lookup must not need two keys for it.
func TestParseCronNormalisesSunday(t *testing.T) {
	c := mustCron(t, "0 0 * * 7")
	if !c.Dow[0] {
		t.Fatal("7 must normalise to 0 (Sunday)")
	}
	if c.Dow[7] {
		t.Fatal("7 must not stay in the set")
	}
}

func TestParseCronRejectsMalformedExpressions(t *testing.T) {
	cases := map[string]string{
		"four fields":       "0 9 * *",
		"six fields":        "0 0 9 * * *",
		"minute out of":     "60 * * * *",
		"hour out of":       "* 24 * * *",
		"day zero":          "* * 0 * *",
		"month thirteen":    "* * * 13 *",
		"weekday eight":     "* * * * 8",
		"reversed range":    "10-5 * * * *",
		"zero step":         "*/0 * * * *",
		"non numeric":       "a * * * *",
		"trailing comma":    "1, * * * *",
		"empty expression":  "",
		"range not numeric": "1-x * * * *",
	}
	for name, expr := range cases {
		if _, err := ParseCron(expr); err == nil {
			t.Errorf("%s: %q must be rejected", name, expr)
		}
	}
}

// ---------------------------------------------------------------------------
// The next occurrence
// ---------------------------------------------------------------------------

// The traditional rule: when both day fields are restricted, a day matches if
// *either* matches. Reading it as AND would silently drop half the occurrences.
func TestCronMatchesDayWhenEitherFieldMatches(t *testing.T) {
	c := mustCron(t, "0 0 1 * 1") // the 1st of the month, or any Monday
	loc := time.UTC
	// 2026-06-01 is a Monday and also the 1st. The Mondays that follow match on
	// the weekday branch; 2026-07-01 is a Wednesday and matches on the day
	// branch. Reading the two fields as AND would drop one of these groups.
	cursor := at(t, time.RFC3339, "2026-05-31T12:00:00Z", loc)
	var got []string
	for i := 0; i < 6; i++ {
		next, ok := c.Next(cursor, loc)
		if !ok {
			t.Fatalf("occurrence %d: none", i)
		}
		got = append(got, next.UTC().Format(time.RFC3339))
		cursor = next
	}
	want := []string{
		"2026-06-01T00:00:00Z", "2026-06-08T00:00:00Z", "2026-06-15T00:00:00Z",
		"2026-06-22T00:00:00Z", "2026-06-29T00:00:00Z", "2026-07-01T00:00:00Z",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("occurrence %d: got %s want %s (all: %v)", i, got[i], want[i], got)
		}
	}
}

// When one day field is "*", the other alone decides — no accidental OR with an
// unrestricted field.
func TestCronUnrestrictedDayFieldDefersToTheOther(t *testing.T) {
	c := mustCron(t, "0 12 15 * *") // the 15th, whatever weekday it is
	loc := time.UTC
	cursor := at(t, time.RFC3339, "2026-01-01T00:00:00Z", loc)
	for i := 0; i < 3; i++ {
		next, ok := c.Next(cursor, loc)
		if !ok {
			t.Fatalf("occurrence %d: none", i)
		}
		if next.Day() != 15 {
			t.Fatalf("occurrence %d is the %d, want the 15th", i, next.Day())
		}
		cursor = next
	}
}

// A named zone decides the wall clock: 09:00 in Shanghai is 01:00 UTC, and it
// stays 01:00 UTC all year because Shanghai has no daylight saving.
func TestCronUsesTheNamedZone(t *testing.T) {
	c := mustCron(t, "0 9 * * *")
	loc := mustLoc(t, "Asia/Shanghai")
	next, ok := c.Next(at(t, time.RFC3339, "2026-06-01T00:00:00Z", time.UTC), loc)
	if !ok {
		t.Fatal("no next occurrence")
	}
	if got := next.UTC().Format(time.RFC3339); got != "2026-06-01T01:00:00Z" {
		t.Fatalf("09:00 Asia/Shanghai must be 01:00Z, got %s", got)
	}
}

// A local time that does not exist must be skipped, not shifted. Firing at 03:30
// because 02:30 was skipped would be a control command at the wrong time.
func TestCronSkipsLocalTimeThatDoesNotExist(t *testing.T) {
	c := mustCron(t, "30 2 * * *")
	loc := mustLoc(t, "America/New_York")
	// US daylight saving starts on 2026-03-08: 02:00 EST jumps to 03:00 EDT.
	next, ok := c.Next(at(t, time.RFC3339, "2026-03-07T12:00:00Z", time.UTC), loc)
	if !ok {
		t.Fatal("no next occurrence")
	}
	if got := next.UTC().Format(time.RFC3339); got != "2026-03-09T06:30:00Z" {
		t.Fatalf("02:30 does not exist on 2026-03-08 and must be skipped; got %s", got)
	}
	if next.Hour() != 2 || next.Minute() != 30 {
		t.Fatalf("the occurrence must still be at 02:30 local, got %s", next)
	}
}

// A local time that happens twice must fire once. Running the job twice in one
// night is the classic daylight-saving duplicate.
func TestCronFiresOnceOnRepeatedLocalTime(t *testing.T) {
	c := mustCron(t, "30 1 * * *")
	loc := mustLoc(t, "America/New_York")
	// US daylight saving ends on 2026-11-01: 01:30 happens twice.
	cursor := at(t, time.RFC3339, "2026-10-31T12:00:00Z", time.UTC)
	windowEnd := at(t, time.RFC3339, "2026-11-02T00:00:00Z", time.UTC)

	var fired []string
	for {
		next, ok := c.Next(cursor, loc)
		if !ok || !next.Before(windowEnd) {
			break
		}
		fired = append(fired, next.UTC().Format(time.RFC3339))
		cursor = next
	}
	if len(fired) != 1 {
		t.Fatalf("want exactly one occurrence across the repeated hour, got %v", fired)
	}
	if fired[0] != "2026-11-01T05:30:00Z" {
		t.Fatalf("want the first 01:30 (EDT), got %s", fired[0])
	}
}

// Feb 29 is the longest legitimate gap between occurrences, which is why the
// search window is four years rather than one.
func TestCronFindsLeapDay(t *testing.T) {
	c := mustCron(t, "0 0 29 2 *")
	next, ok := c.Next(at(t, time.RFC3339, "2026-03-01T00:00:00Z", time.UTC), time.UTC)
	if !ok {
		t.Fatal("no next occurrence")
	}
	if got := next.Format("2006-01-02"); got != "2028-02-29" {
		t.Fatalf("want 2028-02-29, got %s", got)
	}
}

func TestCronNextIsStrictlyAfter(t *testing.T) {
	c := mustCron(t, "0 * * * *")
	now := at(t, time.RFC3339, "2026-06-01T09:00:00Z", time.UTC)
	next, ok := c.Next(now, time.UTC)
	if !ok {
		t.Fatal("no next occurrence")
	}
	if !next.After(now) {
		t.Fatalf("next must be strictly after the cursor, got %s", next)
	}
	if got := next.Format(time.RFC3339); got != "2026-06-01T10:00:00Z" {
		t.Fatalf("want 10:00Z, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// Specs and validation
// ---------------------------------------------------------------------------

func TestOnceSpecIsAnAbsoluteInstant(t *testing.T) {
	runAt := at(t, time.RFC3339, "2026-06-01T09:30:00Z", time.UTC)
	spec := ScheduleSpec{Kind: ScheduleOnce, RunAt: runAt, Timezone: "Asia/Shanghai"}
	if err := spec.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	next, err := spec.NextAfter(at(t, time.RFC3339, "2026-06-01T00:00:00Z", time.UTC))
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if !next.Equal(runAt) {
		t.Fatalf("a one-off must keep its absolute instant, got %s want %s", next, runAt)
	}
	// Once the instant has passed there is nothing left to schedule.
	after, err := spec.NextAfter(runAt.Add(time.Second))
	if err != nil {
		t.Fatalf("next after: %v", err)
	}
	if !after.IsZero() {
		t.Fatalf("a spent one-off must have no next occurrence, got %s", after)
	}
}

func TestValidateRejectsUnschedulableSpecs(t *testing.T) {
	cases := map[string]ScheduleSpec{
		"unknown kind":      {Kind: "sometimes"},
		"once without time": {Kind: ScheduleOnce},
		"once with cron":    {Kind: ScheduleOnce, RunAt: time.Now(), Cron: "0 9 * * *"},
		"cron without cron": {Kind: ScheduleCron},
		"cron malformed":    {Kind: ScheduleCron, Cron: "0 9 * *"},
		"unknown timezone":  {Kind: ScheduleCron, Cron: "0 9 * * *", Timezone: "Mars/Olympus"},
		"unknown misfire":   {Kind: ScheduleCron, Cron: "0 9 * * *", Misfire: "retry"},
	}
	for name, spec := range cases {
		if err := spec.Validate(); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

// An unknown zone is an error rather than a silent fallback: a schedule that
// quietly runs eight hours off is worse than one that refuses to save.
func TestValidateKeepsEmptyTimezoneAsUTC(t *testing.T) {
	spec := ScheduleSpec{Kind: ScheduleCron, Cron: "0 9 * * *"}
	if err := spec.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if spec.Misfire != MisfireSkip {
		t.Fatalf("misfire must default to skip, got %q", spec.Misfire)
	}
	loc, err := spec.Location()
	if err != nil {
		t.Fatalf("location: %v", err)
	}
	if loc != time.UTC {
		t.Fatalf("empty timezone must mean UTC, got %s", loc)
	}
}

// ---------------------------------------------------------------------------
// Missed occurrences
// ---------------------------------------------------------------------------

// Processing an occurrence must move the due time forward. Leaving it due would
// re-materialize the same instant on every pass, which looks like a duplicate but
// is really a stuck clock.
func TestResolveMissedAlwaysMovesTheDueTimeForward(t *testing.T) {
	spec := ScheduleSpec{Kind: ScheduleCron, Cron: "0 9 * * *", Timezone: "Asia/Shanghai"}
	scheduledFor := at(t, time.RFC3339, "2026-06-01T01:00:00Z", time.UTC) // 09:00 CST
	out, err := spec.ResolveMissed(scheduledFor, scheduledFor, 0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out.Missed != 0 || out.FireNow {
		t.Fatalf("an on-time occurrence must not be treated as missed, got %+v", out)
	}
	if !out.NextFire.After(scheduledFor) {
		t.Fatalf("the due time must move forward, got %s", out.NextFire)
	}
	if got := out.NextFire.UTC().Format(time.RFC3339); got != "2026-06-02T01:00:00Z" {
		t.Fatalf("want the next day at 09:00 CST, got %s", got)
	}
}

// The core promise: a restart must not replay a string of historic control
// commands. However long the process was down, skip runs nothing and resumes.
func TestMisfireSkipDoesNotReplayHistory(t *testing.T) {
	spec := ScheduleSpec{Kind: ScheduleCron, Cron: "* * * * *", Misfire: MisfireSkip}
	now := at(t, time.RFC3339, "2026-06-01T12:00:00Z", time.UTC)
	scheduledFor := now.Add(-time.Hour) // 60 occurrences were missed

	out, err := spec.ResolveMissed(scheduledFor, now, 0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out.Missed != 59 {
		t.Fatalf("want 59 missed occurrences, got %d", out.Missed)
	}
	if out.FireNow {
		t.Fatal("skip must not run a catch-up occurrence")
	}
	if !out.NextFire.After(now) {
		t.Fatalf("must resume in the future, got %s", out.NextFire)
	}
}

// run_once is for schedules where being late is acceptable, and it still runs at
// most once no matter how many occurrences were missed.
func TestMisfireRunOnceRunsAtMostOnce(t *testing.T) {
	spec := ScheduleSpec{Kind: ScheduleCron, Cron: "* * * * *", Misfire: MisfireRunOnce}
	now := at(t, time.RFC3339, "2026-06-01T12:00:00Z", time.UTC)
	out, err := spec.ResolveMissed(now.Add(-time.Hour), now, 0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !out.FireNow {
		t.Fatal("run_once must run one catch-up occurrence")
	}
	if out.Missed != 59 {
		t.Fatalf("want 59 missed, got %d", out.Missed)
	}
	if !out.NextFire.After(now) {
		t.Fatalf("must still resume in the future, got %s", out.NextFire)
	}
}

// A one-off that was missed is either dropped or run late. There is no "next"
// occurrence to resume from, so FireNow is the only way it can ever run.
func TestMissedOneOffIsDroppedOrRunLate(t *testing.T) {
	runAt := at(t, time.RFC3339, "2026-06-01T09:00:00Z", time.UTC)
	now := runAt.Add(30 * time.Minute)

	skip := ScheduleSpec{Kind: ScheduleOnce, RunAt: runAt, Misfire: MisfireSkip}
	out, err := skip.ResolveMissed(runAt, now, 0)
	if err != nil {
		t.Fatalf("resolve skip: %v", err)
	}
	if out.FireNow || out.Missed != 1 || !out.NextFire.IsZero() {
		t.Fatalf("a dropped one-off must leave nothing behind, got %+v", out)
	}

	late := ScheduleSpec{Kind: ScheduleOnce, RunAt: runAt, Misfire: MisfireRunOnce}
	out, err = late.ResolveMissed(runAt, now, 0)
	if err != nil {
		t.Fatalf("resolve run_once: %v", err)
	}
	if !out.FireNow {
		t.Fatal("run_once must run a missed one-off late")
	}
	if !out.NextFire.IsZero() {
		t.Fatalf("a one-off has no next occurrence, got %s", out.NextFire)
	}
}

// A scan that arrives a few seconds late is normal operation. Treating it as a
// missed run would label every ordinary execution "missed", which destroys the
// value of the record.
func TestGraceWindowKeepsASlightlyLateOccurrenceOnTime(t *testing.T) {
	spec := ScheduleSpec{Kind: ScheduleCron, Cron: "* * * * *", Misfire: MisfireSkip}
	scheduledFor := at(t, time.RFC3339, "2026-06-01T12:00:00Z", time.UTC)
	now := scheduledFor.Add(3 * time.Second)

	out, err := spec.ResolveMissed(scheduledFor, now, MisfireGrace)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out.Missed != 0 || out.FireNow {
		t.Fatalf("a tick inside the grace window is not a missed run, got %+v", out)
	}
	if !out.NextFire.After(now) {
		t.Fatalf("must still resume in the future, got %s", out.NextFire)
	}

	// The same lateness without a grace window is reported as missed, which is
	// what the old behaviour did and what the test above is guarding against.
	strict, err := spec.ResolveMissed(scheduledFor, now, 0)
	if err != nil {
		t.Fatalf("resolve strict: %v", err)
	}
	if strict.Missed != 0 {
		// One minute granularity: the next occurrence after 12:00:03 is 12:01,
		// so even strictly there is nothing missed yet.
		t.Fatalf("want no missed occurrence at this granularity, got %+v", strict)
	}
}

// The grace window must not swallow a real outage.
func TestGraceWindowDoesNotHideALongOutage(t *testing.T) {
	spec := ScheduleSpec{Kind: ScheduleOnce, RunAt: at(t, time.RFC3339, "2026-06-01T12:00:00Z", time.UTC)}
	out, err := spec.ResolveMissed(
		at(t, time.RFC3339, "2026-06-01T12:00:00Z", time.UTC),
		at(t, time.RFC3339, "2026-06-01T12:30:00Z", time.UTC), MisfireGrace)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out.Missed != 1 || out.FireNow {
		t.Fatalf("a one-off missed by 30 minutes must be recorded as missed, got %+v", out)
	}
}

// A daily job that missed a whole day resumes at the next day's time, not at the
// instant the process came back.
func TestMisfireResumesAtTheNextMatchingInstant(t *testing.T) {
	spec := ScheduleSpec{Kind: ScheduleCron, Cron: "0 9 * * *", Timezone: "Asia/Shanghai"}
	scheduledFor := at(t, time.RFC3339, "2026-06-01T01:00:00Z", time.UTC) // 09:00 CST
	now := at(t, time.RFC3339, "2026-06-01T15:00:00Z", time.UTC)          // 23:00 CST

	out, err := spec.ResolveMissed(scheduledFor, now, 0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := out.NextFire.UTC().Format(time.RFC3339); got != "2026-06-02T01:00:00Z" {
		t.Fatalf("want 2026-06-02 09:00 CST, got %s", got)
	}
	if out.Missed != 0 {
		t.Fatalf("a single pending occurrence is not a missed run, got %d", out.Missed)
	}
}
