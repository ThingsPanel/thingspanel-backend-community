package runtime

import (
	"context"
	"strings"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// TickResult summarises one scheduler pass, which is what the readiness and
// backlog endpoints report.
type TickResult struct {
	PendingExamined int `json:"pending_examined"`
	PendingFired    int `json:"pending_fired"`
	PendingDeferred int `json:"pending_deferred"`
	PendingDropped  int `json:"pending_dropped"`
	RunTimers       int `json:"run_timers"`
	OutboxDrained   int `json:"outbox_drained"`

	// Schedule counters. They are separate from the condition timers because a
	// scheduled occurrence is a different thing from a `for` deadline.
	ScheduleMaterialized int `json:"schedule_materialized"`
	ScheduleDuplicates   int `json:"schedule_duplicates"`
	ScheduleMisfired     int `json:"schedule_misfired"`
	ScheduleExecuted     int `json:"schedule_executed"`
	ScheduleSkipped      int `json:"schedule_skipped"`
	ScheduleFailed       int `json:"schedule_failed"`
	ScheduleRecovered    int `json:"schedule_recovered"`

	// NoDataNoted counts alarm episodes that were marked "cannot be confirmed"
	// because their subject stopped reporting.
	NoDataNoted int `json:"no_data_noted"`

	// AlarmRelayed counts alarm transitions turned into rule inputs in this pass.
	AlarmRelayed int `json:"alarm_relayed"`

	// Run bookkeeping. A run that nobody finishes is worse than a failed one:
	// it reads as "still working" forever. Expiry and resume make both outcomes
	// explicit.
	RunsExpired int `json:"runs_expired"`
	RunsResumed int `json:"runs_resumed"`
}

// Tick runs one scheduler pass: due condition timers, due run continuations and
// pending outbound intents. It is idempotent and safe to run from several
// workers because every claim uses SKIP LOCKED.
func (r *Runtime) Tick(ctx context.Context) (TickResult, error) {
	var out TickResult
	now := r.Now()

	pendings, err := r.store.DuePending(ctx, now, 50)
	if err != nil {
		return out, err
	}
	out.PendingExamined = len(pendings)
	for _, p := range pendings {
		switch r.firePending(ctx, p, now) {
		case "fired":
			out.PendingFired++
		case "deferred":
			out.PendingDeferred++
		case "dropped":
			out.PendingDropped++
		}
	}

	// Runs whose budget is spent are closed first, before any continuation is
	// considered. Otherwise a wait that comes due at the same moment as the
	// deadline would resume a run that is no longer allowed to run, and the
	// expiry would silently never happen.
	if expired, eerr := r.store.ExpireOverdueRuns(ctx, now, 50); eerr == nil {
		out.RunsExpired = len(expired)
		for _, id := range expired {
			_ = r.store.CancelRunTimers(ctx, id)
			_ = r.store.CancelRunOutbox(ctx, id)
		}
	}

	timers, err := r.store.DueRunTimers(ctx, now, 50)
	if err != nil {
		return out, err
	}
	out.RunTimers = len(timers)
	for _, t := range timers {
		if _, err := r.executeRunFromPC(ctx, t.RunID, t.PC+1); err != nil {
			// A run that cannot resume is finished with a readable reason by
			// executeRun; keep draining the rest.
			continue
		}
	}

	// Interrupted runs (a crash, or a worker that died holding the lease) resume
	// from their checkpoint. Resuming from the beginning would replay finished
	// steps; the checkpoint is what makes "no action without a source" true.
	if resumable, rerr := r.store.ResumableRuns(ctx, now, 20); rerr == nil {
		for _, id := range resumable {
			claimed, err := r.executeRun(ctx, id)
			if err != nil || !claimed {
				continue
			}
			out.RunsResumed++
		}
	}

	// Timed triggers run in the same pass, before the outbox is drained, so a
	// scheduled run's control command leaves in the same tick that created it.
	// A broken schedule is counted and skipped; it must not stop the pass.
	if sched, serr := r.FireDue(ctx, ScheduleBatch); serr != nil {
		out.ScheduleFailed++
	} else {
		out.ScheduleMaterialized = sched.Materialized
		out.ScheduleDuplicates = sched.Duplicates
		out.ScheduleMisfired = sched.Misfired
		out.ScheduleExecuted = sched.Executed
		out.ScheduleSkipped = sched.Skipped
		out.ScheduleFailed += sched.Failed
		out.ScheduleRecovered = sched.Recovered
	}

	// Silence is a fact worth recording, but it must not stop the pass.
	if noted, nerr := r.SweepNoData(ctx, now, 200); nerr == nil {
		out.NoDataNoted = noted
	}

	// Alarm transitions become rule inputs before the outbox is drained, so a
	// linkage that only notifies leaves in the same tick that raised the alarm.
	if relayed, rerr := r.RelayAlarmEvents(ctx, 100); rerr == nil {
		out.AlarmRelayed = relayed
	}

	n, err := r.DispatchOutbox(ctx, 50)
	out.OutboxDrained = n
	if err != nil {
		return out, err
	}
	return out, nil
}
func (r *Runtime) firePending(ctx context.Context, p *store.Pending, now time.Time) string {
	// Input-progress barrier (V31). A deadline must not be decided while a
	// sample received before the deadline is still unprocessed: otherwise a
	// 4:59 "normal" reading could be overtaken by the 5:00 timer.
	unprocessed, err := r.store.UnprocessedBefore(ctx, p.TenantID, p.DeviceID, p.Deadline)
	if err != nil {
		return "deferred"
	}
	if unprocessed > 0 {
		_ = r.store.DeferPending(ctx, p.ScopeKey, now.Add(2*time.Second))
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), p.TenantID, "pending.deferred", p.RuleID,
			map[string]any{
				"rule_id": p.RuleID, "device_id": p.DeviceID,
				"unprocessed_events": unprocessed,
				"reason":             "input_progress_behind_deadline",
			})
		return "deferred"
	}

	rule, err := r.store.GetRuleRevision(ctx, p.RuleID, p.Revision)
	if err != nil {
		_ = r.store.ClearPending(ctx, p.ScopeKey)
		return "dropped"
	}

	// Evidence freshness: a timer firing does not prove the physical condition
	// held. If the newest evidence is older than max_age, the result is unknown.
	fresh := checkFreshness(domain.Sample{ObservedAt: p.LastEvidenceAt}, now, rule.Definition.Freshness)
	if !fresh.OK {
		_ = r.store.ClearPending(ctx, p.ScopeKey)
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), p.TenantID, "pending.expired", p.RuleID,
			map[string]any{
				"rule_id": p.RuleID, "device_id": p.DeviceID,
				"reason": "stale_evidence", "last_evidence_at": p.LastEvidenceAt,
			})
		return "dropped"
	}

	if strings.HasSuffix(p.ScopeKey, ":recovery") {
		return r.fireRecovery(ctx, rule, p, now)
	}

	res := r.EvaluateSample(ctx, rule, p.DeviceID, nil, now)
	switch res.Verdict {
	case domain.VerdictTrue:
		_ = r.store.ClearPending(ctx, p.ScopeKey)
		if rule.Definition.Kind == domain.KindAlarm {
			seq, _ := r.store.NextSeq(ctx)
			// confirm, not submit: the declared duration has just elapsed and the
			// pending window is already cleared. Going back through the "wait for
			// `for`" gate would re-arm it and the alarm would never open.
			_, _ = r.confirmDetectorVerdict(ctx, rule, p.DeviceID, res, seq, now,
				cause{Root: p.CausalRoot, Hop: p.CausalHop})
		} else {
			_, _ = r.acceptRun(ctx, rule, p.DeviceID, "", cause{Root: p.CausalRoot, Hop: p.CausalHop},
				res, now, p.EvidenceSeq)
		}
		return "fired"
	case domain.VerdictFalse:
		_ = r.store.ClearPending(ctx, p.ScopeKey)
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), p.TenantID, "pending.cancelled", p.RuleID,
			map[string]any{
				"rule_id": p.RuleID, "device_id": p.DeviceID,
				"reason": "condition_no_longer_holds",
			})
		return "dropped"
	default:
		_ = r.store.ClearPending(ctx, p.ScopeKey)
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), p.TenantID, "pending.undetermined", p.RuleID,
			map[string]any{
				"rule_id": p.RuleID, "device_id": p.DeviceID,
				"verdict": string(res.Verdict), "reasons": res.Reasons,
			})
		return "dropped"
	}
}

