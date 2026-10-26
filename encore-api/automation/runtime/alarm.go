package runtime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// Alarm transition kinds. These are the *facts* written to history; they are
// deliberately not the same list as the decision outcomes below, because
// "nothing changed" is a decision but not a lifecycle fact worth a row.
const (
	TransitionQualified    = "qualified"
	TransitionClear        = "clear"
	TransitionAck          = "ack"
	TransitionNoTransition = "no_transition"
	TransitionUnknown      = "unknown"
	TransitionError        = "error"
	// TransitionImported marks an episode that was adopted from the old system
	// rather than raised by this one. It is a distinct fact because the customer
	// was notified by the old engine: re-raising would send a second message for
	// a problem they already know about.
	TransitionImported = "imported"
	// TransitionRollback marks an episode a migration took back. It is not a
	// "clear": the condition did not recover, an operator withdrew the import.
	// Writing "clear" here would put a recovery that never happened into the
	// history the customer reads.
	TransitionRollback = "rollback"
)

// Decision outcomes. The five the task book asks to keep apart are here as
// separate values; adding "rejected" keeps "a stale worker tried to clear this
// alarm" from being reported as "the alarm is still active", which are different
// facts and lead to different operator actions.
const (
	OutcomeQualified    = "qualified"
	OutcomeCleared      = "clear"
	OutcomeNoTransition = "no_transition"
	OutcomeUnknown      = "unknown"
	OutcomeError        = "error"
	OutcomeRejected     = "rejected"
)

// Reason codes for a refused or empty decision.
const (
	ReasonOutOfOrder        = "out_of_order"
	ReasonStaleRevision     = "stale_revision"
	ReasonStillTrue         = "condition_still_true"
	ReasonRevisionAdvanced  = "revision_advanced_while_active"
	ReasonNoEpisode         = "no_active_episode"
	ReasonNoRecovery        = "no_recovery_configured"
	ReasonHysteresisNeutral = "hysteresis_neutral"
	ReasonNotDetector       = "policy_not_detector_sourced"
	ReasonNotEvent          = "policy_not_event_sourced"
	ReasonSourceDenied      = "source_not_allowed"
	ReasonConflict          = "action_id_content_mismatch"
	ReasonPolicyMissing     = "policy_not_found"
	ReasonPolicyDisabled    = "policy_disabled"
	ReasonBadKind           = "unsupported_event_kind"
	ReasonWaitingForTrigger = "waiting_for_duration"
	ReasonWaitingRecovery   = "waiting_for_recovery_duration"
)

// Decision is the answer the alarm owner gives one intake call.
type Decision struct {
	Outcome      string `json:"outcome"`
	ReasonCode   string `json:"reason_code,omitempty"`
	EpisodeID    string `json:"episode_id,omitempty"`
	TransitionID string `json:"transition_id,omitempty"`
	// Replayed means the answer was taken from an earlier identical submission
	// instead of being computed again.
	Replayed bool `json:"replayed,omitempty"`
}

// decisionInput is the shared body both intake channels feed.
type decisionInput struct {
	rule      *store.Rule
	subject   string
	dimension string
	// kind is what the caller asserts: qualified, clear, unknown or error.
	kind     string
	res      domain.Result
	seq      int64
	now      time.Time
	c        cause
	source   string
	evidence any
	// skipRecoveryCheck is set when the caller has already proven that the
	// recovery condition held for its full duration (the recovery timer firing).
	// Re-evaluating it there would restart the window it just finished.
	skipRecoveryCheck bool
}

// applyAlarmVerdict is the detector channel: the evaluator has already produced a
// four-valued result and the alarm owner decides what it means for the lifecycle.
func (r *Runtime) applyAlarmVerdict(ctx context.Context, rule *store.Rule, deviceID, scope string,
	res domain.Result, seq int64, now time.Time, c cause) error {

	_, err := r.submitDetectorVerdict(ctx, rule, deviceID, res, seq, now, c)
	return err
}

