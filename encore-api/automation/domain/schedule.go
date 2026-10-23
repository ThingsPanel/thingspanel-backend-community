package domain

// Schedule timing: cron parsing, the next occurrence in a named time zone, and
// the explicit rules for daylight saving and missed occurrences.
//
// Everything here is pure: no clock, no database, no goroutine. That is what
// lets the same function decide "when is the next fire" in the runtime, in the
// API preview and in the tests.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ScheduleKind is either a one-off instant or a recurring expression.
type ScheduleKind string

const (
	ScheduleOnce ScheduleKind = "once"
	ScheduleCron ScheduleKind = "cron"
)

// MisfirePolicy decides what happens when the process was not running at the
// scheduled instant.
//
// The default is Skip, because a control command that runs late is often worse
// than one that never ran: nobody wants a valve opened four hours ago to be
// opened now. RunOnce exists for schedules where being late is acceptable, and
// it still runs at most once no matter how many occurrences were missed.
type MisfirePolicy string

const (
	MisfireSkip    MisfirePolicy = "skip"
	MisfireRunOnce MisfirePolicy = "run_once"
)

// MisfireGrace is how late a normal scan may be before an occurrence counts as
// missed. A tick that runs a few seconds after the due time is normal operation,
// not a missed run; only an occurrence older than this was genuinely not
// executed when it should have been.
//
// It lives here, next to the misfire rules, because the same number decides both
// "did it run" and "what do we record", and those two answers must agree.
const MisfireGrace = 2 * time.Minute

// ScheduleSpec is the authored part of a schedule.
type ScheduleSpec struct {
	Kind     ScheduleKind
	// Cron is the five-field expression, used when Kind is cron.
	Cron string
	// RunAt is the absolute instant, used when Kind is once. It is stored as an
	// absolute time, so a daylight-saving change cannot move it.
	RunAt time.Time
	// Timezone is an IANA name ("Asia/Shanghai"). Empty means UTC.
	Timezone string
	// Misfire is applied when the process was down at the scheduled instant.
	Misfire MisfirePolicy
}

// Location resolves the IANA time zone. An unknown name is an error rather than
// a silent fallback to UTC: a schedule that quietly runs eight hours off is
// worse than one that refuses to save.
func (s ScheduleSpec) Location() (*time.Location, error) {
	if strings.TrimSpace(s.Timezone) == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, fmt.Errorf("未知时区 %q（需要 IANA 名称，例如 Asia/Shanghai）", s.Timezone)
	}
	return loc, nil
}

// Cron is a parsed five-field expression.
//
// The fields are minute, hour, day-of-month, month and day-of-week. Day-of-week
// accepts 0 or 7 for Sunday. The day-of-month / day-of-week interaction follows
// the traditional rule: when both are restricted the day matches if *either*
// matches; when one is "*" the other alone decides.
type Cron struct {
	Minute, Hour, Dom, Month, Dow map[int]bool
	domRestricted, dowRestricted  bool
	expr                          string
}

// String returns the original expression.
func (c *Cron) String() string { return c.expr }

// ParseCron parses a five-field expression.
func ParseCron(expr string) (*Cron, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron 需要 5 个字段（分 时 日 月 周），实际 %d 个", len(fields))
	}
	minute, err := parseField(fields[0], 0, 59, "分")
	if err != nil {
		return nil, err
	}
	hour, err := parseField(fields[1], 0, 23, "时")
	if err != nil {
		return nil, err
	}
	dom, err := parseField(fields[2], 1, 31, "日")
	if err != nil {
		return nil, err
	}
	month, err := parseField(fields[3], 1, 12, "月")
	if err != nil {
		return nil, err
	}
	dow, err := parseField(fields[4], 0, 7, "周")
	if err != nil {
		return nil, err
	}
	// 7 and 0 both mean Sunday; normalize so lookups need one key.
	if dow[7] {
		dow[0] = true
		delete(dow, 7)
	}
	return &Cron{
		Minute: minute, Hour: hour, Dom: dom, Month: month, Dow: dow,
		domRestricted: fields[2] != "*",
		dowRestricted: fields[4] != "*",
		expr:          strings.Join(fields, " "),
	}, nil
}

