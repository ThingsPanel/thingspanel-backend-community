package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// Alarm transition kinds.
const (
	TransitionQualified    = "qualified"
	TransitionClear        = "clear"
	TransitionAck          = "ack"
	TransitionNoTransition = "no_transition"
	TransitionUnknown      = "unknown"
	TransitionError        = "error"
)

// applyAlarmVerdict turns an evaluator verdict into a lifecycle decision.
//
// The alarm owner keeps one active episode per (policy, subject). A repeated
// abnormal sample updates that episode; it does not create a new alarm. An
// unknown sample never clears an active alarm.
func (r *Runtime) applyAlarmVerdict(ctx context.Context, rule *store.Rule, deviceID, scope string,
	res domain.Result, seq int64, now time.Time, c cause) error {

	def := rule.Definition
	active, err := r.store.GetActiveEpisode(ctx, rule.TenantID, rule.ID, deviceID, "")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	switch res.Verdict {
	case domain.VerdictTrue:
		if active != nil {
			_ = r.store.TouchEpisode(ctx, active.ID, seq)
			return nil
		}
		if def.Trigger.For.Std() > 0 {
			return r.startPending(ctx, rule, deviceID, scope, domain.Sample{
				DeviceID: deviceID, ObservedAt: now, Seq: seq,
			}, c, seq, now)
		}
		return r.qualify(ctx, rule, deviceID, res, seq, now, c)

	case domain.VerdictFalse:
		// The abnormal trigger no longer holds. That alone is not a recovery:
		// recovery is a separate, explicit condition (hysteresis).
		if active == nil {
			_ = r.store.ClearPending(ctx, scope)
			return nil
		}
		if def.Recovery == nil {
			return r.transition(ctx, active, TransitionNoTransition, seq, res,
				"触发条件不再成立，但没有配置恢复条件，保持活动告警")
		}
		rec := domain.Evaluate(*def.Recovery,
			r.evalContext(ctx, &def, deviceID, nil, now))
		switch rec.Verdict {
		case domain.VerdictTrue:
			if def.Recovery.For.Std() > 0 {
				return r.startRecoveryPending(ctx, rule, deviceID, scope, now, seq, c)
			}
			return r.clear(ctx, rule, active, seq, now, rec, c)
		case domain.VerdictFalse:
			return r.transition(ctx, active, TransitionNoTransition, seq, rec,
				"处于迟滞中性区：既未确认异常持续，也未满足恢复条件")
		default:
			return r.transition(ctx, active, TransitionUnknown, seq, rec,
				"恢复条件无法确定（数据未知或错误），保持活动告警")
		}

	default:
		if active != nil {
			kind := TransitionUnknown
			if res.Verdict == domain.VerdictError {
				kind = TransitionError
			}
			return r.transition(ctx, active, kind, seq, res,
				"条件无法判定，保持活动告警，不自动恢复")
		}
		return nil
	}
}

func (r *Runtime) qualify(ctx context.Context, rule *store.Rule, deviceID string, res domain.Result,
	seq int64, now time.Time, c cause) error {

	ep := &store.Episode{
		ID: NewID(), TenantID: rule.TenantID, PolicyID: rule.ID, SubjectID: deviceID,
		Dimension: "", State: "active", Revision: rule.Revision, Epoch: int64(rule.Revision),
		OpenedAt: now, LastSeq: seq,
	}
	if err := r.store.OpenEpisode(ctx, ep); err != nil {
		// A concurrent detector may have opened it first; the partial unique
		// index guarantees there is still exactly one active episode.
		if existing, gerr := r.store.GetActiveEpisode(ctx, rule.TenantID, rule.ID, deviceID, ""); gerr == nil {
			_ = r.store.TouchEpisode(ctx, existing.ID, seq)
			return nil
		}
		return err
	}
	transitionID := NewID()
	if _, err := r.store.AddTransition(ctx, nil, &store.Transition{
		ID: transitionID, EpisodeID: ep.ID, TenantID: rule.TenantID, Kind: TransitionQualified,
		Seq: seq, Revision: rule.Revision, Epoch: ep.Epoch, Evidence: res,
	}); err != nil {
		return err
	}
	_ = r.store.ClearPending(ctx, ScopeKey(&rule.Definition, rule.ID, deviceID))
	_, _ = r.store.PublishEvent(ctx, nil, NewID(), rule.TenantID, "alarm.qualified", ep.ID,
		alarmPayload(ep, rule, deviceID, seq, TransitionQualified, "active", transitionID, c))
	return nil
}

// alarmPayload is the contract a linked rule consumes. It carries the stable
// transition id (the dedupe key), the episode identity and the causal chain, so
// an alarm-driven run can be traced back and stopped at the hop limit.
func alarmPayload(ep *store.Episode, rule *store.Rule, deviceID string, seq int64,
	kind, state, transitionID string, c cause) map[string]any {

	root := c.Root
	if root == "" {
		root = transitionID
	}
	return map[string]any{
		"transition_id": transitionID, "kind": kind, "state": state,
		"episode_id": ep.ID, "policy_id": rule.ID, "device_id": deviceID,
		"seq": seq, "revision": rule.Revision, "event_id": c.EventID,
		"causal_root": root, "causal_hop": c.Hop,
	}
}