// submitDetectorVerdict is the only way a condition-type policy can be qualified.
//
// It is not reachable over HTTP. The separation is the whole point: an HTTP
// request must never be able to claim "the evaluator said the condition holds",
// because the evaluator is the only thing that can know. A policy that does not
// declare the detector channel is refused here rather than being quietly judged.
func (r *Runtime) submitDetectorVerdict(ctx context.Context, rule *store.Rule, subjectID string,
	res domain.Result, seq int64, now time.Time, c cause) (Decision, error) {

	if !rule.Definition.Alarm.Allows(domain.AlarmSourceDetector) {
		return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonNotDetector}, nil
	}
	// A configured "for" means the condition must hold for a while before the
	// alarm opens. The alarm owner does not grow a second waiting mechanism for
	// that: it opens the same pending window the automation path uses, and the
	// timer comes back with a qualified verdict. Two independent deadlines for
	// one condition is how "it fired 3 seconds early" happens.
	if res.Verdict == domain.VerdictTrue && rule.Definition.Trigger.For.Std() > 0 {
		active, err := r.store.GetActiveEpisode(ctx, rule.TenantID, rule.PolicyKey(),
			subjectID, rule.AlarmDimension())
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return Decision{}, err
		}
		if active == nil {
			scope := ScopeKey(&rule.Definition, rule.ID, subjectID)
			if err := r.startPending(ctx, rule, subjectID, scope, domain.Sample{
				DeviceID: subjectID, ObservedAt: now, Seq: seq,
			}, c, seq, now); err != nil {
				return Decision{}, err
			}
			return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonWaitingForTrigger}, nil
		}
	}
	return r.decideDetector(ctx, rule, subjectID, res, seq, now, c)
}

// confirmDetectorVerdict is the same submission made *after* the declared
// duration has already elapsed.
//
// The timer path must not go through the "wait for `for`" gate again: it has just
// finished that wait and cleared the pending window, so re-entering the gate
// would start a fresh pending row and the alarm would never open — one
// re-arm per tick, forever, with every log line looking healthy. Keeping the two
// entries separate is what makes that mistake impossible to reintroduce: the
// gate lives in exactly one function, and the timer does not call it.
func (r *Runtime) confirmDetectorVerdict(ctx context.Context, rule *store.Rule, subjectID string,
	res domain.Result, seq int64, now time.Time, c cause) (Decision, error) {

	if !rule.Definition.Alarm.Allows(domain.AlarmSourceDetector) {
		return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonNotDetector}, nil
	}
	return r.decideDetector(ctx, rule, subjectID, res, seq, now, c)
}

// decideDetector maps a detector verdict onto a lifecycle transition.
func (r *Runtime) decideDetector(ctx context.Context, rule *store.Rule, subjectID string,
	res domain.Result, seq int64, now time.Time, c cause) (Decision, error) {

	// A false verdict is a *clear candidate*, not a decision: whether it clears
	// depends on the recovery condition and on the hysteresis band. Routing it
	// through the clear path is what makes "the abnormal condition stopped" and
	// "the device is healthy again" two different questions.
	kind := TransitionClear
	switch res.Verdict {
	case domain.VerdictTrue:
		kind = OutcomeQualified
	case domain.VerdictUnknown:
		kind = OutcomeUnknown
	case domain.VerdictError:
		kind = OutcomeError
	}
	return r.decide(ctx, decisionInput{
		rule: rule, subject: subjectID, dimension: rule.AlarmDimension(),
		kind: kind, res: res, seq: seq, now: now, c: c,
		source: string(domain.AlarmSourceDetector),
	})
}

// AlarmEventRequest is one authorised event-channel submission.
type AlarmEventRequest struct {
	// ActionID is the caller's stable logical action id. The same id must mean
	// the same thing forever: a redelivery replays the first answer, and the same
	// id with a different body is a conflict rather than a second alarm.
	ActionID  string
	Source    string
	Kind      string // qualified | clear
	SubjectID string
	Dimension string
	Payload   map[string]any
	// Cause carries the chain the caller belongs to. Without it, an alarm raised
	// by a run would start a fresh chain every time and a deliberate
	// alarm-to-linkage loop would never reach the hop limit.
	Cause cause
}

