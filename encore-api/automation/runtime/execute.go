package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/store"
)

// Run statuses. "partial" and "unknown" are first class so a 200 target fan-out
// can never be reported as "all succeeded".
const (
	StatusAccepted  = "accepted"
	StatusRunning   = "running"
	StatusQueued    = "queued"
	StatusSucceeded = "succeeded"
	StatusPartial   = "partial"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusIgnored   = "ignored"
	StatusRejected  = "rejected"
	StatusExpired   = "expired"
)

// acceptRun applies the run-mode contract and, when admitted, creates the run
// with a frozen target snapshot.
func (r *Runtime) acceptRun(ctx context.Context, rule *store.Rule, deviceID, triggerEventID string,
	c cause, res domain.Result, now time.Time, seq int64) (*store.Run, error) {

	def := rule.Definition
	scope := ScopeKey(&def, rule.ID, deviceID)

	active, err := r.store.ActiveRunCount(ctx, scope)
	if err != nil {
		return nil, err
	}

	status := StatusAccepted
	switch def.Mode {
	case domain.ModeSingle:
		if active > 0 {
			// Record the decision instead of silently dropping the trigger.
			return r.recordSkippedRun(ctx, rule, deviceID, scope, triggerEventID, c, res, now,
				StatusIgnored, "mode_single_busy", "已有运行未结束，本次按 single 模式忽略")
		}
	case domain.ModeRestart:
		if active > 0 {
			if err := r.cancelScope(ctx, scope, "mode_restart", "新触发按 restart 模式替换旧运行"); err != nil {
				return nil, err
			}
		}
	case domain.ModeQueued:
		if active > 0 {
			depth, err := r.queuedDepth(ctx, scope)
			if err != nil {
				return nil, err
			}
			maxDepth := 10
			if def.Queue != nil && def.Queue.MaxDepth > 0 {
				maxDepth = def.Queue.MaxDepth
			}
			if depth >= maxDepth {
				return r.recordSkippedRun(ctx, rule, deviceID, scope, triggerEventID, c, res, now,
					StatusRejected, "queue_overflow",
					fmt.Sprintf("排队已满（%d），本次触发被拒绝", depth))
			}
			status = StatusQueued
		}
	case domain.ModeParallel:
		maxConcurrent := 4
		if def.Parallel != nil && def.Parallel.MaxConcurrent > 0 {
			maxConcurrent = def.Parallel.MaxConcurrent
		}
		if active >= maxConcurrent {
			return r.recordSkippedRun(ctx, rule, deviceID, scope, triggerEventID, c, res, now,
				StatusRejected, "parallel_limit",
				fmt.Sprintf("并行上限 %d 已满，本次触发被拒绝", maxConcurrent))
		}
	}

	generation, err := r.store.NextGeneration(ctx, scope)
	if err != nil {
		return nil, err
	}

	targets, err := r.resolveTargets(ctx, rule, deviceID)
	if err != nil {
		return r.recordSkippedRun(ctx, rule, deviceID, scope, triggerEventID, c, res, now,
			StatusRejected, "target_resolution_failed", err.Error())
	}

	snapshot, _ := json.Marshal(map[string]any{
		"rule_id":       rule.ID,
		"revision":      rule.Revision,
		"definition_hash": rule.DefinitionHash,
		"device_id":     deviceID,
		"targets":       len(targets),
		"generation":    generation,
		"verdict":       res.Verdict,
		"mode":          def.Mode,
	})
	deadline := now.Add(def.Budget.MaxRunDuration.Std())

	run := &store.Run{
		ID:             NewID(),
		TenantID:       rule.TenantID,
		RuleID:         rule.ID,
		Revision:       rule.Revision,
		DefinitionHash: rule.DefinitionHash,
		ScopeKey:       scope,
		Generation:     generation,
		DeviceID:       deviceID,
		TriggerEventID: triggerEventID,
		CausalRoot:     c.Root,
		CausalHop:      c.Hop,
		CauseEventID:   c.EventID,
		Status:         status,
		Snapshot:       snapshot,
		StartedAt:      now,
		Deadline:       &deadline,
	}
	if err := r.store.CreateRun(ctx, run); err != nil {
		return nil, err
	}
	if err := r.store.InsertTargets(ctx, run.ID, targets); err != nil {
		return nil, err
	}

	if status == StatusQueued {
		return run, nil
	}
	if err := r.executeRun(ctx, run.ID, 0); err != nil {
		return run, err
	}
	return run, nil
}

