package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// Ingest persists one normalized event and evaluates the candidate rules.
//
// The order matters: the event is durable before anything is evaluated, so a
// crash between the two steps leaves a reprocessable row instead of a silently
// lost sample. A duplicate event_id returns early and produces no second action.
func (r *Runtime) Ingest(ctx context.Context, e store.IngestEvent) (*store.Receipt, error) {
	if strings.TrimSpace(e.EventID) == "" {
		return nil, errors.New("event_id is required")
	}
	if e.ObservedAt.IsZero() {
		e.ObservedAt = r.Now()
	}
	rec, err := r.store.InsertEvent(ctx, &e)
	if err != nil {
		return nil, err
	}
	if rec.Duplicate {
		return rec, nil
	}
	if err := r.store.ApplyState(ctx, &e); err != nil {
		return nil, err
	}

	rules, err := r.store.CandidateRules(ctx, e.TenantID, e.DeviceID, string(e.Signal), e.Key)
	if err != nil {
		return rec, err
	}
	now := r.Now()
	for _, rule := range rules {
		if err := r.evaluateRule(ctx, rule, &e, rec.Seq, now); err != nil {
			// One broken rule must not block the others or lose the receipt.
			r.recordEvaluationError(ctx, rule, &e, err)
		}
	}
	if err := r.store.MarkProcessed(ctx, e.EventID); err != nil {
		return rec, err
	}
	return rec, nil
}

func (r *Runtime) recordEvaluationError(ctx context.Context, rule *store.Rule, e *store.IngestEvent, err error) {
	_, _ = r.store.PublishEvent(ctx, nil, NewID(), rule.TenantID, "rule.evaluation_error", rule.ID,
		map[string]any{
			"rule_id":   rule.ID,
			"device_id": e.DeviceID,
			"event_id":  e.EventID,
			"error":     err.Error(),
		})
}

// EvaluateSample runs the evaluator for a rule without touching pending state.
// Simulation and explain both use it, so they can never disagree with the real
// path about what a rule means.
func (r *Runtime) EvaluateSample(ctx context.Context, rule *store.Rule, deviceID string, trigger *domain.Sample, now time.Time) domain.Result {
	return domain.Evaluate(rule.Definition.Trigger, domain.EvalContext{
		Now:          now,
		SelfDeviceID: deviceID,
		Trigger:      trigger,
		Lookup:       r.lookup(ctx),
	})
}

func (r *Runtime) evaluateRule(ctx context.Context, rule *store.Rule, e *store.IngestEvent, seq int64, now time.Time) error {
	def := rule.Definition
	sample := domain.Sample{
		DeviceID:   e.DeviceID,
		Signal:     e.Signal,
		Key:        e.Key,
		Value:      e.Value,
		Unit:       e.Unit,
		ObservedAt: e.ObservedAt,
		ReceivedAt: now,
		Seq:        seq,
		Quality:    e.Quality,
		EventID:    e.EventID,
		Params:     e.Params,
	}
	res := r.EvaluateSample(ctx, rule, e.DeviceID, &sample, now)
	scope := ScopeKey(&def, rule.ID, e.DeviceID)

	if def.Kind == domain.KindAlarm {
		return r.applyAlarmVerdict(ctx, rule, e.DeviceID, scope, res, seq, now, e.EventID)
	}

	switch res.Verdict {
	case domain.VerdictTrue:
		if def.Trigger.For.Std() <= 0 {
			// for=0 keeps the legacy behaviour: every satisfying uplink may act.
			_, err := r.acceptRun(ctx, rule, e.DeviceID, e.EventID, res, now, seq)
			return err
		}
		return r.startPending(ctx, rule, e.DeviceID, scope, sample, seq, now)

	case domain.VerdictFalse:
		if p, err := r.store.GetPending(ctx, scope); err == nil && p != nil {
			_ = r.store.ClearPending(ctx, scope)
			_, _ = r.store.PublishEvent(ctx, nil, NewID(), rule.TenantID, "pending.cancelled", rule.ID,
				map[string]any{"rule_id": rule.ID, "device_id": e.DeviceID, "reason": "condition_false"})
		}
		return nil

	default: // unknown / error
		return r.holdPending(ctx, rule, e.DeviceID, scope, res, now, e.EventID)
	}
}

// startPending opens or refreshes the "condition has held since" record.
//
// The start time is the *evidence* time of the first satisfying sample, not the
// wall clock of the evaluation. That is what lets a restart in the middle of a
// 5 minute window resume the original deadline instead of restarting the count.
func (r *Runtime) startPending(ctx context.Context, rule *store.Rule, deviceID, scope string, sample domain.Sample, seq int64, now time.Time) error {
	def := rule.Definition
	existing, err := r.store.GetPending(ctx, scope)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	p := &store.Pending{
		ScopeKey:               scope,
		TenantID:               rule.TenantID,
		RuleID:                 rule.ID,
		Revision:               rule.Revision,
		DeviceID:               deviceID,
		LastEvidenceAt:         sample.ObservedAt,
		EvidenceSeq:            seq,
		RequireProcessedBefore: now,
	}
	if existing != nil {
		// Keep the original start and deadline: later samples only refresh the
		// evidence watermark.
		p.StartedAt = existing.StartedAt
		p.Deadline = existing.Deadline
	} else {
		p.StartedAt = sample.ObservedAt
		p.Deadline = p.StartedAt.Add(def.Trigger.For.Std())
	}
	return r.store.UpsertPending(ctx, p)
}

// holdPending keeps a pending window alive when the condition became unknown.
// It never clears an active alarm and never invents satisfaction.
func (r *Runtime) holdPending(ctx context.Context, rule *store.Rule, deviceID, scope string, res domain.Result, now time.Time, eventID string) error {
	p, err := r.store.GetPending(ctx, scope)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // nothing to hold
		}
		return err
	}
	maxGap := rule.Definition.Freshness.MaxGap.Std()
	if maxGap > 0 && now.Sub(p.LastEvidenceAt) > maxGap {
		if err := r.store.ClearPending(ctx, scope); err != nil {
			return err
		}
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), rule.TenantID, "pending.expired", rule.ID,
			map[string]any{
				"rule_id": rule.ID, "device_id": deviceID, "event_id": eventID,
				"verdict": string(res.Verdict), "last_evidence_at": p.LastEvidenceAt,
				"reason": "evidence_gap_exceeded_max_gap",
			})
		return nil
	}
	_, _ = r.store.PublishEvent(ctx, nil, NewID(), rule.TenantID, "pending.undetermined", rule.ID,
		map[string]any{
			"rule_id": rule.ID, "device_id": deviceID, "event_id": eventID,
			"verdict": string(res.Verdict), "reasons": res.Reasons,
		})
	return nil
}