// SubmitAlarmEvent is the event channel, used to preserve the legacy scene
// "raise this alarm" action without letting a plain business request impersonate
// the detector.
func (r *Runtime) SubmitAlarmEvent(ctx context.Context, tenantID, policyID string, req AlarmEventRequest) (Decision, error) {
	if strings.TrimSpace(req.ActionID) == "" {
		return Decision{Outcome: OutcomeRejected, ReasonCode: "missing_action_id"}, nil
	}
	switch req.Kind {
	case TransitionQualified, TransitionClear:
	default:
		return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonBadKind}, nil
	}

	rule, err := r.store.GetRule(ctx, tenantID, policyID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonPolicyMissing}, nil
		}
		return Decision{}, err
	}
	if !rule.Enabled {
		return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonPolicyDisabled}, nil
	}
	spec := rule.Definition.Alarm
	if !spec.Allows(domain.AlarmSourceEvent) {
		return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonNotEvent}, nil
	}
	if !spec.SourceAllowed(req.Source) {
		return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonSourceDenied}, nil
	}

	subject := req.SubjectID
	dimension := req.Dimension
	if dimension == "" {
		dimension = rule.AlarmDimension()
	}
	if subject == "" {
		return Decision{Outcome: OutcomeRejected, ReasonCode: "missing_subject"}, nil
	}

	hash := intakeHash(rule.PolicyKey(), req)
	existing, created, err := r.store.ReserveAlarmIntake(ctx, &store.AlarmIntake{
		TenantID: tenantID, PolicyKey: rule.PolicyKey(), ActionID: req.ActionID,
		Source: req.Source, SubjectID: subject, Dimension: dimension,
		Kind: req.Kind, PayloadHash: hash,
	})
	if err != nil {
		return Decision{}, err
	}
	if !created {
		if existing.PayloadHash != hash || existing.Kind != req.Kind ||
			existing.SubjectID != subject || existing.Dimension != dimension {
			return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonConflict}, nil
		}
		if existing.Outcome == "in_flight" {
			// The first attempt died between reserving and deciding. Deciding
			// again is safe because nothing was written yet.
			return r.decideEvent(ctx, rule, subject, dimension, req, hash)
		}
		return Decision{
			Outcome: existing.Outcome, ReasonCode: existing.ReasonCode,
			EpisodeID: existing.EpisodeID, TransitionID: existing.TransitionID,
			Replayed: true,
		}, nil
	}

	dec, derr := r.decideEvent(ctx, rule, subject, dimension, req, hash)
	if derr != nil {
		_ = r.store.ReleaseAlarmIntake(ctx, tenantID, rule.PolicyKey(), req.ActionID)
		return Decision{}, derr
	}
	return dec, nil
}

func (r *Runtime) decideEvent(ctx context.Context, rule *store.Rule, subject, dimension string,
	req AlarmEventRequest, hash string) (Decision, error) {

	seq, err := r.store.NextSeq(ctx)
	if err != nil {
		return Decision{}, err
	}
	res := domain.Result{
		Verdict: domain.VerdictTrue,
		Reasons: []domain.Reason{{
			Code:    "alarm_action_from_event_source",
			Message: "授权事件来源提交的告警动作：" + req.Source,
		}},
	}
	if req.Kind == TransitionClear {
		res.Verdict = domain.VerdictFalse
	}
	dec, err := r.decide(ctx, decisionInput{
		rule: rule, subject: subject, dimension: dimension, kind: req.Kind,
		res: res, seq: seq, now: r.Now(), c: req.Cause,
		source: "event:" + req.Source, evidence: req.Payload,
	})
	if err != nil {
		return Decision{}, err
	}
	if err := r.store.FinishAlarmIntake(ctx, nil, rule.TenantID, rule.PolicyKey(), req.ActionID,
		dec.Outcome, dec.ReasonCode, dec.EpisodeID, dec.TransitionID); err != nil {
		return Decision{}, err
	}
	return dec, nil
}

