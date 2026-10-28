package runtime

import (
	"context"
	"encoding/json"
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
	now := r.Now()
	if e.ObservedAt.IsZero() {
		e.ObservedAt = now
	}
	rec, err := r.store.InsertEvent(ctx, &e)
	if err != nil {
		return nil, err
	}
	if rec.Duplicate {
		return rec, nil
	}

	// A repeated online report carries no new information. Devices re-announce
	// online on every reconnect, so without this check each reconnect would
	// start another run and look like a fresh event. The receipt is still
	// persisted — it is a real message — but the evaluation is skipped and the
	// decision is recorded.
	if e.Signal == domain.SignalStatus {
		prev, ok, err := r.store.LatestState(ctx, e.DeviceID, e.Signal, e.Key)
		if err == nil && ok && sameValue(prev.Value, e.Value) &&
			now.Sub(prev.ObservedAt) < StatusCoalesce {
			if err := r.store.ApplyState(ctx, &e); err != nil {
				return nil, err
			}
			_, _ = r.store.PublishEvent(ctx, nil, NewID(), e.TenantID, "ingress.duplicate_state", e.DeviceID,
				map[string]any{
					"device_id": e.DeviceID, "event_id": e.EventID, "key": e.Key,
					"value": e.Value, "previous_observed_at": prev.ObservedAt,
					"reason": "state_unchanged_within_coalesce_window",
					"window": StatusCoalesce.String(),
				})
			return rec, r.store.MarkProcessed(ctx, e.EventID)
		}
	}

	if err := r.store.ApplyState(ctx, &e); err != nil {
		return nil, err
	}

	// Stop an alarm-driven chain that already used up its hops, and record why.
	// This is the loop breaker: without it, alarm A triggering rule R whose
	// action raises alarm A again would run forever.
	if e.CausalHop > MaxCausalHop {
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), e.TenantID, "causal.hop_exceeded", e.DeviceID,
			map[string]any{
				"device_id": e.DeviceID, "event_id": e.EventID,
				"causal_root": e.CausalRoot, "causal_hop": e.CausalHop, "limit": MaxCausalHop,
				"reason": "alarm-driven chain reached the hop limit; further links are stopped",
			})
		return rec, r.store.MarkProcessed(ctx, e.EventID)
	}

	if err := r.evaluateForDevice(ctx, &e, rec.Seq, now); err != nil {
		return rec, err
	}
	if err := r.store.MarkProcessed(ctx, e.EventID); err != nil {
		return rec, err
	}
	return rec, nil
}

// evaluateForDevice resolves the candidate rules for one event, fills in the
// device's parameter values and evaluates each rule.
//
// Both the live ingest path and the restart recovery path go through it. They
// used to be separate loops, which is how a crash-recovered event could be
// evaluated against the template body — with the placeholders still in it —
// while the same event evaluated correctly before the crash.
func (r *Runtime) evaluateForDevice(ctx context.Context, e *store.IngestEvent, seq int64, now time.Time) error {
	// Which template this device belongs to is only needed when the tenant
	// actually uses template bindings; asking the directory on every uplink
	// would be a cost with no benefit for the common case.
	configID := ""
	if r.usesTemplateBindings(ctx, e.TenantID) {
		configID = r.deviceConfigOf(ctx, e.TenantID, e.DeviceID)
	}
	rules, err := r.store.CandidateRules(ctx, e.TenantID, e.DeviceID, configID, string(e.Signal), e.Key)
	if err != nil {
		return err
	}
	// One lookup per event, not one per rule: a device bound to twelve rules
	// still costs a single query.
	bound := r.bindingsFor(ctx, e.TenantID, e.DeviceID)
	for _, rule := range rules {
		// The rule body is shared; the values are this device's. Materializing
		// here — and only here — is what makes "edit the rule once, 500 devices
		// follow, one device overrides" true without 500 copies of the body.
		effective, berr := bindRule(rule, bound[rule.ID])
		if berr != nil {
			r.recordEvaluationError(ctx, rule, e, berr)
			continue
		}
		if err := r.evaluateRule(ctx, effective, e, seq, now); err != nil {
			// One broken rule must not block the others or lose the receipt.
			r.recordEvaluationError(ctx, rule, e, err)
		}
	}
	return nil
}

// sameValue compares two stored values by their canonical JSON form, so a bool
// read back from jsonb equals the bool that was just decoded.
func sameValue(a, b any) bool {
	ja, ea := json.Marshal(a)
	jb, eb := json.Marshal(b)
	if ea != nil || eb != nil {
		return false
	}
	return string(ja) == string(jb)
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
	return domain.Evaluate(rule.Definition.Trigger,
		r.evalContext(ctx, &rule.Definition, deviceID, trigger, now))
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
		return r.applyAlarmVerdict(ctx, rule, e.DeviceID, scope, res, seq, now, causeOf(e))
	}

	switch res.Verdict {
	case domain.VerdictTrue:
		if def.Trigger.For.Std() <= 0 {
			// for=0 keeps the legacy behaviour: every satisfying uplink may act.
			_, err := r.acceptRun(ctx, rule, e.DeviceID, e.EventID, causeOf(e), res, now, seq)
			return err
		}
		return r.startPending(ctx, rule, e.DeviceID, scope, sample, causeOf(e), seq, now)

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
func (r *Runtime) startPending(ctx context.Context, rule *store.Rule, deviceID, scope string,
	sample domain.Sample, c cause, seq int64, now time.Time) error {
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
		CausalRoot:             c.Root,
		CausalHop:              c.Hop,
		// Freeze the values the window was opened with, so an edit to the
		// device override halfway through cannot move the deadline the customer
		// is already waiting on.
		BindingParams: rule.BindingParams,
		BindingSource: rule.BindingSource,
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