func (r *Runtime) recordSkippedRun(ctx context.Context, rule *store.Rule, deviceID, scope, triggerEventID string,
	c cause, res domain.Result, now time.Time, status, reasonCode, reason string) (*store.Run, error) {

	generation, _ := r.store.NextGeneration(ctx, scope)
	snapshot, _ := json.Marshal(map[string]any{
		"rule_id": rule.ID, "revision": rule.Revision, "device_id": deviceID,
		"verdict": res.Verdict, "reason": reasonCode,
	})
	run := &store.Run{
		ID: NewID(), TenantID: rule.TenantID, RuleID: rule.ID, Revision: rule.Revision,
		DefinitionHash: rule.DefinitionHash, ScopeKey: scope, Generation: generation,
		DeviceID: deviceID, TriggerEventID: triggerEventID, Status: status,
		CausalRoot: c.Root, CausalHop: c.Hop, CauseEventID: c.EventID,
		ReasonCode: reasonCode, Reason: reason, Snapshot: snapshot, StartedAt: now,
	}
	if err := r.store.CreateRun(ctx, run); err != nil {
		return nil, err
	}
	_, _ = r.store.PublishEvent(ctx, nil, NewID(), rule.TenantID, "run."+status, run.ID,
		map[string]any{"rule_id": rule.ID, "device_id": deviceID, "reason": reasonCode})
	return run, nil
}

func (r *Runtime) queuedDepth(ctx context.Context, scope string) (int, error) {
	return r.store.CountRunsByStatus(ctx, scope, StatusQueued)
}

// cancelScope stops future steps of every active run in the scope. Already
// accepted side effects stay accepted: cancelling configuration is not the same
// as recalling a command the device may already have executed.
func (r *Runtime) cancelScope(ctx context.Context, scope, reasonCode, reason string) error {
	ids, err := r.store.CancelActiveRuns(ctx, scope, reasonCode, reason)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = r.store.CancelRunTimers(ctx, id)
		_ = r.store.CancelRunOutbox(ctx, id)
	}
	if len(ids) > 0 {
		_, _ = r.store.PublishEvent(ctx, nil, NewID(), "", "run.cancelled", scope,
			map[string]any{"scope": scope, "runs": ids, "reason": reasonCode})
	}
	return nil
}

// ---------------------------------------------------------------------------
// Targets
// ---------------------------------------------------------------------------