// parseField expands one field into the set of matching values.
func parseField(field string, lo, hi int, name string) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("%s字段有空项", name)
		}
		step := 1
		if slash := strings.Index(part, "/"); slash >= 0 {
			n, err := strconv.Atoi(part[slash+1:])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("%s字段步长无效: %q", name, part)
			}
			step = n
			part = part[:slash]
		}
		var start, end int
		switch {
		case part == "*":
			start, end = lo, hi
		case strings.Contains(part, "-"):
			bounds := strings.SplitN(part, "-", 2)
			a, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			b, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("%s字段范围无效: %q", name, part)
			}
			start, end = a, b
		default:
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("%s字段不是数字: %q", name, part)
			}
			start, end = n, n
			if step > 1 {
				// "5/10" means "from 5 to the end of the range, every 10".
				end = hi
			}
		}
		if start < lo || end > hi || start > end {
			return nil, fmt.Errorf("%s字段超出范围 %d-%d: %q", name, lo, hi, part)
		}
		for v := start; v <= end; v += step {
			out[v] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s字段没有匹配值", name)
	}
	return out, nil
}

// matchDay applies the traditional day-of-month / day-of-week rule.
func (c *Cron) matchDay(t time.Time) bool {
	if !c.Month[int(t.Month())] {
		return false
	}
	domHit := c.Dom[t.Day()]
	dowHit := c.Dow[int(t.Weekday())]
	switch {
	case c.domRestricted && c.dowRestricted:
		return domHit || dowHit
	case c.domRestricted:
		return domHit
	case c.dowRestricted:
		return dowHit
	default:
		return true
	}
}

// Next returns the first occurrence strictly after `after`, evaluated in loc.
//
// Daylight saving is handled explicitly:
//   - a local time that does not exist (spring forward) is skipped, because
//     firing at 02:30 when the clock jumps 02:00 -> 03:00 would silently run at
//     03:30 instead;
//   - a local time that exists twice (fall back) fires on the first occurrence
//     only, so the job does not run twice in one night.
func (c *Cron) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	// Start from the next whole minute in the schedule's own zone.
	cur := after.In(loc).Truncate(time.Minute).Add(time.Minute)
	// Four years covers any expression that can ever match again (Feb 29 is the
	// longest gap); beyond that the expression is treated as unsatisfiable.
	limit := cur.AddDate(4, 0, 0)

	for day := cur; day.Before(limit); day = day.AddDate(0, 0, 1) {
		// Normalize the day to local midnight so the hour/minute scan below is
		// independent of where `cur` happened to start.
		midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
		if !c.matchDay(midnight) {
			continue
		}
		for h := 0; h < 24; h++ {
			if !c.Hour[h] {
				continue
			}
			for m := 0; m < 60; m++ {
				if !c.Minute[m] {
					continue
				}
				candidate := time.Date(midnight.Year(), midnight.Month(), midnight.Day(), h, m, 0, 0, loc)
				// A nonexistent local time is normalized by time.Date to a
				// different hour; skip it instead of firing at the shifted time.
				if candidate.Hour() != h || candidate.Minute() != m {
					continue
				}
				if candidate.After(after) {
					return candidate, true
				}
			}
		}
	}
	return time.Time{}, false
}

// NextAfter computes the next fire for a spec, strictly after `after`.
func (s ScheduleSpec) NextAfter(after time.Time) (time.Time, error) {
	loc, err := s.Location()
	if err != nil {
		return time.Time{}, err
	}
	switch s.Kind {
	case ScheduleOnce:
		if s.RunAt.IsZero() {
			return time.Time{}, fmt.Errorf("单次定时必须给出执行时刻")
		}
		if !s.RunAt.After(after) {
			return time.Time{}, nil // already due, nothing left to schedule
		}
		return s.RunAt.In(loc), nil
	case ScheduleCron:
		c, err := ParseCron(s.Cron)
		if err != nil {
			return time.Time{}, err
		}
		next, ok := c.Next(after, loc)
		if !ok {
			return time.Time{}, fmt.Errorf("cron 表达式在可预见范围内没有匹配时刻: %q", s.Cron)
		}
		return next, nil
	}
	return time.Time{}, fmt.Errorf("未知定时类型 %q", s.Kind)
}