func (r *Runtime) fireRecovery(ctx context.Context, rule *store.Rule, p *store.Pending, now time.Time) string {
	if rule.Definition.Recovery == nil {
		_ = r.store.ClearPending(ctx, p.ScopeKey)
		return "dropped"
	}
	res := domain.Evaluate(*rule.Definition.Recovery,
		r.evalContext(ctx, &rule.Definition, p.DeviceID, nil, now))
	_ = r.store.ClearPending(ctx, p.ScopeKey)
	if res.Verdict != domain.VerdictTrue {
		return "dropped"
	}
	seq, _ := r.store.NextSeq(ctx)
	// The recovery window has just been satisfied, so the decision must not
	// re-evaluate the recovery condition: doing so would restart the window it
	// just finished and the alarm would never clear.
	dec, derr := r.decide(ctx, decisionInput{
		rule: rule, subject: p.DeviceID, dimension: rule.AlarmDimension(),
		kind: TransitionClear, res: res, seq: seq, now: now,
		c:      cause{Root: p.CausalRoot, Hop: p.CausalHop},
		source: string(domain.AlarmSourceDetector), skipRecoveryCheck: true,
	})
	if derr != nil {
		return "dropped"
	}
	if dec.Outcome != OutcomeCleared {
		return "dropped"
	}
	return "fired"
}

// RecoverAfterRestart reprocesses unprocessed receipts. It is the durable
// handover guarantee: events that were received before the crash are evaluated
// after it, in receipt order, without duplicating side effects.
func (r *Runtime) RecoverAfterRestart(ctx context.Context, limit int) (int, error) {
	events, err := r.store.UnprocessedEvents(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, e := range events {
		rules, err := r.store.CandidateRules(ctx, e.TenantID, e.DeviceID, string(e.Signal), e.Key)
		if err != nil {
			continue
		}
		now := r.Now()
		for _, rule := range rules {
			if err := r.evaluateRule(ctx, rule, e, e.Seq, now); err != nil {
				r.recordEvaluationError(ctx, rule, e, err)
			}
		}
		if err := r.store.MarkProcessed(ctx, e.EventID); err != nil {
			return 0, err
		}
	}
	return len(events), nil
}
