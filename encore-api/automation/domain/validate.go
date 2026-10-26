package domain

import (
	"fmt"
	"strings"
	"time"
)

// Issue is one machine readable validation failure. The API returns these so
// Web, CLI and MCP all report the same field level error instead of a generic
// 500.
type Issue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (i Issue) Error() string { return i.Field + ": " + i.Message }

// ValidationError aggregates issues.
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, i := range e.Issues {
		parts = append(parts, i.Error())
	}
	return "规则校验失败: " + strings.Join(parts, "; ")
}

// Validate checks a definition before it may be stored as a revision. It is the
// single gate used by every entry point (A04).
func Validate(d *Definition) error {
	d.Normalize()
	var issues []Issue
	add := func(field, code, msg string) {
		issues = append(issues, Issue{Field: field, Code: code, Message: msg})
	}

	switch d.Kind {
	case KindAutomation, KindScene, KindAlarm:
	default:
		add("kind", "unsupported_kind", "不支持的类型: "+string(d.Kind))
	}
	switch d.Mode {
	case ModeSingle, ModeRestart, ModeQueued, ModeParallel:
	default:
		add("mode", "unsupported_mode", "不支持的运行模式: "+string(d.Mode))
	}
	switch d.Scope {
	case ScopeDevice, ScopeRule:
	default:
		add("scope", "unsupported_scope", "不支持的作用范围: "+string(d.Scope))
	}

	// A manual scene has no trigger: the user presses "激活" and the steps run.
	// The old scene object worked exactly like that (no enable switch, no
	// condition). Forcing a condition on it would make the author invent one and
	// would change the meaning of an existing scene, so the requirement is
	// relaxed for kind=scene only — and only there.
	if len(d.Trigger.Groups) == 0 {
		if d.Kind != KindScene {
			add("trigger.groups", "empty_trigger", "至少需要一组触发条件")
		}
	}
	for gi, g := range d.Trigger.Groups {
		if len(g.Conditions) == 0 {
			add(fmt.Sprintf("trigger.groups[%d]", gi), "empty_group", "条件组不能为空")
			continue
		}
		for ci, c := range g.Conditions {
			field := fmt.Sprintf("trigger.groups[%d].conditions[%d]", gi, ci)
			validateCondition(c, field, add)
		}
	}
	if d.Trigger.For < 0 {
		add("trigger.for", "negative_duration", "持续时间不能为负")
	}
	if d.Trigger.For > Duration(24*time.Hour) {
		add("trigger.for", "duration_too_long", "持续时间超过 24 小时")
	}
	if d.Recovery != nil {
		if len(d.Recovery.Groups) == 0 {
			add("recovery.groups", "empty_recovery", "恢复条件不能为空")
		}
		for gi, g := range d.Recovery.Groups {
			for ci, c := range g.Conditions {
				validateCondition(c, fmt.Sprintf("recovery.groups[%d].conditions[%d]", gi, ci), add)
			}
		}
	}
	if d.Freshness.MaxAge <= 0 {
		add("freshness.max_age", "invalid_freshness", "max_age 必须为正数")
	}
	if d.Freshness.MaxGap <= 0 {
		add("freshness.max_gap", "invalid_freshness", "max_gap 必须为正数")
	}
	if d.Freshness.MaxGap > d.Freshness.MaxAge {
		add("freshness.max_gap", "gap_exceeds_age", "max_gap 不能大于 max_age")
	}
	switch d.Freshness.Mode {
	case "", FreshnessPeriodic:
	case FreshnessOnChange:
		// Without a declared window, "the value did not change" and "the device
		// is gone" look identical, and guessing between them is exactly the
		// defect this field exists to prevent.
		if d.Freshness.Heartbeat <= 0 {
			add("freshness.heartbeat", "heartbeat_required",
				"仅变更上报（on_change）必须声明 heartbeat，否则无法判断设备是否仍然可信")
		}
	default:
		add("freshness.mode", "invalid_freshness_mode",
			fmt.Sprintf("未知新鲜度依据 %q（只能是 periodic 或 on_change）", d.Freshness.Mode))
	}
	if d.Freshness.NoData < 0 {
		add("freshness.no_data", "invalid_freshness", "no_data 不能为负数")
	}

	// Targets: at least one selection, bounded size.
	if len(d.Targets.DeviceIDs) == 0 && len(d.Targets.DeviceConfigIDs) == 0 &&
		len(d.Targets.AreaIDs) == 0 && len(d.Targets.Labels) == 0 &&
		len(d.Targets.DeviceTypes) == 0 {
		add("targets", "empty_targets", "至少需要指定一个目标（设备/配置/区域/标签/类型）")
	}
	if d.Targets.MaxTargets <= 0 || d.Targets.MaxTargets > 1000 {
		add("targets.max_targets", "invalid_budget", "目标上限必须在 1..1000 之间")
	}
	if lm := strings.ToLower(d.Targets.LabelMatch); lm != "" && lm != "and" && lm != "or" {
		add("targets.label_match", "invalid_label_match", "标签匹配只能是 and 或 or")
	}

	// Steps and budget.
	stepCount, maxDepth := countSteps(d.Steps, 1)
	// An alarm policy has no actions to run: its "effect" is the lifecycle
	// transition and the notification intent written in the same transaction as
	// that transition. Requiring a step here used to force authors to add one
	// that never executes — a form field that looks like it sends a message and
	// does nothing. The runtime already refuses to start a run for an alarm
	// policy, so an empty step list is the truthful representation.
	if stepCount == 0 && d.Kind != KindAlarm {
		add("steps", "empty_steps", "至少需要一个执行步骤")
	}
	if stepCount > d.Budget.MaxSteps {
		add("steps", "too_many_steps",
			fmt.Sprintf("步骤数 %d 超过预算 %d", stepCount, d.Budget.MaxSteps))
	}
	if maxDepth > d.Budget.MaxDepth {
		add("steps", "too_deep",
			fmt.Sprintf("分支嵌套深度 %d 超过预算 %d", maxDepth, d.Budget.MaxDepth))
	}
	validateSteps(d.Steps, "steps", add)

	if d.Mode == ModeQueued {
		if d.Queue == nil || d.Queue.TTL <= 0 || d.Queue.MaxDepth <= 0 {
			add("queue", "invalid_queue", "排队模式必须声明 ttl 与 max_depth")
		}
	}
	if d.Mode == ModeParallel {
		if d.Parallel == nil || d.Parallel.MaxConcurrent <= 0 || d.Parallel.TTL <= 0 {
			add("parallel", "invalid_parallel", "并行模式必须声明 max_concurrent 与 ttl")
		}
	}

	// Alarm policies must not carry device actions: the alarm owner only
	// produces lifecycle transitions.
	if d.Kind == KindAlarm {
		for i, s := range d.Steps {
			if s.Kind == StepAction || s.Kind == StepActivateScene {
				add(fmt.Sprintf("steps[%d]", i), "alarm_side_effect",
					"告警策略不能直接下发设备动作；请用告警生命周期事件触发自动化")
			}
		}
		if d.Alarm == nil {
			add("alarm", "missing_alarm_spec",
				"告警策略必须声明 alarm：稳定业务键 key 与受理来源 sources")
		} else {
			validateAlarmSpec(d.Alarm, add)
		}

	} else if d.Alarm != nil {
		add("alarm", "alarm_spec_not_allowed", "只有告警策略可以声明 alarm")
	}

	// Parameter slots are checked last, because the check reads the serialized
	// body: everything above has to be structurally sound before asking which
	// placeholders the body actually contains.
	validateParams(d, add)

	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func validateCondition(c Condition, field string, add func(string, string, string)) {
	switch c.Source {
	case "", "self", "device":
	default:
		add(field+".source", "unsupported_source", "条件来源只能是 self 或 device")
	}
	if c.Source == "device" && strings.TrimSpace(c.DeviceID) == "" {
		add(field+".device_id", "missing_device", "指定设备条件必须给出 device_id")
	}
	switch c.Signal {
	case SignalTelemetry, SignalAttribute, SignalEvent, SignalStatus, SignalAlarm:
	default:
		add(field+".signal", "unsupported_signal", "不支持的信号类型: "+string(c.Signal))
	}
	if strings.TrimSpace(c.Key) == "" {
		add(field+".key", "missing_key", "必须指定指标标识符")
	}
	if !ValidOperators[c.Operator] {
		add(field+".operator", "unsupported_operator", "不支持的比较符: "+c.Operator)
	}
	if c.Signal == SignalEvent && c.EventMatch != nil {
		if len(c.EventMatch.Fields) == 0 {
			add(field+".event_match", "empty_event_match", "事件字段条件不能为空")
		}
		for i, f := range c.EventMatch.Fields {
			if strings.TrimSpace(f.Path) == "" {
				add(fmt.Sprintf("%s.event_match.fields[%d].path", field, i), "missing_path", "字段路径不能为空")
			}
			if !ValidOperators[f.Operator] {
				add(fmt.Sprintf("%s.event_match.fields[%d].operator", field, i), "unsupported_operator",
					"不支持的比较符: "+f.Operator)
			}
		}
	}
}

// maxBranchDepth bounds how deeply if/else may nest. A deeper program cannot be
// read by a customer in the form they built it, and it makes the compiled
// program and the run record harder to follow than the rule is worth.
const maxBranchDepth = 3

func validateSteps(steps []Step, field string, add func(string, string, string)) {
	validateStepsAt(steps, field, 0, add)
}

func validateStepsAt(steps []Step, field string, depth int, add func(string, string, string)) {
	for i, s := range steps {
		f := fmt.Sprintf("%s[%d]", field, i)
		if s.Guard != nil {
			if len(s.Guard.Conditions) == 0 {
				add(f+".guard", "empty_guard", "前置检查必须给出条件")
			}
			for j, c := range s.Guard.Conditions {
				validateCondition(c, fmt.Sprintf("%s.guard.conditions[%d]", f, j), add)
			}
			if s.Kind == StepBranch || s.Kind == StepWait {
				add(f+".guard", "guard_not_allowed",
					"前置检查只能挂在会产生实际动作的步骤上")
			}
		}
		switch s.Kind {
		case StepAction:
			switch s.Action {
			case "telemetry", "attribute", "command":
			default:
				add(f+".action", "unsupported_action", "不支持的动作类型: "+s.Action)
			}
			if strings.TrimSpace(s.Identifier) == "" && s.Action != "telemetry" && s.Action != "attribute" {
				add(f+".identifier", "missing_identifier", "命令动作必须指定标识符")
			}
			if strings.TrimSpace(s.Value) == "" {
				add(f+".value", "missing_value", "动作必须给出目标值")
			}
		case StepWait:
			if s.Wait <= 0 {
				add(f+".wait", "invalid_wait", "等待步骤必须给出正数时长")
			}
			if s.Wait > Duration(24*time.Hour) {
				add(f+".wait", "wait_too_long", "等待时长超过 24 小时")
			}
		case StepAlarm:
			if strings.TrimSpace(s.AlarmPolicyID) == "" {
				add(f+".alarm_policy_id", "missing_policy", "告警步骤必须指定策略")
			}
		case StepNotify:
			if strings.TrimSpace(s.NotificationPolicyID) == "" {
				add(f+".notification_policy_id", "missing_policy", "通知步骤必须指定通知策略")
			}
		case StepBranch:
			if s.Condition == nil || len(s.Condition.Conditions) == 0 {
				add(f+".condition", "empty_branch_condition", "分支必须给出条件")
			}
			if len(s.Then) == 0 && len(s.Else) == 0 {
				add(f, "empty_branch", "分支至少需要一个 then 或 else 步骤")
			}
			if depth+1 > maxBranchDepth {
				add(f, "nesting_too_deep",
					fmt.Sprintf("分支嵌套超过 %d 层", maxBranchDepth))
			}
			validateStepsAt(s.Then, f+".then", depth+1, add)
			validateStepsAt(s.Else, f+".else", depth+1, add)
		case StepActivateScene:
			if strings.TrimSpace(s.Value) == "" {
				add(f+".value", "missing_scene", "必须指定要激活的场景")
			}
		default:
			if !SupportsStepKind(s.Kind) {
				add(f+".kind", "unsupported_step",
					"不支持的步骤类型: "+string(s.Kind)+"（本版本可执行："+supportedStepList()+"）")
			}
		}
	}
}

// supportedStepList renders the executable kinds in a stable order, so the error
// message tells the author what they may use instead of only what they cannot.
func supportedStepList() string {
	kinds := []StepKind{StepAction, StepWait, StepBranch, StepAlarm, StepNotify, StepActivateScene}
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		if SupportsStepKind(k) {
			parts = append(parts, string(k))
		}
	}
	return strings.Join(parts, "/")
}