func intakeHash(policyKey string, req AlarmEventRequest) string {
	body, _ := json.Marshal(map[string]any{
		"policy_key": policyKey, "source": req.Source, "kind": req.Kind,
		"subject": req.SubjectID, "dimension": req.Dimension, "payload": req.Payload,
	})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// decide is the single lifecycle decision.
//
// Everything that changes an episode goes through here, so "why is this alarm
// still active" has exactly one implementation to read. The transition row, the
// episode change and the published event are written in one transaction: an alarm
// that clears but whose "cleared" event is lost is a linkage that never fires, and
// an event announced for a clear that rolled back is worse.
func (r *Runtime) decide(ctx context.Context, in decisionInput) (Decision, error) {
	var dec Decision
	err := r.store.WithTx(ctx, func(tx *sql.Tx) error {
		d, err := r.decideInTx(ctx, tx, in)
		dec = d
		return err
	})
	if err != nil {
		return Decision{}, err
	}
	return dec, nil
}

func (r *Runtime) decideInTx(ctx context.Context, tx *sql.Tx, in decisionInput) (Decision, error) {
	rule := in.rule
	key := rule.PolicyKey()
	active, err := r.store.GetActiveEpisodeTx(ctx, tx, rule.TenantID, key, in.subject, in.dimension)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Decision{}, err
	}

	switch in.kind {
	case OutcomeQualified:
		return r.decideQualified(ctx, tx, in, active)
	case TransitionClear:
		return r.decideClear(ctx, tx, in, active)
	case OutcomeUnknown, OutcomeError:
		return r.decideUndetermined(ctx, tx, in, active)
	}
	return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonNoEpisode}, nil
}

func (r *Runtime) decideQualified(ctx context.Context, tx *sql.Tx, in decisionInput,
	active *store.Episode) (Decision, error) {

	rule := in.rule
	evidence := evidenceBytes(in.res, in.source, in.evidence)

	if active == nil {
		// No "for" gate here. The wait belongs to the detector's entry point and
		// lives there exactly once (submitDetectorVerdict), because that is the
		// only place that knows "this is a fresh observation of a continuous
		// condition". Checking it again here would block the very verdict the
		// timer produces after the wait has already elapsed, and would also
		// wrongly delay an event-channel qualification — an event is a fact that
		// happened, not a condition to sustain.
		ep := &store.Episode{
			ID: NewID(), TenantID: rule.TenantID, PolicyID: rule.ID, PolicyKey: rule.PolicyKey(),
			SubjectID: in.subject, Dimension: in.dimension, State: "active",
			Level: rule.AlarmLevel(), Source: in.source,
			Revision: rule.Revision, Epoch: int64(rule.Revision),
			OpenedAt: in.now, FirstSeq: in.seq, LastSeq: in.seq,
		}
		if err := r.store.OpenEpisodeTx(ctx, tx, ep); err != nil {
			// A concurrent detector opened it first; the partial unique index
			// guarantees there is still exactly one active alarm. Refresh it
			// instead of failing the whole evaluation.
			existing, gerr := r.store.GetActiveEpisodeTx(ctx, tx, rule.TenantID, rule.PolicyKey(), in.subject, in.dimension)
			if gerr != nil {
				return Decision{}, err
			}
			if _, rerr := r.store.RefreshEpisode(ctx, tx, existing.ID, in.seq, evidence); rerr != nil {
				return Decision{}, rerr
			}
			return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonStillTrue,
				EpisodeID: existing.ID}, nil
		}
		tid := NewID()
		if _, err := r.store.AddTransition(ctx, tx, &store.Transition{
			ID: tid, EpisodeID: ep.ID, TenantID: rule.TenantID, Kind: TransitionQualified,
			Seq: in.seq, Revision: rule.Revision, Epoch: ep.Epoch, Evidence: evidence,
		}); err != nil {
			return Decision{}, err
		}
		if err := r.publishAlarmEvent(ctx, tx, rule, ep, in.seq, TransitionQualified, "active", tid, in.c); err != nil {
			return Decision{}, err
		}
		return Decision{Outcome: OutcomeQualified, EpisodeID: ep.ID, TransitionID: tid}, nil
	}

	// The condition is already abnormal. That is not a new alarm: it is the same
	// alarm with fresher evidence.
	if in.seq <= active.LastSeq {
		return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonOutOfOrder,
			EpisodeID: active.ID}, nil
	}
	if rule.Revision > active.Revision {
		// Publishing a new version must not duplicate the alarm and must not clear
		// it either. The episode keeps its identity and simply starts being judged
		// by the new revision.
		if _, err := r.store.AdvanceEpisodeRevision(ctx, tx, active.ID, rule.Revision, in.seq, evidence); err != nil {
			return Decision{}, err
		}
		return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonRevisionAdvanced,
			EpisodeID: active.ID}, nil
	}
	if _, err := r.store.RefreshEpisode(ctx, tx, active.ID, in.seq, evidence); err != nil {
		return Decision{}, err
	}
	return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonStillTrue,
		EpisodeID: active.ID}, nil
}

