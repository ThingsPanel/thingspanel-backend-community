package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"encore.app/automation/ports"
	"encore.app/automation/store"
)

// dispatchOne performs the outbound call for one intent and records the exact
// level of proof it obtained: accepted by the owner, rejected, or unknown.
func (r *Runtime) dispatchOne(ctx context.Context, item *store.OutboxItem) (bool, string, string, string, error) {
	// A cancelled run must not send new actions. This guard runs immediately
	// before the call, which is the strongest check available in this process;
	// the authoritative generation check belongs to the command owner (A10).
	if item.RunID != "" {
		status, err := r.store.RunStatus(ctx, item.RunID)
		if err == nil && status == StatusCancelled {
			_ = r.store.MarkOutbox(ctx, item.ID, "cancelled", "", "run cancelled before dispatch", r.Now())
			return false, "cancelled", "", "run cancelled before dispatch", nil
		}
	}

	switch item.Kind {
	case "command":
		var p struct {
			Kind       string `json:"kind"`
			DeviceID   string `json:"device_id"`
			Identifier string `json:"identifier"`
			Value      string `json:"value"`
		}
		if err := json.Unmarshal(item.Payload, &p); err != nil {
			_ = r.store.MarkOutbox(ctx, item.ID, "failed", "", err.Error(), r.Now())
			return false, "failed", "", err.Error(), nil
		}
		if r.cmds == nil {
			_ = r.store.MarkOutbox(ctx, item.ID, "unknown", "", "command owner not configured", r.Now())
			return false, "unknown", "", "command owner not configured", ports.ErrNotConfigured
		}
		res, err := r.cmds.Dispatch(ctx, ports.CommandRequest{
			Kind: ports.CommandKind(p.Kind), TenantID: item.TenantID, DeviceID: p.DeviceID,
			Identifier: p.Identifier, Value: p.Value, IdempotencyKey: item.IdempotencyKey,
			ScopeKey: item.ScopeKey, Generation: item.Generation,
		})
		return r.finishDispatch(ctx, item, res.State, res.Ref, res.Message, err)

	case "notification":
		var p struct {
			PolicyID string         `json:"policy_id"`
			Subject  string         `json:"subject"`
			Text     string         `json:"text"`
			Vars     map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(item.Payload, &p); err != nil {
			_ = r.store.MarkOutbox(ctx, item.ID, "failed", "", err.Error(), r.Now())
			return false, "failed", "", err.Error(), nil
		}
		if r.notif == nil {
			_ = r.store.MarkOutbox(ctx, item.ID, "unknown", "", "notification owner not configured", r.Now())
			return false, "unknown", "", "notification owner not configured", ports.ErrNotConfigured
		}
		res, err := r.notif.Submit(ctx, ports.NotificationRequest{
			TenantID: item.TenantID, PolicyID: p.PolicyID, Subject: p.Subject, Text: p.Text,
			Variables: p.Vars, SourceEventID: item.ID, ActionID: item.IdempotencyKey,
			OccurredAt: r.Now(), IdempotencyKey: item.IdempotencyKey,
		})
		return r.finishDispatch(ctx, item, res.State, res.NotificationID, res.Message, err)

	case "scene":
		var p struct {
			SceneID string `json:"scene_id"`
		}
		if err := json.Unmarshal(item.Payload, &p); err != nil {
			_ = r.store.MarkOutbox(ctx, item.ID, "failed", "", err.Error(), r.Now())
			return false, "failed", "", err.Error(), nil
		}
		activator, ok := r.cmds.(ports.SceneActivator)
		if !ok || activator == nil {
			_ = r.store.MarkOutbox(ctx, item.ID, "unknown", "", "scene activator not configured", r.Now())
			return false, "unknown", "", "scene activator not configured", ports.ErrNotConfigured
		}
		state, ref, msg, err := activator.ActivateScene(ctx, item.TenantID, p.SceneID)
		return r.finishDispatch(ctx, item, state, ref, msg, err)

	default:
		_ = r.store.MarkOutbox(ctx, item.ID, "failed", "", "unsupported outbox kind: "+item.Kind, r.Now())
		return false, "failed", "", "unsupported outbox kind: " + item.Kind, nil
	}
}

// finishDispatch turns the owner's answer into an outbox state.
//
// The distinction that matters is "may I send this again?":
//
//   - accepted            → accepted, terminal
//   - failed / conflict   → failed, terminal (a key reuse is a bug, not a retry)
//   - superseded          → cancelled, terminal: a newer trigger already won, so
//     this intent must never reach a device, and the run it belongs to is
//     recorded as superseded rather than failed
//   - in_flight           → dispatched, short retry: an identical attempt is
//     still running; asking again shortly will replay its result
//   - unknown             → unresolved, terminal: the previous attempt died
//     mid-flight. Retrying with the same key cannot succeed and retrying with a
//     new key would risk a second physical action, so a human must reconcile it.
func (r *Runtime) finishDispatch(ctx context.Context, item *store.OutboxItem, state, ref, msg string, err error) (bool, string, string, string, error) {
	if err != nil && state == "" {
		state = ports.CommandUnknown
	}
	switch state {
	case ports.CommandAccepted:
		_ = r.store.MarkOutbox(ctx, item.ID, "accepted", ref, msg, r.Now())
		r.confirmStep(ctx, item, "accepted", "accepted_by_command_owner")
		return true, "accepted", ref, msg, nil

	case ports.CommandFailed, ports.CommandConflict:
		_ = r.store.MarkOutbox(ctx, item.ID, "failed", "", msg, r.Now())
		return false, "failed", "", msg, nil

	case ports.CommandSuperseded:
		_ = r.store.MarkOutbox(ctx, item.ID, "cancelled", ref, msg, r.Now())
		r.confirmStep(ctx, item, "cancelled", "superseded_by_newer_generation")
		r.recordSuperseded(ctx, item, msg)
		return false, "superseded", ref, msg, nil

	case ports.CommandInFlight:
		// Not an error: the first attempt owns the key and will record an
		// outcome. Asking again in a few seconds replays it.
		_ = r.store.MarkOutbox(ctx, item.ID, "dispatched", ref, msg, r.Now().Add(inFlightRetry))
		return false, "in_flight", ref, msg, nil

	default:
		// Unknown keeps the intent visible but stops the automatic retry loop:
		// the same key will keep answering "unknown", and a new key would be a
		// blind resend of a possibly-executed non-idempotent action.
		_ = r.store.MarkOutbox(ctx, item.ID, "unresolved", ref, msg, r.Now())
		if err == nil {
			err = errors.New(msg)
		}
		return false, "unknown", ref, msg, nil
	}
}

// inFlightRetry is deliberately shorter than the transport retry: the answer is
// expected to change within one request lifetime.
const inFlightRetry = 5 * time.Second

// confirmStep closes the loop between a retried intent and the step that
// produced it.
//
// Without this, a step whose first attempt answered "still processing" would
// stay "unknown" forever while its outbox row later succeeded — the execution
// record and the intent would disagree, and the operator would see a failed step
// for an action that did reach the device.
func (r *Runtime) confirmStep(ctx context.Context, item *store.OutboxItem, status, reason string) {
	if item.StepID == "" {
		return
	}
	current, err := r.store.StepStatus(ctx, item.StepID)
	if err != nil || current != "unknown" {
		// Only an undecided step is completed here. A step that already reached
		// a terminal state keeps the outcome it actually had.
		return
	}
	detail, _ := json.Marshal(map[string]any{"reason": reason, "outbox_id": item.ID})
	_ = r.store.UpdateStepStatus(ctx, item.StepID, status, string(detail))
	if item.RunID != "" {
		_ = r.store.UpdateTargetStatus(ctx, item.RunID, deviceIDOf(item), status, "", reason)
	}
}

// deviceIDOf reads the device out of the stored intent so a retry can update the
// same target row it belongs to.
func deviceIDOf(item *store.OutboxItem) string {
	var p struct {
		DeviceID string `json:"device_id"`
	}
	_ = json.Unmarshal(item.Payload, &p)
	return p.DeviceID
}

// recordSuperseded marks the run so the execution record explains why an action
// never happened, instead of leaving it looking like a silent success.
func (r *Runtime) recordSuperseded(ctx context.Context, item *store.OutboxItem, msg string) {
	if item.RunID == "" {
		return
	}
	_ = r.store.CancelRun(ctx, item.RunID, "superseded: "+msg, r.Now())
}

// DispatchOutbox drains due intents. It is called by the timer loop and by the
// recovery path after a restart.
func (r *Runtime) DispatchOutbox(ctx context.Context, limit int) (int, error) {
	items, err := r.store.ClaimOutbox(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		if _, _, _, _, err := r.dispatchOne(ctx, item); err != nil {
			// A port that is not configured is a deployment gap, not a rule bug;
			// leave the intent pending with its reason visible.
			continue
		}
	}
	return len(items), nil
}

// DispatchByID is used by tests and by the manual "retry now" endpoint.
func (r *Runtime) DispatchByID(ctx context.Context, itemID string) error {
	item, err := r.store.GetOutbox(ctx, itemID)
	if err != nil {
		return err
	}
	_, _, _, _, err = r.dispatchOne(ctx, item)
	return err
}

func outboxSummary(items []*store.OutboxItem) map[string]int {
	out := map[string]int{}
	for _, i := range items {
		out[i.State]++
	}
	return out
}
