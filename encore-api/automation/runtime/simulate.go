package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"encore.app/automation/domain"
)

// SimSample is one synthetic observation for a side-effect-free simulation.
type SimSample struct {
	At       string `json:"at"` // "0s", "60s" or RFC3339
	DeviceID string `json:"device_id,omitempty"`
	Signal   string `json:"signal"`
	Key      string `json:"key"`
	Value    any    `json:"value"`
	Unit     string `json:"unit,omitempty"`
	Quality  string `json:"quality,omitempty"`
}

// SimStep is one line of the simulation trace.
type SimStep struct {
	At      string          `json:"at"`
	Kind    string          `json:"kind"` // sample | timer | pending | verdict | action
	Verdict string          `json:"verdict,omitempty"`
	Detail  string          `json:"detail,omitempty"`
	Reasons []domain.Reason `json:"reasons,omitempty"`
	Actions []string        `json:"actions,omitempty"`
}

// SimAction is one step the simulation would take, with the outcome it would
// have. "would_execute" means the adapter would have been called; "skipped"
// means a branch did not go there; "blocked" means a guard or the budget stopped
// it. None of them actually calls anything.
type SimAction struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Detail  string `json:"detail"`
	Outcome string `json:"outcome"`
}

// SimResult is the whole simulation outcome.
type SimResult struct {
	RuleID         string      `json:"rule_id"`
	Revision       int         `json:"revision"`
	DefinitionHash string      `json:"definition_hash"`
	Steps          []SimStep   `json:"steps"`
	Actions        []SimAction `json:"actions,omitempty"`
	WouldRun       int         `json:"would_run"`
	SideEffects    int         `json:"side_effects"` // always 0: proof of no side effect
	// EndsAt is the virtual clock when the program finished, which is not the
	// same as the last sample once a rule waits.
	EndsAt string   `json:"ends_at,omitempty"`
	Notes  []string `json:"notes"`
}

