package runtime

import (
	"context"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// No-data handling (A08).
//
// "The device stopped reporting" is a different fact from "the condition became
// false", and from "the condition is still true". Folding silence into either
// one is what produces the two classic failures: an alarm that stays active for
// days on a reading from before the device died, and an alarm that clears
// itself because nobody answered.
//
// The rule declares how long silence may last (freshness.no_data). After that,
// the sweep records why the condition can no longer be confirmed. It never
// clears the episode: the device may be perfectly fine, and a cleared alarm
// would hide the fact that we simply stopped hearing from it.

// SweepNoData records a no-data note on every active alarm whose subject has
// been silent for longer than the policy allows.
//
// It returns how many episodes were noted in this pass. The note is written at
// most once per silence: repeating it on every tick would bury the lifecycle
// under identical rows.
func (r *Runtime) SweepNoData(ctx context.Context, now time.Time, limit int) (int, error) {
	episodes, err := r.store.ListActiveEpisodes(ctx, limit)
	if err != nil {
		return 0, err
	}
	noted := 0
	for _, ep := range episodes {
		rule, err := r.store.GetRuleRevision(ctx, ep.PolicyID, ep.Revision)
		if err != nil {
			// A policy that cannot be read is not a reason to touch its alarm.
			continue
		}
		window := rule.Definition.Freshness.NoData.Std()
		if window <= 0 {
			continue
		}
		last, ok, err := r.lastSeenFor(ctx, ep.SubjectID, rule.Definition.Trigger)
		if err != nil {
			continue
		}
		if ok && now.Sub(last) <= window {
			continue
		}
		if r.alreadyNoted(ctx, ep.ID) {
			continue
		}

		seq, serr := r.store.NextSeq(ctx)
		if serr != nil {
			return noted, serr
		}
		message := "设备已超过 " + window.String() + " 没有任何上报，条件是否仍成立无法确认"
		evidence := map[string]any{
			"verdict": "unknown", "code": "no_data",
			"message": message, "no_data": window.String(),
		}
		if ok {
			evidence["last_seen"] = last
			evidence["silent_for"] = now.Sub(last).String()
		} else {
			evidence["last_seen"] = nil
			message = "从未收到该设备的相关上报，条件是否成立无法确认"
			evidence["message"] = message
		}
		if _, err := r.store.AddTransition(ctx, nil, &store.Transition{
			ID: NewID(), EpisodeID: ep.ID, TenantID: ep.TenantID, Kind: TransitionUnknown,
			Seq: seq, Revision: ep.Revision, Epoch: ep.Epoch, Evidence: evidence,
		}); err != nil {
			return noted, err
		}
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), ep.TenantID, "alarm.no_data", ep.ID,
			map[string]any{
				"episode_id": ep.ID, "policy_id": ep.PolicyID, "device_id": ep.SubjectID,
				"no_data": window.String(), "last_seen": last,
				"reason": "设备静默超过 no_data，告警保持活动但状态不确定",
			})
		noted++
	}
	return noted, nil
}

// lastSeenFor returns the newest observation among the fields the trigger
// actually watches. Event conditions are skipped: an event is instantaneous, so
// its absence is not silence.
func (r *Runtime) lastSeenFor(ctx context.Context, deviceID string, t domain.Trigger) (time.Time, bool, error) {
	bySignal := map[domain.SignalKind][]string{}
	for _, g := range t.Groups {
		for _, c := range g.Conditions {
			if c.Signal == domain.SignalEvent || c.Key == "" {
				continue
			}
			bySignal[c.Signal] = append(bySignal[c.Signal], c.Key)
		}
	}
	var latest time.Time
	found := false
	for signal, keys := range bySignal {
		at, ok, err := r.store.LatestObservedAt(ctx, deviceID, signal, keys)
		if err != nil {
			return time.Time{}, false, err
		}
		if ok && (!found || at.After(latest)) {
			latest, found = at, true
		}
	}
	return latest, found, nil
}

// alreadyNoted reports whether the last lifecycle fact of the episode is already
// this same no-data note.
func (r *Runtime) alreadyNoted(ctx context.Context, episodeID string) bool {
	last, err := r.store.LastTransition(ctx, episodeID)
	if err != nil || last == nil || last.Kind != TransitionUnknown {
		return false
	}
	evidence, ok := last.Evidence.(map[string]any)
	if !ok {
		return false
	}
	code, _ := evidence["code"].(string)
	return code == "no_data"
}
