package runtime

import (
	"context"
	"errors"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// Scheduling contract
//
// A schedule is business data: one row per schedule, created and deleted through
// the API. There is no source-code cron entry per schedule, so adding one never
// needs a deployment and deleting one cannot leave a ghost job.
//
// The unit of work is an *occurrence*, not a schedule. Each occurrence gets a row
// in auto_schedule_fires, unique on (schedule_id, scheduled_for). That unique
// index is what makes a duplicate wake-up, a retried claim and a restart all
// harmless: they collide instead of producing a second control command.
//
// States are separated so a crash can never look like success:
//
//	claimed  the occurrence was recorded, nothing has run yet
//	running  a worker holds the lease and is executing
//	done     the run finished (its own status says whether it succeeded)
//	failed   the attempt gave up, with the reason recorded
//	skipped  the policy decided not to run (rule disabled, misfire dropped)
//
// "done" is only written after the run exists, so there is no false "already
// executed" state.

const (
	// ScheduleLease is how long a claim stays valid. A worker that dies stops
	// blocking the occurrence once this expires.
	ScheduleLease = 30 * time.Second

	// MaxFireAttempts bounds recovery: an occurrence that keeps crashing is
	// failed with a recorded reason instead of being retried for ever.
	MaxFireAttempts = 5

	// ScheduleBatch bounds one scan. Due rows are read through the
	// next_fire_at index, never by scanning every schedule.
	ScheduleBatch = 50
)

// ScheduleTick summarises one pass, so an operator can see progress.
type ScheduleTick struct {
	Materialized int `json:"materialized"`
	Duplicates   int `json:"duplicates"`
	Misfired     int `json:"misfired"`
	Executed     int `json:"executed"`
	Skipped      int `json:"skipped"`
	Failed       int `json:"failed"`
	Recovered    int `json:"recovered"`
}

// ScheduleFromSpec builds a store row from an authored spec.
func ScheduleFromSpec(tenantID, ruleID, id string, spec domain.ScheduleSpec, now time.Time) (*store.Schedule, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	next, err := spec.NextAfter(now)
	if err != nil {
		return nil, err
	}
	sc := &store.Schedule{
		ID: id, TenantID: tenantID, RuleID: ruleID,
		Kind: string(spec.Kind), Cron: spec.Cron, Timezone: spec.Timezone,
		MisfirePolicy: string(spec.Misfire), Enabled: true,
	}
	if !spec.RunAt.IsZero() {
		runAt := spec.RunAt.UTC()
		sc.RunAt = &runAt
	}
	if !next.IsZero() {
		n := next.UTC()
		sc.NextFireAt = &n
	}
	return sc, nil
}

// SpecOf converts a stored schedule back into its authored form.
func SpecOf(sc *store.Schedule) domain.ScheduleSpec {
	spec := domain.ScheduleSpec{
		Kind:     domain.ScheduleKind(sc.Kind),
		Cron:     sc.Cron,
		Timezone: sc.Timezone,
		Misfire:  domain.MisfirePolicy(sc.MisfirePolicy),
	}
	if sc.RunAt != nil {
		spec.RunAt = *sc.RunAt
	}
	return spec
}

// FireDue materializes due occurrences and executes the ones that are claimable.
//
// The two phases are deliberately separate: materialization only records intent
// and moves the due time, execution only performs the run. A crash between them
// leaves a claimed occurrence that the next pass picks up, which is why the
// schedule never sits in a fake "already executed" state.
func (r *Runtime) FireDue(ctx context.Context, limit int) (ScheduleTick, error) {
	if limit <= 0 {
		limit = ScheduleBatch
	}
	out := ScheduleTick{}
	now := r.Now()

	due, err := r.store.ClaimDueSchedules(ctx, now, limit, r.workerID, ScheduleLease)
	if err != nil {
		return out, err
	}
	for _, sc := range due {
		if err := r.materialize(ctx, sc, now, &out); err != nil {
			// One broken schedule must not stop the others. The lease is released
			// without moving the due time, so a transient failure is retried on
			// the next pass rather than silently disabling the schedule.
			_ = r.store.ReleaseSchedule(ctx, sc.ID)
			out.Failed++
		}
	}

	fires, err := r.store.ClaimFires(ctx, now, limit, r.workerID, ScheduleLease)
	if err != nil {
		return out, err
	}
	for _, f := range fires {
		if f.Attempt > MaxFireAttempts {
			_ = r.store.FinishFire(ctx, f.ID, store.FireFailed, "",
				"超过恢复上限，停止重试")
			out.Failed++
			continue
		}
		if f.Attempt > 1 {
			out.Recovered++
		}
		r.executeFire(ctx, f, now, &out)
	}
	return out, nil
}

// materialize records the occurrence and moves the due time forward.
//
// The instant that was due is recorded even when it will not run, because "the
// 9:00 job did not run" and "the 9:00 job ran" are different answers and the
// operator needs to tell them apart. What never happens is a late occurrence
// being executed: the historic row is written as skipped, and at most one
// catch-up row is added when the policy allows it.
func (r *Runtime) materialize(ctx context.Context, sc *store.Schedule, now time.Time, out *ScheduleTick) error {
	if sc.NextFireAt == nil {
		return nil
	}
	scheduledFor := *sc.NextFireAt
	spec := SpecOf(sc)

	missed, err := spec.ResolveMissed(scheduledFor, now, domain.MisfireGrace)
	if err != nil {
		// The stored expression can no longer be resolved. Record why and stop
		// scheduling it instead of retrying for ever.
		id := NewID()
		if _, ierr := r.store.InsertFire(ctx, &store.Fire{
			ID: id, ScheduleID: sc.ID, TenantID: sc.TenantID, RuleID: sc.RuleID,
			ScheduledFor: scheduledFor, State: store.FireSkipped,
		}); ierr == nil {
			_ = r.store.FinishFire(ctx, id, store.FireSkipped, "", err.Error())
		}
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), sc.TenantID, "schedule.error", sc.ID,
			map[string]any{"schedule_id": sc.ID, "rule_id": sc.RuleID, "error": err.Error()})
		out.Failed++
		return r.store.AdvanceSchedule(ctx, sc.ID, nil, now)
	}

	onTime := now.Sub(scheduledFor) <= domain.MisfireGrace
	state := store.FireClaimed
	lateReason := ""
	if !onTime {
		// The instant has already passed, so this occurrence itself never runs.
		state = store.FireSkipped
		if missed.FireNow {
			lateReason = "错过执行时间：策略 run_once 改为在当前时刻补一次"
		} else {
			lateReason = "错过执行时间：策略 skip 不补发"
		}
	}

	fireID := NewID()
	inserted, err := r.store.InsertFire(ctx, &store.Fire{
		ID: fireID, ScheduleID: sc.ID, TenantID: sc.TenantID, RuleID: sc.RuleID,
		ScheduledFor: scheduledFor, State: state, Missed: missed.Missed,
	})
	if err != nil {
		return err
	}
	switch {
	case !inserted:
		// This instant was already materialized (a duplicate wake-up or a
		// retried claim). Nothing to do but keep the schedule moving.
		out.Duplicates++
	case onTime:
		out.Materialized++
	default:
		out.Skipped++
		_ = r.store.FinishFire(ctx, fireID, store.FireSkipped, "", lateReason)
	}

	if missed.Missed > 0 {
		out.Misfired++
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), sc.TenantID, "schedule.misfired", sc.ID,
			map[string]any{
				"schedule_id": sc.ID, "rule_id": sc.RuleID, "scheduled_for": scheduledFor,
				"missed": missed.Missed, "policy": string(spec.Misfire),
				"resume_at": missed.NextFire,
				"reason":    "进程未运行期间错过的周期不会逐条补发",
			})
	}

	// One catch-up occurrence, when the policy allows it.
	if missed.FireNow {
		if ok, err := r.store.InsertFire(ctx, &store.Fire{
			ID: NewID(), ScheduleID: sc.ID, TenantID: sc.TenantID, RuleID: sc.RuleID,
			ScheduledFor: now.Truncate(time.Second), State: store.FireClaimed, Missed: missed.Missed,
		}); err == nil && ok {
			out.Materialized++
		}
	}

	var next *time.Time
	if !missed.NextFire.IsZero() {
		n := missed.NextFire.UTC()
		next = &n
	}
	return r.store.AdvanceSchedule(ctx, sc.ID, next, now)
}