// Simulate replays samples against the same evaluator the runtime uses, with a
// virtual clock and an in-memory state map. It never calls a command, an alarm
// or the notification core, so SideEffects is always zero.
func (r *Runtime) Simulate(ctx context.Context, tenantID, ruleID string, samples []SimSample) (*SimResult, error) {
	rule, err := r.store.GetRule(ctx, tenantID, ruleID)
	if err != nil {
		return nil, err
	}
	// A rule with parameters does not have one body: it has one body per device,
	// because the binding supplies the values. Resolving them with bindRule is
	// not a convenience — it is the only way the simulation can agree with the
	// real evaluation, since a device whose threshold is overridden would
	// otherwise be simulated against the template default and fire at the wrong
	// moment.
	type body struct {
		def  domain.Definition
		prog []Instruction
	}
	bodies := map[string]*body{}
	bodyFor := func(deviceID string) (*body, error) {
		if b, ok := bodies[deviceID]; ok {
			return b, nil
		}
		bound, berr := bindRule(rule, r.bindingsFor(ctx, tenantID, deviceID)[rule.ID])
		if berr != nil {
			return nil, berr
		}
		b := &body{def: bound.Definition, prog: Compile(bound.Definition.Steps)}
		bodies[deviceID] = b
		return b, nil
	}

	base := r.Now()
	parsed := make([]struct {
		at time.Time
		s  SimSample
	}, 0, len(samples))
	for _, s := range samples {
		at, err := parseSimTime(base, s.At)
		if err != nil {
			return nil, fmt.Errorf("sample %q: %w", s.At, err)
		}
		if s.DeviceID == "" {
			s.DeviceID = defSelfDevice(samples)
		}
		if s.Signal == "" {
			s.Signal = string(domain.SignalTelemetry)
		}
		parsed = append(parsed, struct {
			at time.Time
			s  SimSample
		}{at, s})
	}
	sort.SliceStable(parsed, func(i, j int) bool { return parsed[i].at.Before(parsed[j].at) })

	state := map[string]domain.Sample{}
	lookup := func(deviceID string, signal domain.SignalKind, key string) (domain.Sample, bool, error) {
		s, ok := state[stateKey(deviceID, signal, key)]
		return s, ok, nil
	}

	res := &SimResult{
		RuleID: rule.ID, Revision: rule.Revision, DefinitionHash: rule.DefinitionHash,
		Notes: []string{"模拟使用同一评估器与虚拟时钟，不会调用设备命令、正式告警或通知"},
	}
	if rule.Definition.HasParams() {
		res.Notes = append(res.Notes,
			"参数已按设备绑定值填充（与真实执行同一份 Materialize 结果），不是模板默认值")
	}

	type pendingSim struct {
		start    time.Time
		deadline time.Time
		device   string
		body     *body
		lastSeen time.Time
		recovery bool
	}
	var pend *pendingSim

	emitTimerIfDue := func(until time.Time) {
		if pend == nil {
			return
		}
		if pend.deadline.After(until) {
			return
		}
		now := pend.deadline
		def := pend.body.def
		// Freshness at the moment the timer fires.
		fresh := checkFreshness(domain.Sample{ObservedAt: pend.lastSeen}, now, def.Freshness)
		if !fresh.OK {
			res.Steps = append(res.Steps, SimStep{
				At: rel(base, now), Kind: "timer", Verdict: "unknown",
				Detail: "计时到点但证据已过期，判定为不确定，不触发",
			})
			pend = nil
			return
		}
		trigger := def.Trigger
		if pend.recovery && def.Recovery != nil {
			trigger = *def.Recovery
		}
		verdict := domain.Evaluate(trigger, domain.EvalContext{
			Now: now, SelfDeviceID: pend.device, Lookup: lookup,
			MaxAge: def.Freshness.MaxAge.Std(), Mode: def.Freshness.Mode,
			Heartbeat: def.Freshness.Heartbeat.Std(),
		})
		switch verdict.Verdict {
		case domain.VerdictTrue:
			if pend.recovery {
				res.Steps = append(res.Steps, SimStep{
					At: rel(base, now), Kind: "timer", Verdict: "clear",
					Detail: "恢复条件已持续满足，将恢复告警",
				})
			} else {
				res.WouldRun++
				actions, trace, ends := simulateProgram(def, pend.body.prog, pend.device, now, lookup, base)
				res.Actions = append(res.Actions, actions...)
				res.Steps = append(res.Steps, trace...)
				res.Steps = append(res.Steps, SimStep{
					At: rel(base, now), Kind: "action", Verdict: "true",
					Detail:  fmt.Sprintf("持续条件成立，将执行 %d 个步骤", countWould(actions)),
					Actions: describeActionsOf(actions),
				})
				res.EndsAt = rel(base, ends)
			}
		default:
			res.Steps = append(res.Steps, SimStep{
				At: rel(base, now), Kind: "timer", Verdict: string(verdict.Verdict),
				Detail: "到点时条件不再成立或无法判定，取消等待",
			})
		}
		pend = nil
	}

	for _, item := range parsed {
		emitTimerIfDue(item.at)

		dev := item.s.DeviceID
		b, berr := bodyFor(dev)
		if berr != nil {
			return nil, berr
		}
		def := b.def
		sig := domain.SignalKind(item.s.Signal)
		q := domain.Quality(item.s.Quality)
		if q == "" {
			q = domain.QualityGood
		}
		sample := domain.Sample{
			DeviceID: dev, Signal: sig, Key: item.s.Key, Value: item.s.Value,
			Unit: item.s.Unit, Quality: q, ObservedAt: item.at, ReceivedAt: item.at,
		}
		state[stateKey(dev, sig, item.s.Key)] = sample
		res.Steps = append(res.Steps, SimStep{
			At: rel(base, item.at), Kind: "sample",
			Detail: fmt.Sprintf("%s %s=%v", dev, item.s.Key, item.s.Value),
		})

		verdict := domain.Evaluate(def.Trigger, domain.EvalContext{
			Now: item.at, SelfDeviceID: dev, Trigger: &sample, Lookup: lookup,
			MaxAge: def.Freshness.MaxAge.Std(), Mode: def.Freshness.Mode,
			Heartbeat: def.Freshness.Heartbeat.Std(),
		})
		res.Steps = append(res.Steps, SimStep{
			At: rel(base, item.at), Kind: "verdict", Verdict: string(verdict.Verdict),
			Reasons: verdict.Reasons,
		})

		switch verdict.Verdict {
		case domain.VerdictTrue:
			if def.Trigger.For.Std() <= 0 {
				if def.Kind == domain.KindAlarm {
					res.Steps = append(res.Steps, SimStep{
						At: rel(base, item.at), Kind: "action", Verdict: "true",
						Detail: "将产生/维持一条活动告警",
					})
				} else {
					res.WouldRun++
					actions, trace, ends := simulateProgram(def, b.prog, dev, item.at, lookup, base)
					res.Actions = append(res.Actions, actions...)
					res.Steps = append(res.Steps, trace...)
					res.Steps = append(res.Steps, SimStep{
						At: rel(base, item.at), Kind: "action", Verdict: "true",
						Detail:  fmt.Sprintf("立即满足（for=0 保持旧触发语义），将执行 %d 个步骤", countWould(actions)),
						Actions: describeActionsOf(actions),
					})
					res.EndsAt = rel(base, ends)
				}
			} else if pend == nil {
				pend = &pendingSim{
					start: item.at, deadline: item.at.Add(def.Trigger.For.Std()),
					device: dev, body: b, lastSeen: item.at,
				}
				res.Steps = append(res.Steps, SimStep{
					At: rel(base, item.at), Kind: "pending", Verdict: "true",
					Detail: "开始持续计时，到点时间 " + rel(base, pend.deadline),
				})
			} else {
				pend.lastSeen = item.at
			}
		case domain.VerdictFalse:
			if pend != nil && !pend.recovery {
				res.Steps = append(res.Steps, SimStep{
					At: rel(base, item.at), Kind: "pending", Verdict: "cancelled",
					Detail: "条件不成立，取消等待，不触发",
				})
				pend = nil
			}
		default:
			if pend != nil {
				res.Steps = append(res.Steps, SimStep{
					At: rel(base, item.at), Kind: "pending", Verdict: string(verdict.Verdict),
					Detail: "数据未知或错误：保持等待但不判定成立，不自动恢复",
				})
			}
		}
	}
	if pend != nil {
		emitTimerIfDue(pend.deadline)
	}
	return res, nil
}