func (r *Runtime) resolveTargets(ctx context.Context, rule *store.Rule, triggerDeviceID string) ([]store.Target, error) {
	spec := rule.Definition.Targets
	seen := map[string]bool{}
	var out []store.Target

	add := func(d ports.Device, status, code, reason string) {
		if seen[d.ID] {
			return
		}
		seen[d.ID] = true
		out = append(out, store.Target{DeviceID: d.ID, Status: status, ReasonCode: code, Reason: reason})
	}

	if r.dir == nil {
		if len(spec.DeviceIDs) == 0 {
			return nil, errors.New("device directory not configured: cannot resolve label/type/config targets")
		}
		for _, id := range spec.DeviceIDs {
			add(ports.Device{ID: id}, "selected", "", "")
		}
		return out, nil
	}

	if len(spec.DeviceIDs) > 0 {
		found, err := r.dir.ByIDs(ctx, rule.TenantID, spec.DeviceIDs)
		if err != nil {
			return nil, err
		}
		byID := map[string]ports.Device{}
		for _, d := range found {
			byID[d.ID] = d
		}
		for _, id := range spec.DeviceIDs {
			if d, ok := byID[id]; ok {
				add(d, "selected", "", "")
			} else {
				if !seen[id] {
					seen[id] = true
					out = append(out, store.Target{
						DeviceID: id, Status: "excluded",
						ReasonCode: "device_not_found", Reason: "设备不存在或不在本租户",
					})
				}
			}
		}
	}
	if len(spec.DeviceConfigIDs) > 0 {
		found, err := r.dir.ByDeviceConfig(ctx, rule.TenantID, spec.DeviceConfigIDs)
		if err != nil {
			return nil, err
		}
		for _, d := range found {
			add(d, "selected", "", "")
		}
	}
	if len(spec.Labels) > 0 {
		found, err := r.dir.ByLabels(ctx, rule.TenantID, spec.Labels, spec.LabelMatch == "and")
		switch {
		case errors.Is(err, ports.ErrLabelNotExposed):
			// Report the gap explicitly instead of resolving to an empty set.
			for _, l := range spec.Labels {
				out = append(out, store.Target{
					DeviceID: "label:" + l, Status: "excluded",
					ReasonCode: "label_not_exposed",
					Reason:     "设备目录未返回标签字段，标签目标尚未启用（需目录 owner 补字段）",
				})
			}
		case err != nil:
			return nil, err
		default:
			for _, d := range found {
				add(d, "selected", "", "")
			}
		}
	}
	if len(spec.DeviceTypes) > 0 {
		for _, t := range spec.DeviceTypes {
			found, err := r.dir.ByType(ctx, rule.TenantID, t)
			if err != nil {
				return nil, err
			}
			for _, d := range found {
				add(d, "selected", "", "")
			}
		}
	}
	// Areas are not part of the community device directory yet. Report it
	// explicitly instead of silently resolving to an empty target set.
	if len(spec.AreaIDs) > 0 {
		for _, area := range spec.AreaIDs {
			out = append(out, store.Target{
				DeviceID: "area:" + area, Status: "excluded",
				ReasonCode: "area_not_supported",
				Reason:     "当前设备目录没有区域层级，区域目标尚未启用（需目录 owner 增加成员关系）",
			})
		}
	}

	// Capability filter.
	if spec.RequiredCapability != "" {
		filtered := out[:0]
		for _, t := range out {
			if t.Status != "selected" {
				filtered = append(filtered, t)
				continue
			}
			d, ok := r.deviceByID(ctx, rule.TenantID, t.DeviceID)
			if !ok {
				t.Status = "excluded"
				t.ReasonCode = "device_unavailable"
				t.Reason = "执行前复核时设备不可见"
				filtered = append(filtered, t)
				continue
			}
			if !hasCapability(d, spec.RequiredCapability) {
				t.Status = "excluded"
				t.ReasonCode = "capability_missing"
				t.Reason = "设备不具备所需能力: " + spec.RequiredCapability
			}
			filtered = append(filtered, t)
		}
		out = filtered
	}

	// Budget cap.
	if spec.MaxTargets > 0 {
		selected := 0
		for i := range out {
			if out[i].Status == "selected" {
				selected++
				if selected > spec.MaxTargets {
					out[i].Status = "excluded"
					out[i].ReasonCode = "target_budget_exceeded"
					out[i].Reason = fmt.Sprintf("超过单次目标上限 %d", spec.MaxTargets)
				}
			}
		}
	}
	return out, nil
}

func (r *Runtime) deviceByID(ctx context.Context, tenantID, deviceID string) (ports.Device, bool) {
	if r.dir == nil {
		return ports.Device{}, false
	}
	list, err := r.dir.ByIDs(ctx, tenantID, []string{deviceID})
	if err != nil || len(list) == 0 {
		return ports.Device{}, false
	}
	return list[0], true
}

