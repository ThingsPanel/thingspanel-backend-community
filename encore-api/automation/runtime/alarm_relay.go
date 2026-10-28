package runtime

import (
	"context"
	"encoding/json"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// Alarm relay (A15/A05).
//
// An alarm transition is a business event. To let a rule react to it, the
// transition has to become an input the evaluator can see — but the wrong way to
// do that is to poll alarm_episodes and compare timestamps, because then "did this
// already fire" depends on clock skew and on how often the poller runs.
//
// The relay therefore reads the event exit (auto_events), which is written in the
// same transaction as the lifecycle change, and turns each unrelayed alarm event
// into one alarm signal with a stable event id derived from the transition id.
// Redelivery is then handled by the ingress dedupe that already exists, and the
// chain carries its hop count so a deliberately constructed alarm↔linkage loop is
// stopped at MaxCausalHop with a trace instead of running forever.

// AlarmSignalKey is the condition key a rule uses to react to a transition kind.
//
// Every published transition becomes a signal under its own key, so a rule that
// only cares about "qualified" is not woken by a no-data note, and a rule that
// does care about silence can subscribe to "no_data" instead of inferring it from
// the absence of something.
func AlarmSignalKey(kind string) string {
	if kind == "" {
		return TransitionQualified
	}
	return kind
}

// RelayAlarmEvents publishes unrelayed alarm transitions into the ingress.
func (r *Runtime) RelayAlarmEvents(ctx context.Context, limit int) (int, error) {
	events, err := r.store.UnrelayedAlarmEvents(ctx, limit)
	if err != nil {
		return 0, err
	}
	relayed := 0
	for _, ev := range events {
		var p struct {
			TransitionID string `json:"transition_id"`
			Kind         string `json:"kind"`
			EpisodeID    string `json:"episode_id"`
			PolicyID     string `json:"policy_id"`
			PolicyKey    string `json:"policy_key"`
			SubjectID    string `json:"subject_id"`
			DeviceID     string `json:"device_id"`
			Dimension    string `json:"dimension"`
			Level        string `json:"level"`
			Revision     int    `json:"revision"`
			CausalRoot   string `json:"causal_root"`
			CausalHop    int    `json:"causal_hop"`
		}
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			// A malformed payload cannot be turned into a signal. Mark it relayed
			// so one bad row does not block the whole feed forever.
			_ = r.store.MarkEventRelayed(ctx, nil, ev.ID)
			continue
		}
		if p.TransitionID == "" {
			_ = r.store.MarkEventRelayed(ctx, nil, ev.ID)
			continue
		}
		subject := p.SubjectID
		if subject == "" {
			subject = p.DeviceID
		}
		hop := p.CausalHop + 1
		_, ierr := r.Ingest(ctx, store.IngestEvent{
			EventID:    "alarm:" + p.TransitionID,
			TenantID:   ev.TenantID,
			DeviceID:   subject,
			Signal:     domain.SignalAlarm,
			Key:        AlarmSignalKey(p.Kind),
			Value:      p.Kind,
			Quality:    domain.QualityGood,
			ObservedAt: ev.CreatedAt,
			Source:     "alarm_relay",
			InputClass: "alarm",
			CausalRoot: p.CausalRoot,
			CausalHop:  hop,
			Params: map[string]any{
				"transition_id": p.TransitionID,
				"episode_id":    p.EpisodeID,
				"policy_id":     p.PolicyID,
				"policy_key":    p.PolicyKey,
				"dimension":     p.Dimension,
				"level":         p.Level,
				"revision":      p.Revision,
				"kind":          p.Kind,
			},
		})
		if ierr != nil {
			// Leave the row unrelayed: the next pass retries. The ingress dedupe
			// makes a partial success safe.
			continue
		}
		_ = r.store.MarkEventRelayed(ctx, nil, ev.ID)
		relayed++
	}
	return relayed, nil
}