func (r *Runtime) decideClear(ctx context.Context, tx *sql.Tx, in decisionInput,
	active *store.Episode) (Decision, error) {

	rule := in.rule
	if active == nil {
		return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonNoEpisode}, nil
	}
	evidence := evidenceBytes(in.res, in.source, in.evidence)

	// Recovery is a separate, explicit condition. "The abnormal condition stopped
	// holding" is not a recovery: without hysteresis it would flip on the first
	// normal reading, and with a neutral band it would clear in the middle of it.
	//
	// An authorised event source is different: it says "clear this alarm" and it
	// was whitelisted to say so. The detector channel is the one that has to earn
	// a recovery through the declared condition.
	if in.source == string(domain.AlarmSourceDetector) && rule.Definition.Recovery == nil {
		return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonNoRecovery,
			EpisodeID: active.ID}, nil
	}
	if rule.Definition.Recovery != nil && in.source == string(domain.AlarmSourceDetector) && !in.skipRecoveryCheck {
		rec := domain.Evaluate(*rule.Definition.Recovery,
			r.evalContext(ctx, &rule.Definition, in.subject, nil, in.now))
		switch rec.Verdict {
		case domain.VerdictTrue:
			if rule.Definition.Recovery.For.Std() > 0 {
				scope := ScopeKey(&rule.Definition, rule.ID, in.subject)
				if err := r.startRecoveryPending(ctx, rule, in.subject, scope, in.now, in.seq, in.c); err != nil {
					return Decision{}, err
				}
				return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonWaitingRecovery,
					EpisodeID: active.ID}, nil
			}
			evidence = evidenceBytes(rec, in.source+":recovery", in.evidence)
		case domain.VerdictFalse:
			if err := r.recordFactBytes(ctx, tx, active, TransitionNoTransition, in.seq, evidence); err != nil {
				return Decision{}, err
			}
			return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonHysteresisNeutral,
				EpisodeID: active.ID}, nil
		default:
			kind := TransitionUnknown
			if rec.Verdict == domain.VerdictError {
				kind = TransitionError
			}
			if err := r.recordFact(ctx, tx, active, kind, in.seq, evidenceOf(rec, in.source+":recovery", nil)); err != nil {
				return Decision{}, err
			}
			return Decision{Outcome: string(rec.Verdict), ReasonCode: "recovery_undetermined",
				EpisodeID: active.ID}, nil
		}
	}

	outcome, err := r.store.ClearEpisode(ctx, tx, active.ID, in.seq, rule.Revision)
	if err != nil {
		return Decision{}, err
	}
	switch outcome {
	case store.ClearApplied:
	case store.ClearOutOfOrder:
		return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonOutOfOrder,
			EpisodeID: active.ID}, nil
	case store.ClearStaleRev:
		// An executor pinned to a superseded revision must not clear an alarm that
		// a newer revision is still asserting.
		return Decision{Outcome: OutcomeRejected, ReasonCode: ReasonStaleRevision,
			EpisodeID: active.ID}, nil
	default:
		return Decision{Outcome: OutcomeNoTransition, ReasonCode: ReasonNoEpisode,
			EpisodeID: active.ID}, nil
	}

	tid := NewID()
	if _, err := r.store.AddTransition(ctx, tx, &store.Transition{
		ID: tid, EpisodeID: active.ID, TenantID: rule.TenantID, Kind: TransitionClear,
		Seq: in.seq, Revision: rule.Revision, Epoch: active.Epoch, Evidence: evidence,
	}); err != nil {
		return Decision{}, err
	}
	if err := r.publishAlarmEvent(ctx, tx, rule, active, in.seq, TransitionClear, "cleared", tid, in.c); err != nil {
		return Decision{}, err
	}
	return Decision{Outcome: OutcomeCleared, EpisodeID: active.ID, TransitionID: tid}, nil
}