// executeFire performs one occurrence.
func (r *Runtime) executeFire(ctx context.Context, f *store.Fire, now time.Time, out *ScheduleTick) {
	rule, err := r.store.GetRule(ctx, f.TenantID, f.RuleID)
	if err != nil {
		_ = r.store.FinishFire(ctx, f.ID, store.FireSkipped, "", "规则不存在或已删除")
		out.Skipped++
		return
	}
	if !rule.Enabled {
		_ = r.store.FinishFire(ctx, f.ID, store.FireSkipped, "", "规则已停用")
		out.Skipped++
		return
	}

	// A retry after a crash may find the run already created. Reuse it instead
	// of starting a second execution.
	if existing, err := r.store.FindRunByTrigger(ctx, f.RuleID, f.ID); err == nil && existing != nil {
		_ = r.store.FinishFire(ctx, f.ID, store.FireDone, existing.ID, "")
		out.Executed++
		return
	}

	seq, err := r.store.NextSeq(ctx)
	if err != nil {
		_ = r.store.FinishFire(ctx, f.ID, store.FireFailed, "", err.Error())
		out.Failed++
		return
	}
	res := domain.Result{Verdict: domain.VerdictTrue, Reasons: []domain.Reason{{
		Code: "schedule_fired", Message: "定时触发：" + f.ScheduledFor.UTC().Format(time.RFC3339),
	}}}
	// A scheduled rule has no triggering device, so the run is scoped to the
	// rule and its targets come from the definition.
	run, err := r.acceptRun(ctx, rule, "", f.ID, cause{EventID: f.ID}, res, now, seq)
	if err != nil {
		_ = r.store.FinishFire(ctx, f.ID, store.FireFailed, "", err.Error())
		out.Failed++
		return
	}
	switch run.Status {
	case StatusIgnored, StatusRejected, StatusExpired:
		_ = r.store.FinishFire(ctx, f.ID, store.FireSkipped, run.ID, run.ReasonCode)
		out.Skipped++
	default:
		_ = r.store.FinishFire(ctx, f.ID, store.FireDone, run.ID, "")
		out.Executed++
	}
}

// PreviewSchedule returns the next few occurrences without storing anything, so
// the UI can show what an expression actually means before it is saved.
func (r *Runtime) PreviewSchedule(spec domain.ScheduleSpec, from time.Time, count int) ([]time.Time, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if count <= 0 || count > 20 {
		count = 5
	}
	var out []time.Time
	cursor := from
	for i := 0; i < count; i++ {
		next, err := spec.NextAfter(cursor)
		if err != nil {
			return nil, err
		}
		if next.IsZero() {
			break
		}
		out = append(out, next)
		cursor = next
	}
	return out, nil
}

// ErrScheduleNotFound is returned when a schedule id does not belong to the
// tenant or was deleted.
var ErrScheduleNotFound = errors.New("schedule not found")
