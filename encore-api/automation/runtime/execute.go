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

	deadlineReason := ""
	deadline := now.Add(def.Budget.MaxRunDuration.Std())
	if def.Mode == domain.ModeParallel && def.Parallel != nil && def.Parallel.TTL > 0 {
		// A parallel slot is a shared resource: the run may not hold it longer
		// than the rule allows, even if its own step budget is larger.
		if ttl := now.Add(def.Parallel.TTL.Std()); ttl.Before(deadline) {
			deadline = ttl
			deadlineReason = "parallel_ttl_exceeded"
		}
	}
	if def.Mode == domain.ModeQueued && def.Queue != nil && def.Queue.TTL > 0 {
		// A queued run may wait up to the queue TTL and then run for the full
		// step budget. The combined bound is only a safety net for a queue that
		// never drains; the wait itself is checked when the run is promoted,
		// because "waited too long" is a different outcome from "ran too long".
		deadline = now.Add(def.Queue.TTL.Std()).Add(def.Budget.MaxRunDuration.Std())
		deadlineReason = "queue_ttl_exceeded"
	}

	snapshot, _ := json.Marshal(map[string]any{
		"rule_id":         rule.ID,
		"revision":        rule.Revision,
		"definition_hash": rule.DefinitionHash,
		"device_id":       deviceID,
		"targets":         len(targets),
		"generation":      generation,
		"verdict":         res.Verdict,
		"mode":            def.Mode,
		"manual":          c.Manual,
		// Which clock ran out, so the expiry reason is readable instead of
		// always claiming the step budget was exceeded.
		"deadline_reason": deadlineReason,
	})

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
	if _, err := r.executeRun(ctx, run.ID); err != nil {
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

// executeRun walks the compiled program from the run's persisted checkpoint.
//
// It returns whether this worker actually got the run. `false` means another
// worker holds a live lease: walking the program anyway would repeat side
// effects, so doing nothing is the only correct answer.
func (r *Runtime) executeRun(ctx context.Context, runID string) (bool, error) {
	run, err := r.store.GetRunByID(ctx, runID)
	if err != nil {
		return false, err
	}
	return r.executeRunAt(ctx, run, run.CursorPC)
}

// ResumeRun executes a run from its checkpoint, reporting whether this worker got
// it. It is the manual form of the recovery sweep: an operator (or the CLI) can
// nudge an interrupted run without waiting for the next pass, and the lease keeps
// it safe to call from anywhere.
func (r *Runtime) ResumeRun(ctx context.Context, runID string) (bool, error) {
	return r.executeRun(ctx, runID)
}

// RunNow starts a run without evaluating a trigger. It is what the old scene
// page's "激活" button did: the user is the trigger.
//
// The run-mode contract still applies, because "the user pressed it twice" is
// exactly the re-entry case single/restart/queued/parallel exist for. The manual
// flag is recorded in the snapshot so the detail view can tell this apart from a
// condition-driven run.
func (r *Runtime) RunNow(ctx context.Context, rule *store.Rule, deviceID string) (*store.Run, error) {
	if rule.Kind == domain.KindAlarm {
		return nil, errors.New("告警策略没有可执行的动作；手动执行只适用于场景和自动化")
	}
	res := domain.Result{
		Verdict: domain.VerdictTrue,
		Reasons: []domain.Reason{{
			Code:    "manual_run",
			Message: "用户手动执行，未经条件判定",
		}},
	}
	subject := deviceID
	if subject == "" && rule.Definition.Scope == domain.ScopeDevice {
		// A device-scoped rule needs a subject to isolate its scope. Fall back to
		// the single target when the caller did not name one.
		if ids := rule.Definition.Targets.DeviceIDs; len(ids) == 1 {
			subject = ids[0]
		} else {
			return nil, errors.New("该规则按设备隔离，手动执行必须指定 device_id")
		}
	}
	run, err := r.acceptRun(ctx, rule, subject, "", cause{Manual: true}, res, r.Now(), 0)
	if err != nil {
		return run, err
	}
	// acceptRun returns the row it inserted, which still says "accepted" because
	// the program runs after the insert. Reload so the caller sees the real
	// outcome instead of a status that was true for a few microseconds.
	if run == nil || run.ID == "" {
		return run, nil
	}
	if fresh, gerr := r.store.GetRunByID(ctx, run.ID); gerr == nil {
		return fresh, nil
	}
	return run, nil
}

// executeRunFromPC resumes at an explicit program counter. It is used by the
// wait timer, whose continuation point is the recorded counter, not the
// checkpoint (the checkpoint still points at the wait step itself).
func (r *Runtime) executeRunFromPC(ctx context.Context, runID string, pc int) (bool, error) {
	run, err := r.store.GetRunByID(ctx, runID)
	if err != nil {
		return false, err
	}
	return r.executeRunAt(ctx, run, pc)
}

func (r *Runtime) executeRunAt(ctx context.Context, run *store.Run, startPC int) (bool, error) {
	// Branch conditions evaluate against the revision the run was admitted on,
	// so a concurrent edit cannot change what a half-finished program means.
	if run.Status != StatusAccepted && run.Status != StatusRunning {
		return false, nil
	}
	now := r.Now()
	claimed, err := r.store.ClaimRun(ctx, run.ID, r.workerID, now, now.Add(store.RunLeaseDuration))
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}
	// The lease is released on every exit path, including a panic in a port. A
	// leftover lease would only delay the next resume by one lease period, but
	// releasing eagerly keeps "who is working on this" accurate.
	defer func() { _ = r.store.ReleaseRun(ctx, run.ID, r.workerID) }()

	rule, err := r.store.GetRuleRevision(ctx, run.RuleID, run.Revision)
	if err != nil {
		_ = r.store.FinishRun(ctx, run.ID, StatusFailed, "revision_missing", "规则版本不可用")
		return true, err
	}
	prog := Compile(rule.Definition.Steps)

	pc := startPC
	if pc < 0 {
		pc = 0
	}
	if pc > len(prog) {
		pc = len(prog)
	}
	failed := false
	anyFailed := false

	for pc >= 0 && pc < len(prog) {
		if run.Deadline != nil && r.Now().After(*run.Deadline) {
			_ = r.store.FinishRun(ctx, run.ID, StatusExpired, "run_budget_exceeded", "运行超过最长时限")
			return true, nil
		}
		// Checkpoint before the step runs. If the process dies inside the step,
		// the resume starts here and the replay is absorbed by the stable step
		// and action identity instead of producing a second action.
		if err := r.store.SetRunCursor(ctx, run.ID, pc); err != nil {
			return true, err
		}
		ins := prog[pc]
		if ins.Op == OpJump {
			pc = ins.JumpTarget
			continue
		}
		step := ins.Step

		switch step.Kind {
		case domain.StepBranch:
			stepID, attempt, err := r.ensureStep(ctx, run, pc, "branch")
			if err != nil {
				return true, err
			}
			verdict := domain.Evaluate(domain.Trigger{Groups: []domain.Group{*step.Condition}},
				r.evalContext(ctx, &rule.Definition, run.DeviceID, nil, r.Now()))
			detail, _ := json.Marshal(map[string]any{
				"verdict": verdict.Verdict, "reasons": verdict.Reasons, "attempt": attempt,
			})
			if err := r.store.UpdateStepStatus(ctx, stepID, "succeeded", string(detail)); err != nil {
				return true, err
			}
			switch verdict.Verdict {
			case domain.VerdictTrue:
				pc++
			case domain.VerdictFalse:
				pc = ins.ElseTarget
			default:
				// unknown/error never falls into else silently: the branch stops
				// the run with an explanation the customer can read.
				_ = r.store.FinishRun(ctx, run.ID, StatusFailed, "branch_undetermined",
					"分支条件无法确定（数据未知或错误），未进入任何分支")
				return true, nil
			}
			continue

		case domain.StepWait:
			stepID, attempt, err := r.ensureStep(ctx, run, pc, "wait")
			if err != nil {
				return true, err
			}
			dueAt := r.Now().Add(step.Wait.Std())
			if err := r.store.ScheduleWait(ctx, run.ID, pc, dueAt); err != nil {
				return true, err
			}
			detail, _ := json.Marshal(map[string]any{"due_at": dueAt, "attempt": attempt})
			if err := r.store.UpdateStepStatus(ctx, stepID, "waiting", string(detail)); err != nil {
				return true, err
			}
			return true, nil // resumed by the timer worker

		default:
			stepID, attempt, err := r.ensureStep(ctx, run, pc, string(step.Kind))
			if err != nil {
				return true, err
			}
			outcome, err := r.performStep(ctx, run, rule, stepID, step, pc, attempt)
			if err != nil {
				// A step that cannot even record its intent must stop the run:
				// continuing would dispatch actions with no source record.
				_ = r.store.FinishRun(ctx, run.ID, StatusFailed, "step_error", err.Error())
				return true, err
			}
			if outcome == stepSuperseded {
				// The run was cancelled while this step was being dispatched.
				// Returning without FinishRun keeps the reason "superseded"
				// instead of rewriting it into success or failure.
				return true, nil
			}
			if outcome != stepOK {
				anyFailed = true
				if !step.ContinueOnFailure {
					_ = r.store.FinishRun(ctx, run.ID, StatusFailed, outcome,
						stepFailureMessage(outcome, step))
					return true, nil
				}
			}
			pc++
		}
	}

	switch {
	case failed:
		_ = r.store.FinishRun(ctx, run.ID, StatusFailed, "step_failed", "步骤执行失败")
	case anyFailed:
		_ = r.store.FinishRun(ctx, run.ID, StatusPartial, "step_partial", "部分步骤失败")
	default:
		_ = r.store.FinishRun(ctx, run.ID, StatusSucceeded, "", "")
	}
	r.promoteQueued(ctx, run.ScopeKey)
	return true, nil
}