func (r *Runtime) decideUndetermined(ctx context.Context, tx *sql.Tx, in decisionInput,
	active *store.Episode) (Decision, error) {

	// An unknown or broken evaluation is not a recovery. It is recorded against
	// the active alarm so "why is this still open" has an answer, and it changes
	// nothing about the alarm's state.
	if active == nil {
		return Decision{Outcome: in.kind, ReasonCode: ReasonNoEpisode}, nil
	}
	kind := TransitionUnknown
	if in.kind == OutcomeError {
		kind = TransitionError
	}
	if err := r.recordFact(ctx, tx, active, kind, in.seq, evidenceOf(in.res, in.source, in.evidence)); err != nil {
		return Decision{}, err
	}
	return Decision{Outcome: in.kind, EpisodeID: active.ID}, nil
}

func (r *Runtime) recordFact(ctx context.Context, tx *sql.Tx, ep *store.Episode, kind string,
	seq int64, evidence map[string]any) error {
	_, err := r.store.AddTransition(ctx, tx, &store.Transition{
		ID: NewID(), EpisodeID: ep.ID, TenantID: ep.TenantID, Kind: kind,
		Seq: seq, Revision: ep.Revision, Epoch: ep.Epoch, Evidence: evidence,
	})
	return err
}

// recordFactBytes is the same write for callers that already hold encoded
// evidence, so a byte slice never has to be decoded just to be re-encoded.
func (r *Runtime) recordFactBytes(ctx context.Context, tx *sql.Tx, ep *store.Episode, kind string,
	seq int64, evidence []byte) error {
	_, err := r.store.AddTransition(ctx, tx, &store.Transition{
		ID: NewID(), EpisodeID: ep.ID, TenantID: ep.TenantID, Kind: kind,
		Seq: seq, Revision: ep.Revision, Epoch: ep.Epoch, Evidence: json.RawMessage(evidence),
	})
	return err
}

// publishAlarmEvent writes the transition into the existing event exit, inside the
// same transaction. The payload carries the stable transition id (the dedupe key
// for whoever consumes it), the policy and subject, the sequence and the causal
// chain, so a linked rule can be traced back and stopped at the hop limit.
func (r *Runtime) publishAlarmEvent(ctx context.Context, tx *sql.Tx, rule *store.Rule,
	ep *store.Episode, seq int64, kind, state, transitionID string, c cause) error {

	root := c.Root
	if root == "" {
		root = transitionID
	}
	payload := map[string]any{
		"transition_id": transitionID, "kind": kind, "state": state,
		"episode_id": ep.ID, "policy_id": rule.ID, "policy_key": rule.PolicyKey(),
		"subject_id": ep.SubjectID, "device_id": ep.SubjectID, "dimension": ep.Dimension,
		"level": ep.Level, "source": ep.Source,
		"seq": seq, "revision": rule.Revision, "event_id": c.EventID,
		"causal_root": root, "causal_hop": c.Hop,
	}
	if _, err := r.store.PublishEvent(ctx, tx, NewID(), rule.TenantID, "alarm."+kind, ep.ID, payload); err != nil {
		return err
	}
	return r.enqueueAlarmNotification(ctx, tx, rule, ep, transitionID, kind)
}