func stateKey(deviceID string, signal domain.SignalKind, key string) string {
	return deviceID + "|" + string(signal) + "|" + key
}

func parseSimTime(base time.Time, s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("missing time")
	}
	if d, err := time.ParseDuration(s); err == nil {
		return base.Add(d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if n, err := time.Parse("15:04:05", s); err == nil {
		return time.Date(base.Year(), base.Month(), base.Day(), n.Hour(), n.Minute(), n.Second(), 0, base.Location()), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q", s)
}

func rel(base, t time.Time) string {
	return t.Sub(base).Round(time.Second).String()
}

func defSelfDevice(samples []SimSample) string {
	for _, s := range samples {
		if s.DeviceID != "" {
			return s.DeviceID
		}
	}
	return ""
}

// simulateProgram walks the compiled body exactly the way the executor does:
// the same program counter, the same branch targets, the same guard rule, the
// same budget. The only difference is that a step which would call an adapter
// records "would_execute" instead of calling it.
//
// Writing this as a second interpreter would be the mistake: two interpreters
// disagree eventually, and then the simulation is confidently wrong.
func simulateProgram(def domain.Definition, prog []Instruction, deviceID string,
	start time.Time, lookup func(string, domain.SignalKind, string) (domain.Sample, bool, error),
	base time.Time) ([]SimAction, []SimStep, time.Time) {

	var actions []SimAction
	var trace []SimStep
	now := start
	steps := 0
	maxSteps := def.Budget.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 32
	}
	maxDur := def.Budget.MaxRunDuration.Std()

	ctxAt := func(t time.Time) domain.EvalContext {
		return domain.EvalContext{
			Now: t, SelfDeviceID: deviceID, Lookup: lookup,
			MaxAge: def.Freshness.MaxAge.Std(), Mode: def.Freshness.Mode,
			Heartbeat: def.Freshness.Heartbeat.Std(),
		}
	}

	pc := 0
	for pc >= 0 && pc < len(prog) {
		if steps >= maxSteps {
			trace = append(trace, SimStep{At: rel(base, now), Kind: "budget",
				Detail: fmt.Sprintf("步骤预算 %d 用尽，后续步骤不会执行", maxSteps)})
			break
		}
		if maxDur > 0 && now.Sub(start) > maxDur {
			trace = append(trace, SimStep{At: rel(base, now), Kind: "budget",
				Detail: "运行时长预算用尽，后续步骤不会执行"})
			break
		}
		ins := prog[pc]
		if ins.Op == OpJump {
			pc = ins.JumpTarget
			continue
		}
		steps++
		s := ins.Step

		if s.Kind == domain.StepBranch {
			verdict := domain.VerdictFalse
			if s.Condition != nil {
				// Same shape the executor uses for a branch condition.
				verdict = domain.Evaluate(domain.Trigger{Groups: []domain.Group{*s.Condition}},
					ctxAt(now)).Verdict
			}
			taken := verdict == domain.VerdictTrue
			detail := "条件不成立，走 else 分支"
			if taken {
				detail = "条件成立，走 then 分支"
			}
			if verdict != domain.VerdictTrue && verdict != domain.VerdictFalse {
				detail = "条件无法判定（" + string(verdict) + "），按不成立处理，走 else 分支"
			}
			trace = append(trace, SimStep{At: rel(base, now), Kind: "branch",
				Verdict: string(verdict), Detail: ins.Path + "：" + detail})
			if taken {
				pc++
			} else {
				pc = ins.ElseTarget
			}
			continue
		}

		// A guard is an interlock, not a soft failure: the executor stops the
		// run and never continues past it.
		if s.Guard != nil {
			g := domain.Evaluate(domain.Trigger{Groups: []domain.Group{*s.Guard}},
				ctxAt(now)).Verdict
			if g != domain.VerdictTrue {
				actions = append(actions, SimAction{Path: ins.Path, Kind: string(s.Kind),
					Outcome: "blocked",
					Detail:  "前置条件不成立（" + string(g) + "），实际执行会在这里终止"})
				trace = append(trace, SimStep{At: rel(base, now), Kind: "guard",
					Verdict: string(g), Detail: ins.Path + "：前置条件不成立，运行会终止"})
				return actions, trace, now
			}
			trace = append(trace, SimStep{At: rel(base, now), Kind: "guard",
				Verdict: "true", Detail: ins.Path + "：前置条件成立，放行"})
		}

		switch s.Kind {
		case domain.StepWait:
			now = now.Add(s.Wait.Std())
			actions = append(actions, SimAction{Path: ins.Path, Kind: "wait",
				Outcome: "would_execute", Detail: "等待 " + s.Wait.Std().String() +
					"，虚拟时钟推进到 " + rel(base, now)})
		default:
			actions = append(actions, SimAction{Path: ins.Path, Kind: string(s.Kind),
				Outcome: "would_execute", Detail: describeStep(s)})
		}
		pc++
	}
	return actions, trace, now
}

func countWould(actions []SimAction) int {
	n := 0
	for _, a := range actions {
		if a.Outcome == "would_execute" {
			n++
		}
	}
	return n
}

func describeActionsOf(actions []SimAction) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		mark := "将执行"
		if a.Outcome != "would_execute" {
			mark = "不会执行（" + a.Outcome + "）"
		}
		out = append(out, a.Path+" "+mark+": "+a.Detail)
	}
	return out
}

func describeStep(s domain.Step) string {
	switch s.Kind {
	case domain.StepAction:
		return fmt.Sprintf("%s %s=%s", s.Action, s.Identifier, s.Value)
	case domain.StepAlarm:
		return "告警:" + s.AlarmPolicyID
	case domain.StepNotify:
		return "通知:" + s.NotificationPolicyID
	case domain.StepActivateScene:
		return "激活场景:" + s.Value
	}
	return string(s.Kind)
}

func describeActions(prog []Instruction) []string {
	var out []string
	for _, ins := range prog {
		if ins.Op != OpStep {
			continue
		}
		switch ins.Step.Kind {
		case domain.StepAction:
			out = append(out, fmt.Sprintf("%s %s=%s", ins.Step.Action, ins.Step.Identifier, ins.Step.Value))
		case domain.StepAlarm:
			out = append(out, "告警:"+ins.Step.AlarmPolicyID)
		case domain.StepNotify:
			out = append(out, "通知:"+ins.Step.NotificationPolicyID)
		case domain.StepWait:
			out = append(out, "等待 "+ins.Step.Wait.Std().String())
		case domain.StepActivateScene:
			out = append(out, "激活场景:"+ins.Step.Value)
		}
	}
	return out
}