type stepOutcome = string

const (
	stepOK          stepOutcome = ""
	stepFailed      stepOutcome = "step_failed"
	stepUnknown     stepOutcome = "step_unknown"
	stepRejected    stepOutcome = "step_rejected"
	stepUnsupported stepOutcome = "unsupported_step"
	// stepSuperseded means the command owner refused the action because a newer
	// trigger in the same scope already won. The run is already cancelled by the
	// time this is returned, so the loop must stop without finishing it again.
	stepSuperseded stepOutcome = "superseded"
)

// stepFailureMessage turns an outcome into a sentence a customer can act on.
// "步骤执行失败" alone tells nobody whether to fix the device or the rule.
func stepFailureMessage(outcome stepOutcome, step domain.Step) string {
	switch outcome {
	case stepUnsupported:
		return "规则包含未实现的步骤类型 " + string(step.Kind) + "；发布时应当被拒绝"
	case stepUnknown:
		return "步骤结果未知（无法确认对端是否已受理），未按成功处理"
	case stepRejected:
		return "步骤被对端拒绝"
	default:
		return "步骤执行失败"
	}
}

// ensureStep records the logical step identity and returns the authoritative row
// id. The id is not generated by the caller because a re-entry hits the existing
// row, and a caller-invented id would silently update nothing from then on.
func (r *Runtime) ensureStep(ctx context.Context, run *store.Run, pc int, kind string) (string, int, error) {
	return r.store.UpsertStep(ctx, &store.Step{
		ID: NewID(), RunID: run.ID, Idx: pc, Kind: kind,
		Status: "pending", IdempotencyKey: r.stepKey(run, pc),
	})
}