func hasCapability(d ports.Device, capability string) bool {
	if len(d.Capabilities) == 0 {
		// The community directory does not expose capability lists yet; treat a
		// missing list as "unknown" and let the command owner reject the action.
		return true
	}
	for _, c := range d.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Execution
// ---------------------------------------------------------------------------

// executeRun walks the compiled program from a program counter. It is safe to
// call again after a restart: terminal steps are skipped and a pending wait
// simply returns without changing state.
func (r *Runtime) executeRun(ctx context.Context, runID string, startPC int) error {
	run, err := r.store.GetRunByID(ctx, runID)
	if err != nil {
		return err
	}

	// Branch conditions evaluate against the revision the run was admitted on,
	// so a concurrent edit cannot change what a half-finished program means.
	var maxAge domain.Duration
	if pinned, perr := r.store.GetRuleRevision(ctx, run.RuleID, run.Revision); perr == nil && pinned != nil {
		maxAge = pinned.Definition.Freshness.MaxAge
	}
	if run.Status != StatusAccepted && run.Status != StatusRunning && run.Status != StatusQueued {
		return nil
	}
	rule, err := r.store.GetRuleRevision(ctx, run.RuleID, run.Revision)
	if err != nil {
		_ = r.store.FinishRun(ctx, runID, StatusFailed, "revision_missing", "规则版本不可用")
		return err
	}
	prog := Compile(rule.Definition.Steps)

	_ = r.store.MarkRunRunning(ctx, runID)
	pc := startPC
	failed := false
	anyFailed := false

	for pc >= 0 && pc < len(prog) {
		if run.Deadline != nil && r.Now().After(*run.Deadline) {
			_ = r.store.FinishRun(ctx, runID, StatusExpired, "run_budget_exceeded", "运行超过最长时限")
			return nil
		}
		ins := prog[pc]
		if ins.Op == OpJump {
			pc = ins.JumpTarget
			continue
		}
		step := ins.Step

		switch step.Kind {
		case domain.StepBranch:
			verdict := domain.Evaluate(domain.Trigger{Groups: []domain.Group{*step.Condition}},
				domain.EvalContext{Now: r.Now(), SelfDeviceID: run.DeviceID,
					MaxAge: maxAge.Std(), Lookup: r.lookup(ctx)})
			stepID := r.newStep(ctx, runID, pc, "branch", r.stepKey(run, pc, 0))
			detail, _ := json.Marshal(map[string]any{"verdict": verdict.Verdict, "reasons": verdict.Reasons})
			_ = r.store.UpdateStepStatus(ctx, stepID, "succeeded", string(detail))
			switch verdict.Verdict {
			case domain.VerdictTrue:
				pc++
			case domain.VerdictFalse:
				pc = ins.ElseTarget
			default:
				// unknown/error never falls into else silently: the branch stops
				// the run with an explanation the customer can read.
				_ = r.store.FinishRun(ctx, runID, StatusFailed, "branch_undetermined",
					"分支条件无法确定（数据未知或错误），未进入任何分支")
				return nil
			}
			continue

		case domain.StepWait:
			dueAt := r.Now().Add(step.Wait.Std())
			if err := r.store.ScheduleWait(ctx, runID, pc, dueAt); err != nil {
				return err
			}
			stepID := r.newStep(ctx, runID, pc, "wait", r.stepKey(run, pc, 0))
			detail, _ := json.Marshal(map[string]any{"due_at": dueAt})
			_ = r.store.UpdateStepStatus(ctx, stepID, "waiting", string(detail))
			return nil // resumed by the timer worker

		default:
			stepID := r.newStep(ctx, runID, pc, string(step.Kind), r.stepKey(run, pc, 0))
			outcome, err := r.performStep(ctx, run, rule, stepID, step, pc)
			if err != nil {
				return err
			}
			if outcome != stepOK {
				anyFailed = true
				if !step.ContinueOnFailure {
					failed = true
					status := StatusFailed
					_ = r.store.FinishRun(ctx, runID, status, outcome, "步骤执行失败")
					return nil
				}
			}
			pc++
		}
	}

	switch {
	case failed:
		_ = r.store.FinishRun(ctx, runID, StatusFailed, "step_failed", "步骤执行失败")
	case anyFailed:
		_ = r.store.FinishRun(ctx, runID, StatusPartial, "step_partial", "部分步骤失败")
	default:
		_ = r.store.FinishRun(ctx, runID, StatusSucceeded, "", "")
	}
	r.promoteQueued(ctx, run.ScopeKey)
	return nil
}

type stepOutcome = string

const (
	stepOK       stepOutcome = ""
	stepFailed   stepOutcome = "step_failed"
	stepUnknown  stepOutcome = "step_unknown"
	stepRejected stepOutcome = "step_rejected"
)

func (r *Runtime) newStep(ctx context.Context, runID string, pc int, kind, key string) string {
	id := NewID()
	_ = r.store.CreateStep(ctx, &store.Step{
		ID: id, RunID: runID, Idx: pc, Kind: kind, Status: "pending", IdempotencyKey: key,
	})
	return id
}

func (r *Runtime) stepKey(run *store.Run, pc int, attempt int) string {
	// A transport retry of the same logical step keeps the same key; a second
	// legitimate execution of the step in another run gets a different run id.
	return fmt.Sprintf("%s:%d", run.ID, pc)
}

// performStep writes the intent and dispatches it.
func (r *Runtime) performStep(ctx context.Context, run *store.Run, rule *store.Rule, stepID string,
	step domain.Step, pc int) (stepOutcome, error) {

	switch step.Kind {
	case domain.StepAction:
		return r.performAction(ctx, run, stepID, step, pc)
	case domain.StepNotify:
		return r.performNotify(ctx, run, stepID, step, pc)
	case domain.StepAlarm:
		return r.performAlarm(ctx, run, stepID, step, pc)
	case domain.StepActivateScene:
		return r.performScene(ctx, run, stepID, step, pc)
	default:
		_ = r.store.UpdateStepStatus(ctx, stepID, "failed",
			`{"reason":"unsupported_step"}`)
		return stepRejected, nil
	}
}

func (r *Runtime) performAction(ctx context.Context, run *store.Run, stepID string, step domain.Step, pc int) (stepOutcome, error) {
	targets, err := r.store.ListTargets(ctx, run.ID)
	if err != nil {
		return stepFailed, err
	}
	var deviceIDs []string
	for _, t := range targets {
		if t.Status == "selected" {
			deviceIDs = append(deviceIDs, t.DeviceID)
		}
	}
	if len(deviceIDs) == 0 {
		_ = r.store.UpdateStepStatus(ctx, stepID, "skipped", `{"reason":"empty_targets"}`)
		return stepOK, nil
	}

	anyFail, anyUnknown := false, false
	for _, deviceID := range deviceIDs {
		payload, _ := json.Marshal(map[string]any{
			"kind": step.Action, "device_id": deviceID,
			"identifier": step.Identifier, "value": step.Value,
		})
		item := &store.OutboxItem{
			ID: NewID(), TenantID: run.TenantID, RunID: run.ID, StepID: stepID,
			Kind: "command", ScopeKey: run.ScopeKey, Generation: run.Generation,
			IdempotencyKey: fmt.Sprintf("%s:cmd:%s", r.stepKey(run, pc, 0), deviceID),
			Payload:        payload,
		}
		accepted, state, _, msg, err := r.enqueueAndDispatch(ctx, item)
		if err != nil {
			return stepFailed, err
		}
		_ = r.store.UpdateTargetStatus(ctx, run.ID, deviceID, state, "", msg)
		switch {
		case !accepted && state == "failed":
			anyFail = true
		case state == "unknown":
			anyUnknown = true
		}
	}
	switch {
	case anyUnknown:
		_ = r.store.UpdateStepStatus(ctx, stepID, "unknown", `{"reason":"device_result_unknown"}`)
		return stepUnknown, nil
	case anyFail:
		_ = r.store.UpdateStepStatus(ctx, stepID, "failed", `{"reason":"command_rejected"}`)
		return stepFailed, nil
	default:
		_ = r.store.UpdateStepStatus(ctx, stepID, "accepted", `{"reason":"accepted_by_command_owner"}`)
		return stepOK, nil
	}
}

func (r *Runtime) performNotify(ctx context.Context, run *store.Run, stepID string, step domain.Step, pc int) (stepOutcome, error) {
	payload, _ := json.Marshal(map[string]any{
		"policy_id": step.NotificationPolicyID,
		"subject":   "自动化通知",
		"text":      step.Value,
	})
	item := &store.OutboxItem{
		ID: NewID(), TenantID: run.TenantID, RunID: run.ID, StepID: stepID,
		Kind: "notification", ScopeKey: run.ScopeKey, Generation: run.Generation,
		IdempotencyKey: r.stepKey(run, pc, 0) + ":notify",
		Payload:        payload,
	}
	accepted, state, _, msg, err := r.enqueueAndDispatch(ctx, item)
	if err != nil {
		return stepFailed, err
	}
	if !accepted && state == "failed" {
		_ = r.store.UpdateStepStatus(ctx, stepID, "failed", `{"reason":"`+msg+`"}`)
		return stepFailed, nil
	}
	_ = r.store.UpdateStepStatus(ctx, stepID, "accepted", `{"reason":"accepted_by_notification_core"}`)
	return stepOK, nil
}

func (r *Runtime) performScene(ctx context.Context, run *store.Run, stepID string, step domain.Step, pc int) (stepOutcome, error) {
	payload, _ := json.Marshal(map[string]any{"scene_id": step.Value})
	item := &store.OutboxItem{
		ID: NewID(), TenantID: run.TenantID, RunID: run.ID, StepID: stepID,
		Kind: "scene", ScopeKey: run.ScopeKey, Generation: run.Generation,
		IdempotencyKey: r.stepKey(run, pc, 0) + ":scene",
		Payload:        payload,
	}
	accepted, state, _, msg, err := r.enqueueAndDispatch(ctx, item)
	if err != nil {
		return stepFailed, err
	}
	if !accepted && state == "failed" {
		_ = r.store.UpdateStepStatus(ctx, stepID, "failed", `{"reason":"`+msg+`"}`)
		return stepFailed, nil
	}
	_ = r.store.UpdateStepStatus(ctx, stepID, "accepted", `{"reason":"scene_accepted"}`)
	return stepOK, nil
}

// enqueueAndDispatch commits the intent first, then performs the network call.
// If the process dies between the two, the intent is still claimable by the
// dispatcher: no silent loss, and no double action because the idempotency key
// is unique.
func (r *Runtime) enqueueAndDispatch(ctx context.Context, item *store.OutboxItem) (bool, string, string, string, error) {
	tx, err := r.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, "failed", "", err.Error(), err
	}
	if err := r.store.EnqueueOutbox(ctx, tx, item); err != nil {
		_ = tx.Rollback()
		return false, "failed", "", err.Error(), err
	}
	if err := tx.Commit(); err != nil {
		return false, "failed", "", err.Error(), err
	}
	return r.dispatchOne(ctx, item)
}

func (r *Runtime) promoteQueued(ctx context.Context, scope string) {
	for {
		next, err := r.store.NextQueuedRun(ctx, scope)
		if err != nil || next == "" {
			return
		}
		if err := r.store.MarkRunRunning(ctx, next); err != nil {
			return
		}
		if err := r.executeRun(ctx, next, 0); err != nil {
			return
		}
	}
}
