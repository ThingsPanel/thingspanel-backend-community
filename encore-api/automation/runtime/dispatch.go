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

func (r *Runtime) finishDispatch(ctx context.Context, item *store.OutboxItem, state, ref, msg string, err error) (bool, string, string, string, error) {
	if err != nil && state == "" {
		state = "unknown"
	}
	switch state {
	case "accepted":
		_ = r.store.MarkOutbox(ctx, item.ID, "accepted", ref, msg, r.Now())
		return true, "accepted", ref, msg, nil
	case "failed":
		_ = r.store.MarkOutbox(ctx, item.ID, "failed", "", msg, r.Now())
		return false, "failed", "", msg, nil
	default:
		// Unknown keeps the intent visible and retryable; it is never rewritten
		// into a success and never re-sent with a new identity.
		retry := r.Now().Add(30 * time.Second)
		_ = r.store.MarkOutbox(ctx, item.ID, "dispatched", "", msg, retry)
		if err == nil {
			err = errors.New(msg)
		}
		return false, "unknown", "", msg, nil
	}
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