// stepKey is the logical step identity inside a run.
//
// It deliberately has no attempt component: a transport retry of the same step
// must reuse the same identity, otherwise the far end cannot dedupe it. A second
// *legitimate* execution is a different run and therefore gets a different run
// id — that is the occurrence boundary, and it is why two runs of the same rule
// are both accepted instead of being folded into one.
func (r *Runtime) stepKey(run *store.Run, pc int) string {
	return fmt.Sprintf("%s:%d", run.ID, pc)
}

// performStep writes the intent and dispatches it.
func (r *Runtime) performStep(ctx context.Context, run *store.Run, rule *store.Rule, stepID string,
	step domain.Step, pc, attempt int) (stepOutcome, error) {

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
		// Validation rejects unknown kinds at publish time, so reaching here
		// means the stored revision predates the check or was written by hand.
		// Either way it must stop the run with a readable reason instead of
		// being treated as a successful no-op.
		detail, _ := json.Marshal(map[string]any{
			"reason": "unsupported_step", "kind": string(step.Kind), "attempt": attempt,
		})
		if err := r.store.UpdateStepStatus(ctx, stepID, "failed", string(detail)); err != nil {
			return stepFailed, err
		}
		return stepUnsupported, nil
	}
}

func (r *Runtime) performAction(ctx context.Context, run *store.Run, stepID string, step domain.Step, pc int) (stepOutcome, error) {
	targets, err := r.store.ListTargets(ctx, run.ID)
	if err != nil {
		return stepFailed, err
	}
	var deviceIDs []string
	for _, t := range targets {
		// Selected is the frozen snapshot; Status has since been overwritten by
		// the dispatch result of an earlier attempt. Filtering on Status would
		// make a resumed step believe it owns no devices.
		if t.Selected {
			deviceIDs = append(deviceIDs, t.DeviceID)
		}
	}
	if len(deviceIDs) == 0 {
		_ = r.store.UpdateStepStatus(ctx, stepID, "skipped", `{"reason":"empty_targets"}`)
		return stepOK, nil
	}

	anyFail, anyUnknown, anySuperseded := false, false, false
	for _, deviceID := range deviceIDs {
		payload, _ := json.Marshal(map[string]any{
			"kind": step.Action, "device_id": deviceID,
			"identifier": step.Identifier, "value": step.Value,
		})
		item := &store.OutboxItem{
			ID: NewID(), TenantID: run.TenantID, RunID: run.ID, StepID: stepID,
			Kind: "command", ScopeKey: run.ScopeKey, Generation: run.Generation,
			IdempotencyKey: fmt.Sprintf("%s:cmd:%s", r.stepKey(run, pc), deviceID),
			Payload:        payload,
		}
		accepted, state, _, msg, err := r.enqueueAndDispatch(ctx, item)
		if err != nil {
			return stepFailed, err
		}
		_ = r.store.UpdateTargetStatus(ctx, run.ID, deviceID, state, "", msg)
		switch {
		case accepted:
			// Nothing to record here: the target row already carries "accepted".
		case state == "superseded":
			anySuperseded = true
		case state == "unknown", state == "in_flight":
			// in_flight is not a decision: an identical attempt is still being
			// processed, so the honest step state is "not confirmed yet". The
			// outbox row stays retryable and will complete the step later.
			anyUnknown = true
		default:
			// failed or cancelled — either way the device did not take it.
			anyFail = true
		}
	}
	switch {
	case anySuperseded:
		if err := r.store.UpdateStepStatus(ctx, stepID, "cancelled",
			`{"reason":"superseded_by_newer_generation"}`); err != nil {
			return stepFailed, err
		}
		return stepSuperseded, nil
	case anyUnknown:
		if err := r.store.UpdateStepStatus(ctx, stepID, "unknown",
			`{"reason":"device_result_unknown"}`); err != nil {
			return stepFailed, err
		}
		return stepUnknown, nil
	case anyFail:
		if err := r.store.UpdateStepStatus(ctx, stepID, "failed",
			`{"reason":"command_rejected"}`); err != nil {
			return stepFailed, err
		}
		return stepFailed, nil
	default:
		if err := r.store.UpdateStepStatus(ctx, stepID, "accepted",
			`{"reason":"accepted_by_command_owner"}`); err != nil {
			return stepFailed, err
		}
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
		IdempotencyKey: r.stepKey(run, pc) + ":notify",
		Payload:        payload,
	}
	accepted, state, _, msg, err := r.enqueueAndDispatch(ctx, item)
	if err != nil {
		return stepFailed, err
	}
	if accepted {
		if err := r.store.UpdateStepStatus(ctx, stepID, "accepted",
			`{"reason":"accepted_by_notification_core"}`); err != nil {
			return stepFailed, err
		}
		return stepOK, nil
	}
	return r.recordStepDispatchFailure(ctx, stepID, state, msg)
}

