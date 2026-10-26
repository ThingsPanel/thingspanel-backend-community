// Package executor holds the admission contract for external executors.
//
// An external engine (Node-RED, Flink, a customer's own job) is allowed to drive
// devices and to qualify alarms, but only inside a contract it registered in
// advance. The rules in this package are what makes "registered" mean something:
//
//   - the contract names an engine profile, the input it may read, the outputs
//     it may produce and the devices it may touch;
//   - every envelope carries the executor's epoch and a monotonically increasing
//     sequence number, so a replayed or reordered message is rejected instead of
//     opening a valve a second time;
//   - an alarm verdict names the exact policy revision and definition hash the
//     engine was deployed against. A verdict computed from an older threshold is
//     refused, because accepting it would let an external job keep running a
//     policy the operator has already changed;
//   - a "qualified" verdict and a raw observation are different things. An engine
//     that reports "the condition held for five minutes" must say since when, and
//     the platform checks that claim against its own published duration. Mixing
//     the two — sending a raw true/false and letting it be read as "for satisfied"
//     — is rejected outright.
//
// Nothing here touches the database or the network: the same functions are used
// by the endpoints, by the tests and by the documentation generator, so the
// contract cannot drift between them.
package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// Engine is the execution engine an executor runs. It is recorded rather than
// inferred: two engines with the same capabilities still have different timing
// semantics, and the operator needs to know which one produced a verdict.
type Engine string

const (
	EngineNodeRed Engine = "node_red"
	EngineFlink   Engine = "flink"
	EngineGeneric Engine = "generic"
)

func (e Engine) Valid() bool {
	switch e {
	case EngineNodeRed, EngineFlink, EngineGeneric:
		return true
	default:
		return false
	}
}

// Capability is one thing an executor is allowed to produce. The list is closed:
// an executor that declares something else cannot register at all, so a typo
// becomes a registration error instead of a silently ignored permission.
type Capability string

const (
	// CapabilityActionIntent is "run this published rule on this device".
	CapabilityActionIntent Capability = "action_intent"
	// CapabilityAlarmVerdict is "this policy's condition is qualified / cleared".
	CapabilityAlarmVerdict Capability = "alarm_verdict"
)

func (c Capability) Valid() bool {
	return c == CapabilityActionIntent || c == CapabilityAlarmVerdict
}

// Scope is the set of devices an executor may address.
//
// It is a whitelist, never a blacklist: an empty scope means "nothing", not
// "everything". That default is deliberate — a registration missing a field must
// fail closed.
type Scope struct {
	DeviceIDs       []string `json:"device_ids,omitempty"`
	DeviceConfigIDs []string `json:"device_config_ids,omitempty"`
	Labels          []string `json:"labels,omitempty"`
}

// Empty reports whether the scope addresses no device at all.
func (s Scope) Empty() bool {
	return len(s.DeviceIDs) == 0 && len(s.DeviceConfigIDs) == 0 && len(s.Labels) == 0
}