// enqueueAlarmNotification writes the notification intent in the same transaction
// as the alarm transition.
//
// Two facts depend on this being one transaction: an alarm that exists always has
// its message queued (so "the alarm fired but nobody was told" cannot happen), and
// a message never exists for a transition that rolled back. The body is frozen
// here — group revision, occurrence time and expiry — so a later edit of the
// notification group cannot rewrite a message that is already queued.
func (r *Runtime) enqueueAlarmNotification(ctx context.Context, tx *sql.Tx, rule *store.Rule,
	ep *store.Episode, transitionID, kind string) error {

	spec := rule.Definition.Alarm
	if spec == nil || spec.NotifyPolicyID == "" {
		return nil
	}
	// Only the two real state changes notify. An "unknown" note is a fact for the
	// operator, not a reason to page somebody at 3am.
	if kind != TransitionQualified && kind != TransitionClear {
		return nil
	}
	now := r.Now()
	text := "告警触发：" + rule.Name
	if kind == TransitionClear {
		text = "告警恢复：" + rule.Name
	}
	body, _ := json.Marshal(map[string]any{
		"policy_id":      spec.NotifyPolicyID,
		"group_revision": spec.NotifyRevision,
		"subject":        ep.SubjectID,
		"text":           text,
		"occurred_at":    now.UTC().Format(time.RFC3339Nano),
		"expires_at":     now.Add(24 * time.Hour).UTC().Format(time.RFC3339Nano),
		"variables": map[string]any{
			"episode_id": ep.ID, "policy_key": rule.PolicyKey(), "level": ep.Level,
			"subject_id": ep.SubjectID, "dimension": ep.Dimension,
			"transition_id": transitionID, "kind": kind,
		},
	})
	_, err := r.store.EnqueueOutbox(ctx, tx, &store.OutboxItem{
		ID: NewID(), TenantID: rule.TenantID, Kind: "notification",
		ScopeKey: "alarm:" + ep.ID, Generation: 0,
		// The transition id makes the intent exactly-once per transition, no
		// matter how many times the decision is replayed.
		IdempotencyKey: "alarm:" + transitionID + ":notify",
		Payload:        body,
	})
	return err
}

func evidenceBytes(res domain.Result, source string, extra any) []byte {
	body, _ := json.Marshal(evidenceOf(res, source, extra))
	return body
}

func evidenceOf(res domain.Result, source string, extra any) map[string]any {
	out := map[string]any{
		"verdict": string(res.Verdict),
		"source":  source,
	}
	if len(res.Reasons) > 0 {
		out["reasons"] = res.Reasons
	}
	if extra != nil {
		out["payload"] = extra
	}
	return out
}