func (r *Runtime) performScene(ctx context.Context, run *store.Run, stepID string, step domain.Step, pc int) (stepOutcome, error) {
	payload, _ := json.Marshal(map[string]any{"scene_id": step.Value})
	item := &store.OutboxItem{
		ID: NewID(), TenantID: run.TenantID, RunID: run.ID, StepID: stepID,
		Kind: "scene", ScopeKey: run.ScopeKey, Generation: run.Generation,
		IdempotencyKey: r.stepKey(run, pc) + ":scene",
		Payload:        payload,
	}
	accepted, state, _, msg, err := r.enqueueAndDispatch(ctx, item)
	if err != nil {
		return stepFailed, err
	}
	if accepted {
		if err := r.store.UpdateStepStatus(ctx, stepID, "accepted",
			`{"reason":"scene_accepted"}`); err != nil {
			return stepFailed, err
		}
		return stepOK, nil
	}
	return r.recordStepDispatchFailure(ctx, stepID, state, msg)
}

// recordStepDispatchFailure keeps "unknown" and "refused" apart on the step row.
// Folding them together is how a rejected command ends up looking like a sent
// one, which is the exact failure the four-value verdict exists to prevent.
func (r *Runtime) recordStepDispatchFailure(ctx context.Context, stepID, state, msg string) (stepOutcome, error) {
	detail, _ := json.Marshal(map[string]any{"reason": state, "message": msg})
	status, outcome := "failed", stepFailed
	switch state {
	case "unknown", "in_flight":
		status, outcome = "unknown", stepUnknown
	case "superseded":
		// Not a failure of this run: a newer run in the same scope won. Calling
		// it failed would make an expected race look like a defect.
		status, outcome = "cancelled", stepSuperseded
	}
	if err := r.store.UpdateStepStatus(ctx, stepID, status, string(detail)); err != nil {
		return stepFailed, err
	}
	return outcome, nil
}