func (r *Runtime) clear(ctx context.Context, rule *store.Rule, ep *store.Episode, seq int64,
	now time.Time, res domain.Result, c cause) error {

	changed, err := r.store.ClearEpisode(ctx, ep.ID, seq)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	transitionID := NewID()
	if _, err := r.store.AddTransition(ctx, nil, &store.Transition{
		ID: transitionID, EpisodeID: ep.ID, TenantID: rule.TenantID, Kind: TransitionClear,
		Seq: seq, Revision: rule.Revision, Epoch: ep.Epoch, Evidence: res,
	}); err != nil {
		return err
	}
	_ = r.store.ClearPending(ctx, ScopeKey(&rule.Definition, rule.ID, ep.SubjectID))
	_, _ = r.store.PublishEvent(ctx, nil, NewID(), rule.TenantID, "alarm.cleared", ep.ID,
		alarmPayload(ep, rule, ep.SubjectID, seq, TransitionClear, "cleared", transitionID, c))
	return nil
}

func (r *Runtime) transition(ctx context.Context, ep *store.Episode, kind string, seq int64,
	res domain.Result, message string) error {

	_, err := r.store.AddTransition(ctx, nil, &store.Transition{
		ID: NewID(), EpisodeID: ep.ID, TenantID: ep.TenantID, Kind: kind,
		Seq: seq, Revision: ep.Revision, Epoch: ep.Epoch,
		Evidence: map[string]any{"verdict": res.Verdict, "reasons": res.Reasons, "message": message},
	})
	return err
}

func (r *Runtime) startRecoveryPending(ctx context.Context, rule *store.Rule, deviceID, scope string,
	now time.Time, seq int64, c cause) error {

	recScope := scope + ":recovery"
	p := &store.Pending{
		ScopeKey: recScope, TenantID: rule.TenantID, RuleID: rule.ID, Revision: rule.Revision,
		DeviceID: deviceID, StartedAt: now,
		Deadline:       now.Add(rule.Definition.Recovery.For.Std()),
		LastEvidenceAt: now, EvidenceSeq: seq, RequireProcessedBefore: now,
		CausalRoot: c.Root, CausalHop: c.Hop,
	}
	return r.store.UpsertPending(ctx, p)
}

// Ack acknowledges an episode. Acknowledgement changes "someone knows", not
// "the device is healthy".
func (r *Runtime) Ack(ctx context.Context, tenantID, episodeID, actor string) (bool, error) {
	seq, err := r.store.NextSeq(ctx)
	if err != nil {
		return false, err
	}
	ok, err := r.store.AckEpisode(ctx, tenantID, episodeID, actor, seq)
	if err != nil || !ok {
		return ok, err
	}
	ep, err := r.store.GetActiveEpisodeByID(ctx, tenantID, episodeID)
	if err == nil {
		_, _ = r.store.AddTransition(ctx, nil, &store.Transition{
			ID: NewID(), EpisodeID: ep.ID, TenantID: tenantID, Kind: TransitionAck,
			Seq: seq, Revision: ep.Revision, Epoch: ep.Epoch,
			Evidence: map[string]any{"actor": actor},
		})
	}
	return true, nil
}

// performAlarm executes an alarm step: the run raises a qualified transition on
// the referenced policy for the run's subject device.
func (r *Runtime) performAlarm(ctx context.Context, run *store.Run, stepID string, step domain.Step, pc int) (stepOutcome, error) {
	policy, err := r.store.GetRule(ctx, run.TenantID, step.AlarmPolicyID)
	if err != nil {
		_ = r.store.UpdateStepStatus(ctx, stepID, "failed", `{"reason":"alarm_policy_not_found"}`)
		return stepFailed, nil
	}
	if policy.Kind != domain.KindAlarm {
		_ = r.store.UpdateStepStatus(ctx, stepID, "failed", `{"reason":"not_an_alarm_policy"}`)
		return stepFailed, nil
	}
	subject := run.DeviceID
	if step.DeviceRef != "" && step.DeviceRef != "self" && step.DeviceRef != "target" {
		subject = step.DeviceRef
	}
	seq, err := r.store.NextSeq(ctx)
	if err != nil {
		return stepFailed, err
	}
	res := domain.Result{Verdict: domain.VerdictTrue, Reasons: []domain.Reason{{
		Code:    "alarm_action_from_automation",
		Message: "自动化按旧场景告警动作提交的授权事件型判定",
	}}}
	// The linked alarm inherits the run's chain so an alarm -> rule -> alarm
	// loop is stopped at the hop limit instead of running forever.
	if err := r.qualify(ctx, policy, subject, res, seq, r.Now(), cause{
		Root: run.CausalRoot, Hop: run.CausalHop, EventID: run.TriggerEventID,
	}); err != nil {
		return stepFailed, err
	}
	detail, _ := json.Marshal(map[string]any{"policy_id": policy.ID, "subject": subject, "seq": seq})
	if err := r.store.UpdateStepStatus(ctx, stepID, "succeeded", string(detail)); err != nil {
		return stepFailed, err
	}
	return stepOK, nil
}

// ResolvePolicyRevision exposes the published revision of an alarm policy so an
// external executor can pin the same immutable parameters (A28).
func (r *Runtime) PolicyRevision(ctx context.Context, tenantID, policyID string) (*store.Rule, error) {
	rule, err := r.store.GetRule(ctx, tenantID, policyID)
	if err != nil {
		return nil, err
	}
	if rule.Kind != domain.KindAlarm {
		return nil, errors.New("rule is not an alarm policy")
	}
	return rule, nil
}
