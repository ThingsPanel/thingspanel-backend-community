package runtime

import (
	"context"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// ExplainVerdict is the answer to "why did this rule not fire?".
//
// Each value names a different thing an operator must do next, which is why they
// are separate values and not a boolean:
//
//	fired               a run exists; look at its steps
//	waiting_for_duration the condition is met, the "for" window has not elapsed
//	condition_not_met   the data was read and the condition was false
//	undetermined        the data could not be read, so neither true nor false
//	no_data             nothing has been received for the condition at all
//	not_fired           the condition is true and nothing explains the silence
type ExplainVerdict string

const (
	ExplainFired              ExplainVerdict = "fired"
	ExplainWaitingForDuration ExplainVerdict = "waiting_for_duration"
	ExplainConditionNotMet    ExplainVerdict = "condition_not_met"
	ExplainUndetermined       ExplainVerdict = "undetermined"
	ExplainNoData             ExplainVerdict = "no_data"
	ExplainNotFired           ExplainVerdict = "not_fired"
)

// ExplainResult is what the console shows for "why didn't it run".
type ExplainResult struct {
	RuleID   string
	DeviceID string
	From     time.Time
	To       time.Time
	Verdict  ExplainVerdict
	// Reasons are the evaluator's own reasons, so the explanation and the
	// decision cannot drift apart.
	Reasons []domain.Reason
	// Runs are the runs admitted in the window, newest first, with their steps.
	Runs []*ExplainRun
	// Pending is the open duration window, when there is one.
	Pending *ExplainPending
	// Input is the sample the verdict was judged on.
	Input *ExplainInput
	// Evidence says how much history the answer is based on. When it is
	// incomplete the caller must show that, not a confident reason.
	Evidence ExplainEvidence
}

type ExplainRun struct {
	Run    *store.Run
	Steps  []*store.Step
	Target []store.Target
	// CommandRefs are the device command ids the owner returned, so a customer
	// can jump from the record to the original command.
	CommandRefs []string
}

type ExplainPending struct {
	ScopeKey        string
	StartedAt       time.Time
	Deadline        time.Time
	LastEvidenceAt  time.Time
	RequireBefore   time.Time
	DeferCount      int
	RemainingMillis int64
}

type ExplainInput struct {
	DeviceID   string
	Signal     domain.SignalKind
	Key        string
	Value      any
	Quality    domain.Quality
	ObservedAt time.Time
	AgeMillis  int64
}

type ExplainEvidence struct {
	// RunsInWindow is how many run records exist in the window. Zero is a fact,
	// not a failure.
	RunsInWindow int
	// StateObserved is whether any sample exists for the condition key.
	StateObserved bool
	// Complete is false when the answer cannot be fully justified, for example
	// when the condition key has never been received.
	Complete bool
	Note     string
}

// Explain answers "why did this rule fire, or why did it not" for one device and
// one window.
//
// It deliberately reuses the same evaluator as the live path: an explanation
// computed by different code is a second opinion, not an explanation. It never
// invents a reason — when the evidence is missing it says so.
func (r *Runtime) Explain(ctx context.Context, rule *store.Rule, deviceID string,
	from, to time.Time) (*ExplainResult, error) {

	now := r.Now()
	if to.IsZero() {
		to = now
	}
	if from.IsZero() {
		from = to.Add(-time.Hour)
	}
	res := &ExplainResult{
		RuleID: rule.ID, DeviceID: deviceID, From: from, To: to,
	}

	// 1. Runs in the window. A run record is the strongest evidence there is.
	runs, err := r.store.ListRuns(ctx, rule.TenantID, rule.ID, deviceID, 100)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if run.StartedAt.Before(from) || run.StartedAt.After(to) {
			continue
		}
		detail, err := r.explainRun(ctx, run)
		if err != nil {
			return nil, err
		}
		res.Runs = append(res.Runs, detail)
	}
	res.Evidence.RunsInWindow = len(res.Runs)
	if len(res.Runs) > 0 {
		res.Verdict = ExplainFired
		res.Evidence.Complete = true
		res.Evidence.Note = "窗口内存在运行记录，结果以运行详情为准"
		return res, nil
	}

	// 2. An open duration window explains "the condition is met but nothing ran".
	scope := ScopeKey(&rule.Definition, rule.ID, deviceID)
	if pending, err := r.store.GetPending(ctx, scope); err == nil && pending != nil {
		res.Verdict = ExplainWaitingForDuration
		res.Pending = &ExplainPending{
			ScopeKey: pending.ScopeKey, StartedAt: pending.StartedAt, Deadline: pending.Deadline,
			LastEvidenceAt: pending.LastEvidenceAt, RequireBefore: pending.RequireProcessedBefore,
			DeferCount: pending.DeferCount,
		}
		if !pending.Deadline.IsZero() {
			res.Pending.RemainingMillis = int64(pending.Deadline.Sub(now) / time.Millisecond)
		}
		res.Reasons = []domain.Reason{{Code: "duration_pending", Message: "条件已满足，持续时长尚未走完"}}
		res.Evidence.Complete = true
		res.Evidence.Note = "存在未到期的持续条件窗口"
		return res, nil
	}

	// 3. Judge the current stored value with the same evaluator the trigger uses.
	verdict := domain.Evaluate(rule.Definition.Trigger, r.evalContext(ctx, &rule.Definition, deviceID, nil, now))
	res.Reasons = verdict.Reasons
	switch verdict.Verdict {
	case domain.VerdictTrue:
		res.Verdict = ExplainNotFired
		res.Evidence.Note = "条件当前为真，但窗口内没有运行记录；请检查该时刻的入站事件是否到达"
	case domain.VerdictFalse:
		res.Verdict = ExplainConditionNotMet
		res.Evidence.Complete = true
		res.Evidence.Note = "条件判定为假"
	default:
		res.Verdict = ExplainUndetermined
		res.Evidence.Note = "条件无法确定（数据未知或读取错误），未按假处理"
	}

	// 4. Show the sample the verdict was based on, when there is one.
	trigger := firstCondition(rule.Definition.Trigger)
	if trigger != nil {
		target := deviceID
		if trigger.Source == "device" && trigger.DeviceID != "" {
			target = trigger.DeviceID
		}
		if sample, ok, err := r.store.LatestState(ctx, target, trigger.Signal, trigger.Key); err == nil && ok {
			res.Evidence.StateObserved = true
			res.Input = &ExplainInput{
				DeviceID: target, Signal: sample.Signal, Key: sample.Key,
				Value: sample.Value, Quality: sample.Quality, ObservedAt: sample.ObservedAt,
				AgeMillis: int64(now.Sub(sample.ObservedAt) / time.Millisecond),
			}
		} else {
			res.Verdict = ExplainNoData
			res.Evidence.Complete = false
			res.Evidence.Note = "该指标从未收到数据，无法判定条件；这不是“条件不满足”"
			res.Reasons = append(res.Reasons, domain.Reason{
				Code: "no_history", Key: trigger.Key,
				Message: "指标 " + string(trigger.Signal) + "/" + trigger.Key + " 没有历史样本",
			})
		}
	}
	return res, nil
}

// explainRun collects the parts of a run a customer needs: what each step did,
// which targets were selected, and which device command ids came back.
func (r *Runtime) explainRun(ctx context.Context, run *store.Run) (*ExplainRun, error) {
	steps, err := r.store.ListSteps(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	targets, err := r.store.ListTargets(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	items, err := r.store.OutboxForRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	out := &ExplainRun{Run: run, Steps: steps, Target: targets}
	for _, item := range items {
		if item.AcceptedRef != "" {
			out.CommandRefs = append(out.CommandRefs, item.AcceptedRef)
		}
	}
	return out, nil
}

// firstCondition returns the first condition of the first group, which is the
// one whose sample best explains the verdict for the common single-condition
// rule.
func firstCondition(t domain.Trigger) *domain.Condition {
	for _, g := range t.Groups {
		for i := range g.Conditions {
			return &g.Conditions[i]
		}
	}
	return nil
}