// Allows answers whether one device may be addressed, given what the platform
// knows about it. `deviceConfigID` and `labels` come from the device directory;
// an unknown device (empty config id, no labels) is only allowed when the scope
// names it explicitly.
func (s Scope) Allows(deviceID, deviceConfigID string, labels []string) bool {
	for _, id := range s.DeviceIDs {
		if id == deviceID {
			return true
		}
	}
	if deviceConfigID != "" {
		for _, id := range s.DeviceConfigIDs {
			if id == deviceConfigID {
				return true
			}
		}
	}
	if len(s.Labels) > 0 && len(labels) > 0 {
		owned := map[string]bool{}
		for _, label := range labels {
			owned[label] = true
		}
		all := true
		for _, want := range s.Labels {
			if !owned[want] {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// Contract is what an executor registers. It is immutable once registered: a
// change is a new registration with a higher epoch, so a message always names
// the contract it was produced under.
type Contract struct {
	Engine Engine `json:"engine"`
	// InputScope is what the executor is allowed to subscribe to. It is recorded
	// so the platform can show which data left the building, and so a later
	// subscription request can be checked against it.
	InputScope Scope `json:"input_scope"`
	// Outputs is the closed list of things this executor may produce.
	Outputs []Capability `json:"outputs"`
	// TargetScope bounds the devices an action intent may address.
	TargetScope Scope `json:"target_scope"`
	// MaxTargets and MaxRatePerMinute are the declared back-pressure limits. They
	// are part of the contract, not a runtime tuning knob: an executor that
	// exceeds them is refused, which is how one misbehaving workflow is stopped
	// without turning the platform off.
	MaxTargets    int `json:"max_targets"`
	MaxRatePerMin int `json:"max_rate_per_min"`
}

// Issue is one reason a registration or an envelope was refused.
type Issue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (i Issue) String() string { return i.Field + ": " + i.Code + ": " + i.Message }

// Validate checks a contract on its own, before it is stored.
func (c Contract) Validate() []Issue {
	var issues []Issue
	add := func(field, code, msg string) {
		issues = append(issues, Issue{Field: field, Code: code, Message: msg})
	}
	if !c.Engine.Valid() {
		add("contract.engine", "unsupported_engine", "不支持的引擎类型: "+string(c.Engine))
	}
	if c.InputScope.Empty() {
		add("contract.input_scope", "empty_scope", "输入范围不能为空；空范围表示不允许读任何设备，不能注册")
	}
	if c.TargetScope.Empty() {
		add("contract.target_scope", "empty_scope", "目标范围不能为空；空范围会让这条登记永远无法执行任何动作")
	}
	if len(c.Outputs) == 0 {
		add("contract.outputs", "empty_outputs", "至少要声明一种输出能力")
	}
	seen := map[Capability]bool{}
	for i, output := range c.Outputs {
		if !output.Valid() {
			add(fmt.Sprintf("contract.outputs[%d]", i), "unsupported_capability",
				"不支持的输出能力: "+string(output))
		}
		if seen[output] {
			add(fmt.Sprintf("contract.outputs[%d]", i), "duplicate_capability", "输出能力重复声明: "+string(output))
		}
		seen[output] = true
	}
	if c.MaxTargets <= 0 {
		add("contract.max_targets", "required", "必须声明一次最多影响多少台设备")
	}
	if c.MaxTargets > 2000 {
		add("contract.max_targets", "too_large", "单次目标上限不能超过 2000")
	}
	if c.MaxRatePerMin <= 0 {
		add("contract.max_rate_per_min", "required", "必须声明每分钟最多提交多少次")
	}
	if c.MaxRatePerMin > 6000 {
		add("contract.max_rate_per_min", "too_large", "每分钟提交上限不能超过 6000")
	}
	return issues
}

// Hash is the identity of a contract. It is stored with the registration and
// echoed in every envelope, so "the engine ran an older contract" is visible
// rather than inferred from a timestamp.
func (c Contract) Hash() string {
	parts := []string{string(c.Engine)}
	parts = append(parts, "in:"+c.InputScope.canonical())
	parts = append(parts, "out:"+canonicalCaps(c.Outputs))
	parts = append(parts, "target:"+c.TargetScope.canonical())
	parts = append(parts, fmt.Sprintf("max:%d/%d", c.MaxTargets, c.MaxRatePerMin))
	return shortHash(strings.Join(parts, "|"))
}

// Allows reports whether the contract grants a capability.
func (c Contract) Allows(capability Capability) bool {
	for _, output := range c.Outputs {
		if output == capability {
			return true
		}
	}
	return false
}

func (s Scope) canonical() string {
	ids := append([]string(nil), s.DeviceIDs...)
	cfgs := append([]string(nil), s.DeviceConfigIDs...)
	labels := append([]string(nil), s.Labels...)
	sort.Strings(ids)
	sort.Strings(cfgs)
	sort.Strings(labels)
	return strings.Join(ids, ",") + ";" + strings.Join(cfgs, ",") + ";" + strings.Join(labels, ",")
}

func canonicalCaps(c []Capability) string {
	out := make([]string, 0, len(c))
	for _, item := range c {
		out = append(out, string(item))
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// Envelope is the common head of every message an executor sends.
//
// Epoch is the registration generation and Seq is monotonic within it. Both are
// required: a message that arrives after the executor was re-registered with a
// narrower scope must be refused, and a message that arrives twice must not be
// executed twice — even if the transport gave it a new id.
type Envelope struct {
	ExecutorID string
	Epoch      int
	Seq        int64
	// ContractHash pins the contract the message was produced under.
	ContractHash string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	// IdempotencyKey is the executor's own stable key for this intent. Retrying
	// the same intent must reuse it; changing it turns a retry into a new action.
	IdempotencyKey string
	// CausalRoot links back to the upstream event or run that caused this
	// message, so a command can be traced to its trigger across the boundary.
	CausalRoot string
}

// Registration is the stored side of a contract, as the validator needs it.
type Registration struct {
	ID           string
	Epoch        int
	Contract     Contract
	ContractHash string
	Enabled      bool
	// LastSeq is the highest sequence number already accepted for this epoch.
	LastSeq int64
}

// ValidateEnvelope checks everything that does not depend on the payload.
func (e Envelope) Validate(now time.Time, reg Registration) []Issue {
	var issues []Issue
	add := func(field, code, msg string) {
		issues = append(issues, Issue{Field: field, Code: code, Message: msg})
	}
	if !reg.Enabled {
		add("executor_id", "executor_disabled", "这个执行者已被停用")
	}
	if strings.TrimSpace(e.IdempotencyKey) == "" {
		add("idempotency_key", "required", "必须带上幂等键；否则重试会被当成新的动作")
	}
	if e.Epoch != reg.Epoch {
		add("epoch", "superseded_epoch",
			fmt.Sprintf("消息属于第 %d 代登记，当前是第 %d 代；旧代际的判定和动作都不再受理", e.Epoch, reg.Epoch))
	}
	if e.Seq <= 0 {
		add("seq", "required", "必须带上单调递增的序号")
	} else if e.Seq <= reg.LastSeq {
		add("seq", "replayed_sequence",
			fmt.Sprintf("序号 %d 不大于已受理的 %d；这是重放或乱序，不能再次执行", e.Seq, reg.LastSeq))
	}
	if e.ContractHash != "" && e.ContractHash != reg.ContractHash {
		add("contract_hash", "stale_contract", "消息按旧合同生成，与当前登记的合同不一致")
	}
	if !e.ExpiresAt.IsZero() && !now.Before(e.ExpiresAt) {
		add("expires_at", "expired", "消息已过期，不再受理")
	}
	if e.ExpiresAt.IsZero() {
		add("expires_at", "required", "必须声明有效期，否则无法判断一条迟到的消息还能不能用")
	}
	if !e.IssuedAt.IsZero() && e.ExpiresAt.After(e.IssuedAt) {
		// A window longer than an hour is almost always a clock or config mistake.
		if e.ExpiresAt.Sub(e.IssuedAt) > time.Hour {
			add("expires_at", "window_too_long", "有效期不能超过 1 小时")
		}
	}
	return issues
}

// VerdictMode separates "the condition is true now" from "the condition has held
// long enough". They are not interchangeable: the platform's published `for` is
// the only thing that may decide the second one.
type VerdictMode string

const (
	// ModeRaw reports an observation. The platform applies its own duration.
	ModeRaw VerdictMode = "raw"
	// ModeQualified reports that the engine has already applied a duration, and
	// must therefore say since when.
	ModeQualified VerdictMode = "qualified"
)

// Verdict is one alarm judgement submitted by an external executor.
type Verdict struct {
	Envelope
	PolicyID       string
	PolicyRevision int
	DefinitionHash string
	Mode           VerdictMode
	// Observed is the raw truth value. For ModeQualified it is the value at the
	// end of the window, and Since must be set.
	Observed bool
	// Since is when the condition started holding. Required for ModeQualified.
	Since      *time.Time
	SubjectID  string
	Dimension  string
	ActionKind string
}

// PolicyRef is the published side of an alarm policy, as the validator needs it.
type PolicyRef struct {
	Revision       int
	DefinitionHash string
	// ForSeconds is the published hold duration. Zero means "fire immediately",
	// which is the legacy behaviour.
	ForSeconds  float64
	Enabled     bool
	AllowsEvent bool
	// SourceAllowed is the policy's own whitelist check for this executor.
	SourceAllowed bool
}

// ValidateVerdict checks a verdict against the contract and the published policy.
func (v Verdict) Validate(now time.Time, reg Registration, policy PolicyRef) []Issue {
	issues := v.Envelope.Validate(now, reg)
	add := func(field, code, msg string) {
		issues = append(issues, Issue{Field: field, Code: code, Message: msg})
	}
	if !reg.Contract.Allows(CapabilityAlarmVerdict) {
		add("executor_id", "capability_denied", "这个执行者没有登记告警判定能力")
	}
	if strings.TrimSpace(v.PolicyID) == "" {
		add("policy_id", "required", "必须指明是哪条告警策略")
	}
	if strings.TrimSpace(v.SubjectID) == "" {
		add("subject_id", "required", "必须指明判定针对哪台设备或哪个对象")
	}
	if v.ActionKind != "qualified" && v.ActionKind != "clear" {
		add("action_kind", "unsupported_kind", "只接受 qualified 或 clear")
	}

	// The revision check is the whole point of this channel: a job deployed
	// against last week's threshold must not be able to raise an alarm today.
	if policy.Revision == 0 {
		add("policy_id", "policy_missing", "告警策略不存在")
	} else {
		if v.PolicyRevision != policy.Revision {
			add("policy_revision", "stale_policy_revision",
				fmt.Sprintf("判定基于第 %d 版策略，当前已发布第 %d 版", v.PolicyRevision, policy.Revision))
		}
		if v.DefinitionHash != "" && v.DefinitionHash != policy.DefinitionHash {
			add("definition_hash", "stale_policy_definition",
				"判定基于的策略内容与当前发布的不一致")
		}
	}
	if policy.Revision != 0 && !policy.Enabled {
		add("policy_id", "policy_disabled", "告警策略已停用")
	}
	if policy.Revision != 0 && !policy.AllowsEvent {
		add("policy_id", "policy_rejects_event", "这条策略不接受外部事件判定")
	}
	if policy.Revision != 0 && !policy.SourceAllowed {
		add("executor_id", "source_denied", "这条策略的允许来源里没有这个执行者")
	}

	switch v.Mode {
	case ModeRaw:
		if v.Since != nil {
			add("since", "mixed_verdict_semantics",
				"原始观测不应带起始时间；带起始时间会被读成“已满足持续时长”，两者不能混用")
		}
		if v.ActionKind == "qualified" && policy.ForSeconds > 0 {
			// A raw observation cannot qualify a policy that requires a hold; the
			// engine would be claiming a duration it never measured.
			add("mode", "raw_cannot_qualify",
				fmt.Sprintf("这条策略要求条件连续满足 %.0f 秒，原始观测不能直接判定为已满足", policy.ForSeconds))
		}
	case ModeQualified:
		if v.Since == nil {
			add("since", "qualified_without_evidence",
				"声明“已满足持续时长”必须给出条件开始成立的时间")
			break
		}
		held := v.IssuedAt.Sub(*v.Since).Seconds()
		if v.IssuedAt.IsZero() {
			held = now.Sub(*v.Since).Seconds()
		}
		if held < 0 {
			add("since", "since_in_future", "条件开始时间晚于消息发出时间")
		} else if policy.ForSeconds > 0 && held+0.5 < policy.ForSeconds {
			add("since", "hold_not_satisfied",
				fmt.Sprintf("按平台发布的持续时长 %.0f 秒计算，目前只满足 %.0f 秒", policy.ForSeconds, held))
		}
	default:
		add("mode", "unsupported_mode", "只接受 raw 或 qualified")
	}
	return issues
}

// Intent is "run this published rule revision on this device".
//
// It is deliberately not a free-form step list. Letting an external engine send
// arbitrary steps would create a second execution path with its own ordering,
// retries and idempotency — and then two paths would own the same physical
// device. An intent names a published rule, so the existing run, generation and
// command-owner machinery decides everything else.
type Intent struct {
	Envelope
	RuleID     string
	Revision   int
	DeviceID   string
	Targets    []string
	CausalRoot string
}

// RuleRef is the published side of a rule, as the validator needs it.
type RuleRef struct {
	ID       string
	Revision int
	Enabled  bool
}

// ValidateIntent checks an action intent against the contract, the rule and the
// device directory.
func (in Intent) Validate(now time.Time, reg Registration, rule RuleRef, deviceConfigID string, labels []string) []Issue {
	issues := in.Envelope.Validate(now, reg)
	add := func(field, code, msg string) {
		issues = append(issues, Issue{Field: field, Code: code, Message: msg})
	}
	if !reg.Contract.Allows(CapabilityActionIntent) {
		add("executor_id", "capability_denied", "这个执行者没有登记动作意图能力")
	}
	if strings.TrimSpace(in.RuleID) == "" {
		add("rule_id", "required", "动作意图必须指明要执行哪条已发布规则")
	} else if rule.ID == "" {
		add("rule_id", "rule_missing", "规则不存在")
	} else {
		if !rule.Enabled {
			add("rule_id", "rule_disabled", "规则已停用，外部执行者不能绕过停用")
		}
		if in.Revision != rule.Revision {
			add("revision", "stale_rule_revision",
				fmt.Sprintf("意图基于第 %d 版规则，当前是第 %d 版", in.Revision, rule.Revision))
		}
	}
	targets := in.Targets
	if len(targets) == 0 && in.DeviceID != "" {
		targets = []string{in.DeviceID}
	}
	if len(targets) == 0 {
		add("targets", "required", "必须指明要影响哪台设备")
	}
	if len(targets) > reg.Contract.MaxTargets {
		add("targets", "too_many_targets",
			fmt.Sprintf("一次最多影响 %d 台设备，这次是 %d 台", reg.Contract.MaxTargets, len(targets)))
	}
	for i, target := range targets {
		if strings.TrimSpace(target) == "" {
			add(fmt.Sprintf("targets[%d]", i), "empty_device", "设备 ID 不能为空")
			continue
		}
		if !reg.Contract.TargetScope.Allows(target, deviceConfigID, labels) {
			add(fmt.Sprintf("targets[%d]", i), "out_of_scope",
				"设备 "+target+" 不在登记的 target_scope 内")
		}
	}
	return issues
}

// HasBlocking reports whether any issue prevents acceptance.
func HasBlocking(issues []Issue) bool { return len(issues) > 0 }

// Summary is the one-line description shown in the executor list.
func (c Contract) Summary() string {
	return fmt.Sprintf("%s · 输出 %s · 最多 %d 台 · 每分钟 %d 次",
		c.Engine, canonicalCaps(c.Outputs), c.MaxTargets, c.MaxRatePerMin)
}