// Ack acknowledges an episode. Acknowledgement changes "someone knows", not
// "the device is healthy": the episode stays active and the abnormal condition
// keeps being tracked.
func (r *Runtime) Ack(ctx context.Context, tenantID, episodeID, actor string) (bool, error) {
	seq, err := r.store.NextSeq(ctx)
	if err != nil {
		return false, err
	}
	ep, err := r.store.GetActiveEpisodeByID(ctx, tenantID, episodeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	ok, err := r.store.AckEpisode(ctx, tenantID, episodeID, actor, seq)
	if err != nil || !ok {
		return ok, err
	}
	err = r.store.WithTx(ctx, func(tx *sql.Tx) error {
		_, aerr := r.store.AddTransition(ctx, tx, &store.Transition{
			ID: NewID(), EpisodeID: ep.ID, TenantID: tenantID, Kind: TransitionAck,
			Seq: seq, Revision: ep.Revision, Epoch: ep.Epoch,
			Evidence: map[string]any{"actor": actor, "state": "active"},
		})
		return aerr
	})
	return true, err
}

// startRecoveryPending opens the "recovery condition has held since" record. It is
// a separate scope so it cannot collide with the trigger's pending window.
func (r *Runtime) startRecoveryPending(ctx context.Context, rule *store.Rule, deviceID, scope string,
	now time.Time, seq int64, c cause) error {

	p := &store.Pending{
		ScopeKey: scope + ":recovery", TenantID: rule.TenantID, RuleID: rule.ID, Revision: rule.Revision,
		DeviceID: deviceID, StartedAt: now,
		Deadline:       now.Add(rule.Definition.Recovery.For.Std()),
		LastEvidenceAt: now, EvidenceSeq: seq, RequireProcessedBefore: now,
		CausalRoot: c.Root, CausalHop: c.Hop,
	}
	return r.store.UpsertPending(ctx, p)
}

// performAlarm executes an alarm step: the run raises a transition on the
// referenced policy for the run's subject device.
//
// It goes through the event channel, not through the detector. A scene or an
// automation is a business event, and it must be authorised like one — otherwise
// any flow could forge a detector verdict for any policy.
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
	kind := TransitionQualified
	if step.Value == TransitionClear {
		kind = TransitionClear
	}
	// The action id is the run's step identity: stable across retries, different
	// for a second run. That is what makes "重投不重复执行" true at the alarm
	// owner instead of relying on the dispatcher to behave.
	dec, err := r.SubmitAlarmEvent(ctx, run.TenantID, policy.ID, AlarmEventRequest{
		ActionID:  stepID,
		Source:    "automation",
		Kind:      kind,
		SubjectID: subject,
		Payload: map[string]any{
			"run_id": run.ID, "causal_root": run.CausalRoot, "causal_hop": run.CausalHop,
		},
		Cause: cause{Root: run.CausalRoot, Hop: run.CausalHop, EventID: run.TriggerEventID},
	})
	if err != nil {
		return stepFailed, err
	}
	switch dec.Outcome {
	case OutcomeQualified, OutcomeCleared, OutcomeNoTransition:
	default:
		detail, _ := json.Marshal(map[string]any{
			"policy_id": policy.ID, "subject": subject, "reason": dec.ReasonCode,
			"outcome": dec.Outcome,
		})
		_ = r.store.UpdateStepStatus(ctx, stepID, "failed", string(detail))
		return stepFailed, nil
	}
	detail, _ := json.Marshal(map[string]any{
		"policy_id": policy.ID, "policy_key": policy.PolicyKey(), "subject": subject,
		"outcome": dec.Outcome, "episode_id": dec.EpisodeID, "transition_id": dec.TransitionID,
	})
	if err := r.store.UpdateStepStatus(ctx, stepID, "succeeded", string(detail)); err != nil {
		return stepFailed, err
	}
	return stepOK, nil
}

// PolicyRevision exposes the published revision of an alarm policy so an
// external executor can pin the same immutable parameters (A28).
//
// A rule that exists but is not an alarm policy is reported as not found, not as
// an internal failure: the caller asked for "the alarm policy with this id", and
// there is none. Turning that into a 500 would tell an external engine its
// request was broken when the real answer is "you named the wrong rule", and it
// would put a stack trace where the caller needs `policy_missing`.
func (r *Runtime) PolicyRevision(ctx context.Context, tenantID, policyID string) (*store.Rule, error) {
	rule, err := r.store.GetRule(ctx, tenantID, policyID)
	if err != nil {
		return nil, err
	}
	if rule.Kind != domain.KindAlarm {
		return nil, fmt.Errorf("%w: rule %s is not an alarm policy", store.ErrNotFound, policyID)
	}
	return rule, nil
}