// Validate checks a spec before it is stored, so an unschedulable expression can
// never reach the database.
func (s *ScheduleSpec) Validate() error {
	if s.Misfire == "" {
		s.Misfire = MisfireSkip
	}
	switch s.Misfire {
	case MisfireSkip, MisfireRunOnce:
	default:
		return fmt.Errorf("未知漏跑策略 %q（只能是 skip 或 run_once）", s.Misfire)
	}
	if _, err := s.Location(); err != nil {
		return err
	}
	switch s.Kind {
	case ScheduleOnce:
		if s.RunAt.IsZero() {
			return fmt.Errorf("单次定时必须给出执行时刻")
		}
		if s.Cron != "" {
			return fmt.Errorf("单次定时不能同时给出 cron 表达式")
		}
	case ScheduleCron:
		if strings.TrimSpace(s.Cron) == "" {
			return fmt.Errorf("周期定时必须给出 cron 表达式")
		}
		if _, err := ParseCron(s.Cron); err != nil {
			return err
		}
	default:
		return fmt.Errorf("未知定时类型 %q（只能是 once 或 cron）", s.Kind)
	}
	return nil
}

// MissedOutcome describes what to do about occurrences that were missed while
// the process was down.
type MissedOutcome struct {
	// NextFire is where the schedule resumes.
	NextFire time.Time
	// Missed counts the occurrences that will not run.
	Missed int
	// FireNow says whether one catch-up run should happen at the current time.
	FireNow bool
}

// ResolveMissed applies the misfire policy and says where the schedule resumes.
//
// This is the rule that stops a restart from replaying a string of historic
// control commands: however long the process was down, at most one catch-up run
// happens, and only if the policy allows it.
//
// It is also the rule that makes the due time move forward. Every call returns
// the instant the schedule resumes at, and that instant is always strictly after
// now, so processing one occurrence can never leave the same occurrence due
// again — a schedule that stays due would be re-materialized on every pass and
// would only be stopped by the unique index, which looks like a duplicate rather
// than the stuck clock it really is.
func (s ScheduleSpec) ResolveMissed(scheduledFor, now time.Time, grace time.Duration) (MissedOutcome, error) {
	late := scheduledFor.Add(grace).Before(now)

	if s.Kind == ScheduleOnce {
		// A one-off has no next occurrence. On time it runs now and is then
		// spent; missed, it is either run late or dropped.
		if !late {
			return MissedOutcome{}, nil
		}
		if s.Misfire == MisfireRunOnce {
			return MissedOutcome{FireNow: true}, nil
		}
		return MissedOutcome{Missed: 1}, nil
	}

	loc, err := s.Location()
	if err != nil {
		return MissedOutcome{}, err
	}
	c, err := ParseCron(s.Cron)
	if err != nil {
		return MissedOutcome{}, err
	}

	// Count how many occurrences were skipped. The count is bounded so a
	// schedule that fires every minute and a week of downtime cannot make this
	// loop for ever.
	missed := 0
	if late {
		cursor := scheduledFor
		for {
			next, ok := c.Next(cursor, loc)
			if !ok || !next.Before(now) {
				break
			}
			missed++
			cursor = next
			if missed >= 10000 {
				break
			}
		}
	}
	resume, ok := c.Next(now, loc)
	if !ok {
		return MissedOutcome{}, fmt.Errorf("cron 表达式在可预见范围内没有匹配时刻: %q", s.Cron)
	}
	out := MissedOutcome{NextFire: resume, Missed: missed}
	if s.Misfire == MisfireRunOnce && missed > 0 {
		out.FireNow = true
	}
	return out, nil
}