// enqueueAndDispatch commits the intent first, then performs the network call.
//
// The split is what makes a crash between the two harmless: the intent is still
// claimable by the dispatcher, so nothing is lost, and the unique idempotency key
// means nothing is doubled.
//
// A duplicate insert means this exact logical action already has a row. That
// happens on a resume after a crash inside the step. The existing row is reused:
//   - already accepted → the step is reported as accepted again, with no second
//     call, so a retry can never be counted as a second acceptance;
//   - not yet confirmed → the transport is retried with the SAME identity, so the
//     far end can still dedupe it;
//   - cancelled → reported as cancelled, never silently re-sent.
func (r *Runtime) enqueueAndDispatch(ctx context.Context, item *store.OutboxItem) (bool, string, string, string, error) {
	tx, err := r.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, "failed", "", err.Error(), err
	}
	inserted, err := r.store.EnqueueOutbox(ctx, tx, item)
	if err != nil {
		_ = tx.Rollback()
		return false, "failed", "", err.Error(), err
	}
	if err := tx.Commit(); err != nil {
		return false, "failed", "", err.Error(), err
	}
	if inserted {
		return r.dispatchOne(ctx, item)
	}

	existing, err := r.store.GetOutboxByKey(ctx, item.IdempotencyKey)
	if err != nil {
		return false, "unknown", "", "重复意图回读失败: " + err.Error(), err
	}
	switch existing.State {
	case "accepted":
		return true, "accepted", existing.AcceptedRef, existing.LastError, nil
	case "cancelled":
		return false, "cancelled", "", "运行已取消，意图未派发", nil
	default:
		return r.dispatchOne(ctx, existing)
	}
}

// promoteQueued starts the next waiting run of a scope.
//
// Two checks happen here rather than at admission time, because both can change
// while the run waits:
//
//   - the queue TTL: a trigger that waited longer than the rule allows must not
//     fire late. Running it anyway is how a light turns on for a person who left
//     the corridor ten minutes ago.
//   - the rule and its revision: a rule disabled or deleted while its trigger
//     waited must not still act on devices.
//
// A run that fails either check is closed with a readable reason and the queue
// moves on; it never stays "queued" forever and never blocks the ones behind it.
func (r *Runtime) promoteQueued(ctx context.Context, scope string) {
	for {
		next, err := r.store.NextQueuedRun(ctx, scope)
		if err != nil || next == "" {
			return
		}
		run, err := r.store.GetRunByID(ctx, next)
		if err != nil {
			return
		}
		rule, err := r.store.GetRuleRevision(ctx, run.RuleID, run.Revision)
		if err != nil {
			_ = r.store.FinishRun(ctx, next, StatusCancelled, "rule_unavailable_before_start",
				"排队期间规则版本已不可用，未执行")
			_ = r.store.CancelRunOutbox(ctx, next)
			continue
		}
		if !rule.Enabled {
			_ = r.store.FinishRun(ctx, next, StatusCancelled, "rule_disabled_before_start",
				"排队期间规则已被停用，未执行")
			_ = r.store.CancelRunOutbox(ctx, next)
			continue
		}
		if ttl := rule.Definition.Queue; ttl != nil && ttl.TTL > 0 {
			waited := r.Now().Sub(run.StartedAt)
			if waited > ttl.TTL.Std() {
				_ = r.store.FinishRun(ctx, next, StatusExpired, "queue_ttl_exceeded",
					fmt.Sprintf("排队等待 %s 超过上限 %s，未执行", waited.Truncate(time.Second), ttl.TTL.Std()))
				_ = r.store.CancelRunOutbox(ctx, next)
				continue
			}
		}
		// The run's own clock starts now, not when it was queued.
		deadline := r.Now().Add(rule.Definition.Budget.MaxRunDuration.Std())
		if err := r.store.StartQueuedRun(ctx, next, deadline); err != nil {
			return
		}
		if _, err := r.executeRun(ctx, next); err != nil {
			return
		}
	}
}