// CountSteps reports the flattened step count, used by the API to show the
// budget consumption before publishing.
func CountSteps(steps []Step) int {
	n, _ := countSteps(steps, 1)
	return n
}

func countSteps(steps []Step, depth int) (count, maxDepth int) {
	maxDepth = depth
	for _, s := range steps {
		count++
		if s.Kind == StepBranch {
			c1, d1 := countSteps(s.Then, depth+1)
			c2, d2 := countSteps(s.Else, depth+1)
			count += c1 + c2
			if d1 > maxDepth {
				maxDepth = d1
			}
			if d2 > maxDepth {
				maxDepth = d2
			}
		}
	}
	return count, maxDepth
}

// validateAlarmSpec enforces the two things that make an alarm policy
// trustworthy: an identity that survives re-publishing, and intake channels that
// are declared rather than assumed.
func validateAlarmSpec(a *AlarmSpec, add func(string, string, string)) {
	if k := strings.TrimSpace(a.Key); k != "" {
		if len(k) > 120 {
			add("alarm.key", "invalid_alarm_key", "业务键不能超过 120 个字符")
		} else if !validAlarmKey(k) {
			add("alarm.key", "invalid_alarm_key",
				"业务键只能包含字母、数字、点、下划线、冒号和连字符")
		}
	}
	if len(a.Sources) == 0 {
		add("alarm.sources", "empty_alarm_sources",
			"必须声明至少一个受理来源（detector / event）")
	}
	seen := map[AlarmSource]bool{}
	hasEvent := false
	for _, s := range a.Sources {
		switch s {
		case AlarmSourceDetector:
		case AlarmSourceEvent:
			hasEvent = true
		default:
			add("alarm.sources", "invalid_alarm_source",
				"未知受理来源 "+string(s)+"（只能是 detector 或 event）")
		}
		if seen[s] {
			add("alarm.sources", "duplicate_alarm_source", "受理来源重复: "+string(s))
		}
		seen[s] = true
	}
	// The event channel is the only one an outside caller can reach, so it is
	// the only one that needs a whitelist. Demanding one when the channel is
	// closed would be a declaration nobody enforces; allowing event without one
	// would let any caller raise any alarm.
	if hasEvent {
		if len(a.AllowedSources) == 0 {
			add("alarm.allowed_sources", "empty_allowed_sources",
				"声明 event 受理来源时必须给出允许的调用方（如 scene:<id>、automation）")
		}
		for i, s := range a.AllowedSources {
			if strings.TrimSpace(s) == "" {
				add(fmt.Sprintf("alarm.allowed_sources[%d]", i), "empty_allowed_source",
					"允许的调用方不能为空字符串")
			}
		}
	} else if len(a.AllowedSources) > 0 {
		add("alarm.allowed_sources", "allowed_sources_without_event",
			"未声明 event 受理来源时不应给出调用方白名单")
	}
	switch a.Level {
	case "", "H", "M", "L":
	default:
		add("alarm.level", "invalid_alarm_level", "告警等级只能是 H、M 或 L")
	}
	if len(a.Dimension) > 64 {
		add("alarm.dimension", "invalid_alarm_dimension", "维度标识不能超过 64 个字符")
	}
}

func validAlarmKey(k string) bool {
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == ':', r == '-':
		default:
			return false
		}
	}
	return k != ""
}
