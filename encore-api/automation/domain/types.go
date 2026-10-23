// Package domain holds the typed rule model and the pure evaluation logic.
//
// It deliberately has no database, network or Encore dependency: the same
// evaluator is used by the real runtime, by the side-effect-free simulation
// endpoint and by table-driven unit tests. Anything that needs I/O is passed in
// through EvalContext.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RuleKind separates the three editable configuration objects. They share the
// evaluator but have different owners and different lifecycles.
type RuleKind string

const (
	KindAutomation RuleKind = "automation"
	KindScene      RuleKind = "scene"
	KindAlarm      RuleKind = "alarm_policy"
)

// RunMode is the concurrency contract for one scope. Defaults follow the
// product decision recorded in A02: new rules default to "single".
type RunMode string

const (
	ModeSingle   RunMode = "single"
	ModeRestart  RunMode = "restart"
	ModeQueued   RunMode = "queued"
	ModeParallel RunMode = "parallel"
)

// ScopeKind decides what competes with what. "device" means one run per
// device, so two devices driven by the same rule never cancel each other.
type ScopeKind string

const (
	ScopeDevice ScopeKind = "device"
	ScopeRule   ScopeKind = "rule"
)

// SignalKind mirrors the legacy TEL/ATTR/EVT/STATUS condition types.
type SignalKind string

const (
	SignalTelemetry SignalKind = "telemetry"
	SignalAttribute SignalKind = "attribute"
	SignalEvent     SignalKind = "event"
	SignalStatus    SignalKind = "status"
	// SignalAlarm is an alarm lifecycle transition used as an input. It lets a
	// rule react to "alarm qualified/cleared" without polling the alarm table.
	SignalAlarm SignalKind = "alarm"
)

// Verdict is the four-valued result of a condition. "unknown" and "error" are
// first class: they must never be silently folded into false, because that is
// what used to clear active alarms when a device went offline.
type Verdict string

const (
	VerdictTrue    Verdict = "true"
	VerdictFalse   Verdict = "false"
	VerdictUnknown Verdict = "unknown"
	VerdictError   Verdict = "error"
)

// Duration is a JSON friendly time.Duration ("30s", "5m").
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*d = 0
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		parsed, err := time.ParseDuration(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	}
	// Also accept plain seconds for hand written payloads.
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("invalid duration: %s", string(b))
	}
	*d = Duration(time.Duration(n * float64(time.Second)))
	return nil
}

// Definition is the immutable, versioned body of a rule revision.
type Definition struct {
	Kind      RuleKind    `json:"kind"`
	Mode      RunMode     `json:"mode"`
	Scope     ScopeKind   `json:"scope"`
	Trigger   Trigger     `json:"trigger"`
	Recovery  *Trigger    `json:"recovery,omitempty"`
	Freshness Freshness   `json:"freshness"`
	Targets   TargetSpec  `json:"targets"`
	Steps     []Step      `json:"steps"`
	Queue     *QueueRule  `json:"queue,omitempty"`
	Parallel  *ParallelRule `json:"parallel,omitempty"`
	Budget    Budget      `json:"budget"`
}

// Trigger is an OR of groups; each group is an AND of conditions.
type Trigger struct {
	Groups []Group `json:"groups"`
	For    Duration `json:"for"`
}

// Group is an AND of conditions.
type Group struct {
	ID         string      `json:"id,omitempty"`
	Conditions []Condition `json:"conditions"`
}

// Condition is a single typed leaf. Source "self" means the device that
// produced the triggering event; "device" pins an explicit device id, which is
// how cross-device rules avoid reading the same identifier from the wrong
// device.
type Condition struct {
	Source    string     `json:"source"` // self | device
	DeviceID  string     `json:"device_id,omitempty"`
	Signal    SignalKind `json:"signal"`
	Key       string     `json:"key"`
	Operator  string     `json:"operator"`
	Value     string     `json:"value"`
	Unit      string     `json:"unit,omitempty"`
	EventMatch *EventMatch `json:"event_match,omitempty"`
}

// EventMatch narrows an event condition to parameters carried by the event.
type EventMatch struct {
	Fields []EventField `json:"fields"`
}

type EventField struct {
	Path     string `json:"path"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// Freshness declares how long a stored state value stays trustworthy.
type Freshness struct {
	MaxAge Duration `json:"max_age"`
	MaxGap Duration `json:"max_gap"`
}

// TargetSpec is resolved to a frozen snapshot when a run is accepted.
type TargetSpec struct {
	DeviceIDs      []string `json:"device_ids,omitempty"`
	DeviceConfigIDs []string `json:"device_config_ids,omitempty"`
	AreaIDs        []string `json:"area_ids,omitempty"`
	IncludeSubAreas bool    `json:"include_sub_areas,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	LabelMatch     string   `json:"label_match,omitempty"` // and | or
	DeviceTypes    []string `json:"device_types,omitempty"`
	RequiredCapability string `json:"required_capability,omitempty"`
	MaxTargets     int      `json:"max_targets,omitempty"`
}

// Step is one finite action in a run.
type Step struct {
	Kind       StepKind   `json:"kind"`
	DeviceRef  string     `json:"device_ref,omitempty"` // target | self | explicit id
	Action     string     `json:"action,omitempty"`     // telemetry | attribute | command
	Identifier string     `json:"identifier,omitempty"`
	Value      string     `json:"value,omitempty"`
	Wait       Duration   `json:"wait,omitempty"`
	AlarmPolicyID string  `json:"alarm_policy_id,omitempty"`
	NotificationPolicyID string `json:"notification_policy_id,omitempty"`
	ContinueOnFailure bool `json:"continue_on_failure,omitempty"`
	Then       []Step     `json:"then,omitempty"` // finite if/else branch
	Else       []Step     `json:"else,omitempty"`
	Condition  *Group     `json:"condition,omitempty"`
}

type StepKind string

const (
	StepAction       StepKind = "action"
	StepWait         StepKind = "wait"
	StepAlarm        StepKind = "alarm"
	StepNotify       StepKind = "notify"
	StepBranch       StepKind = "branch"
	StepActivateScene StepKind = "activate_scene"
)

// QueueRule bounds the queued mode.
type QueueRule struct {
	TTL      Duration `json:"ttl"`
	MaxDepth int      `json:"max_depth"`
}

// ParallelRule bounds the parallel mode.
type ParallelRule struct {
	MaxConcurrent int      `json:"max_concurrent"`
	TTL           Duration `json:"ttl"`
}

// Budget is the versioned runtime budget (A04/Task book 7.4). Zero values are
// replaced by defaults during validation so nothing is "unlimited".
type Budget struct {
	MaxSteps        int      `json:"max_steps"`
	MaxDepth        int      `json:"max_depth"`
	MaxRunDuration  Duration `json:"max_run_duration"`
	MaxAttempts     int      `json:"max_attempts"`
	ActionTimeout   Duration `json:"action_timeout"`
}

// DefaultBudget is the frozen default for the first release. Values are small
// on purpose: every rule is bounded.
func DefaultBudget() Budget {
	return Budget{
		MaxSteps:       32,
		MaxDepth:       3,
		MaxRunDuration: Duration(10 * time.Minute),
		MaxAttempts:    5,
		ActionTimeout:  Duration(30 * time.Second),
	}
}

// Normalize fills omitted fields with the documented defaults. It does not
// invent values for fields that must be explicit (operators, keys).
func (d *Definition) Normalize() {
	if d.Kind == "" {
		d.Kind = KindAutomation
	}
	if d.Mode == "" {
		d.Mode = ModeSingle
	}
	if d.Scope == "" {
		d.Scope = ScopeDevice
	}
	if d.Freshness.MaxAge == 0 {
		d.Freshness.MaxAge = Duration(90 * time.Second)
	}
	if d.Freshness.MaxGap == 0 {
		d.Freshness.MaxGap = Duration(90 * time.Second)
	}
	def := DefaultBudget()
	if d.Budget.MaxSteps == 0 {
		d.Budget.MaxSteps = def.MaxSteps
	}
	if d.Budget.MaxDepth == 0 {
		d.Budget.MaxDepth = def.MaxDepth
	}
	if d.Budget.MaxRunDuration == 0 {
		d.Budget.MaxRunDuration = def.MaxRunDuration
	}
	if d.Budget.MaxAttempts == 0 {
		d.Budget.MaxAttempts = def.MaxAttempts
	}
	if d.Budget.ActionTimeout == 0 {
		d.Budget.ActionTimeout = def.ActionTimeout
	}
	if d.Mode == ModeQueued && d.Queue == nil {
		d.Queue = &QueueRule{TTL: Duration(2 * time.Minute), MaxDepth: 10}
	}
	if d.Mode == ModeParallel && d.Parallel == nil {
		d.Parallel = &ParallelRule{MaxConcurrent: 4, TTL: Duration(5 * time.Minute)}
	}
	if d.Targets.MaxTargets == 0 {
		d.Targets.MaxTargets = 200
	}
}

// DefinitionHash is a stable identity for a published definition. It is used
// to prove that a preview or an external deployment refers to the same body.
func DefinitionHash(d Definition) string {
	body, _ := json.Marshal(d)
	return fmt.Sprintf("%08x", fnv32a(body))
}

func fnv32a(b []byte) uint32 {
	var h uint32 = 2166136261
	for _, c := range b {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

// ---------------------------------------------------------------------------
// Evaluation
// ---------------------------------------------------------------------------

// Quality describes how much a stored sample can be trusted.
type Quality string

const (
	QualityGood    Quality = "good"
	QualityStale   Quality = "stale"
	QualityUnknown Quality = "unknown"
)

// Sample is one observation used by the evaluator.
type Sample struct {
	DeviceID   string
	Signal     SignalKind
	Key        string
	Value      any
	Unit       string
	ObservedAt time.Time
	ReceivedAt time.Time
	Seq        int64
	Quality    Quality
	EventID    string
	Params     map[string]any
}

// Reason explains a leaf or group result in business language.
type Reason struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	DeviceID string `json:"device_id,omitempty"`
	Key      string `json:"key,omitempty"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	// Age is the timestamp of the evidence a staleness decision was based on.
	Age string `json:"age,omitempty"`
}

// CompareError classifies why a comparison could not be performed, so an
// explain view can say "the value is not a number" instead of a generic
// "invalid comparison". Every branch that can fail returns one.
type CompareError struct {
	Code string
	Msg  string
}

func (e *CompareError) Error() string { return e.Msg }

// LeafResult is the outcome of one condition.
type LeafResult struct {
	Verdict Verdict
	Reasons []Reason
}

// GroupResult is the outcome of an AND group.
type GroupResult struct {
	Verdict Verdict
	Reasons []Reason
	// ShortCircuit records that the group stopped at the first false leaf.
	ShortCircuit bool
}

// Result is the whole trigger evaluation.
type Result struct {
	Verdict Verdict
	Groups  []GroupResult
	Reasons []Reason
}

// EvalContext carries everything the evaluator may look at. Lookup returns the
// latest stored state for a (device, signal, key); Trigger is the sample that
// woke the evaluator (nil when running from a timer).
type EvalContext struct {
	Now          time.Time
	SelfDeviceID string
	Trigger      *Sample

	// MaxAge bounds how old a stored value may be and still count as evidence.
	// A value older than this is "unknown", never "false": a device that went
	// silent two hours ago must not clear an active alarm by timing out.
	MaxAge time.Duration

	// Lookup reads the latest stored sample. The error return exists so a
	// storage failure can be reported as "error" instead of being silently
	// downgraded to "no data" — the two mean different things to an operator.
	Lookup func(deviceID string, signal SignalKind, key string) (Sample, bool, error)
}

// Evaluate runs the trigger against the context.
func Evaluate(t Trigger, ctx EvalContext) Result {
	res := Result{Verdict: VerdictFalse}
	var unknown, failed bool

	for _, g := range t.Groups {
		gr := evaluateGroup(g, ctx)
		res.Groups = append(res.Groups, gr)
		switch gr.Verdict {
		case VerdictTrue:
			// OR: one satisfied group is enough, regardless of other groups.
			res.Verdict = VerdictTrue
		case VerdictError:
			failed = true
		case VerdictUnknown:
			unknown = true
		}
		res.Reasons = append(res.Reasons, gr.Reasons...)
	}

	if res.Verdict != VerdictTrue {
		switch {
		case failed:
			res.Verdict = VerdictError
		case unknown:
			res.Verdict = VerdictUnknown
		default:
			res.Verdict = VerdictFalse
		}
	}
	return res
}

func evaluateGroup(g Group, ctx EvalContext) GroupResult {
	res := GroupResult{Verdict: VerdictTrue}
	var unknown, failed bool
	for _, c := range g.Conditions {
		lr := evaluateLeaf(c, ctx)
		res.Reasons = append(res.Reasons, lr.Reasons...)
		switch lr.Verdict {
		case VerdictFalse:
			// AND: a definite false decides the group.
			res.Verdict = VerdictFalse
			res.ShortCircuit = true
			return res
		case VerdictError:
			failed = true
		case VerdictUnknown:
			unknown = true
		}
	}
	switch {
	case failed:
		res.Verdict = VerdictError
	case unknown:
		res.Verdict = VerdictUnknown
	default:
		res.Verdict = VerdictTrue
	}
	return res
}

func evaluateLeaf(c Condition, ctx EvalContext) LeafResult {
	deviceID := c.DeviceID
	if c.Source == "" || c.Source == "self" {
		deviceID = ctx.SelfDeviceID
	}
	if deviceID == "" {
		return LeafResult{Verdict: VerdictError, Reasons: []Reason{{
			Code: "missing_device", Message: "条件未指定设备，无法求值", Key: c.Key,
		}}}
	}

	// Event conditions describe "this event happened", so they are only
	// provable while evaluating the event itself.
	if c.Signal == SignalEvent {
		if ctx.Trigger == nil || ctx.Trigger.Signal != SignalEvent ||
			ctx.Trigger.DeviceID != deviceID || ctx.Trigger.Key != c.Key {
			return LeafResult{Verdict: VerdictUnknown, Reasons: []Reason{{
				Code: "event_not_current", DeviceID: deviceID, Key: c.Key,
				Message: "本次求值不是该事件的发生时刻，无法确认事件条件",
			}}}
		}
		return compareLeaf(c, *ctx.Trigger, ctx.Now)
	}

	sample, ok, err := ctx.Lookup(deviceID, c.Signal, c.Key)
	if err != nil {
		return LeafResult{Verdict: VerdictError, Reasons: []Reason{{
			Code: "read_error", DeviceID: deviceID, Key: c.Key,
			Message: "读取设备数据失败：" + err.Error(),
		}}}
	}

	// The sample being evaluated is the freshest evidence for its own signal:
	// it is the message that arrived just now. Prefer it over the stored value
	// when it is at least as new, so a device with a lagging clock is not
	// mistaken for a device that went silent.
	//
	// When the stored value is strictly newer (an out-of-order delivery), the
	// stored value wins — otherwise a late-arriving old sample would overwrite
	// the judgement about the current state.
	fromTrigger := false
	if t := ctx.Trigger; t != nil && t.DeviceID == deviceID &&
		t.Key == c.Key && t.Signal == c.Signal &&
		(!ok || !t.ObservedAt.Before(sample.ObservedAt)) {
		sample, fromTrigger = *t, true
	}
	if !ok && !fromTrigger {
		return LeafResult{Verdict: VerdictUnknown, Reasons: []Reason{{
			Code: "no_data", DeviceID: deviceID, Key: c.Key,
			Message: "没有该设备该指标的数据，结果不确定",
		}}}
	}
	if sample.Quality == QualityUnknown {
		return LeafResult{Verdict: VerdictUnknown, Reasons: []Reason{{
			Code: "quality_unknown", DeviceID: deviceID, Key: c.Key,
			Message: "数据质量标记为未知，结果不确定",
		}}}
	}
	// Freshness applies to values read from storage. The triggering sample is
	// the current evidence by construction, so it is exempt.
	if !fromTrigger && ctx.MaxAge > 0 && !sample.ObservedAt.IsZero() &&
		ctx.Now.Sub(sample.ObservedAt) > ctx.MaxAge {
		return LeafResult{Verdict: VerdictUnknown, Reasons: []Reason{{
			Code: "stale_evidence", DeviceID: deviceID, Key: c.Key,
			Age: sample.ObservedAt.Format(time.RFC3339),
			Message: fmt.Sprintf("数据已过期（最后更新 %s，超过 max_age %s），结果不确定",
				sample.ObservedAt.Format(time.RFC3339), ctx.MaxAge),
		}}}
	}
	return compareLeaf(c, sample, ctx.Now)
}

func compareLeaf(c Condition, s Sample, now time.Time) LeafResult {
	// Unit compatibility is checked before comparison: comparing ℃ with ℉ is a
	// configuration error, not a false result.
	if c.Unit != "" && s.Unit != "" && c.Unit != s.Unit {
		return LeafResult{Verdict: VerdictError, Reasons: []Reason{{
			Code: "unit_mismatch", DeviceID: s.DeviceID, Key: c.Key,
			Expected: c.Unit, Actual: s.Unit,
			Message: "条件单位与数据单位不一致，无法比较",
		}}}
	}

	if c.Signal == SignalEvent && c.EventMatch != nil {
		return matchEventParams(c, s)
	}

	ok, err := compareValues(c.Operator, c.Value, s.Value)
	if err != nil {
		code := "invalid_compare"
		var ce *CompareError
		if errors.As(err, &ce) {
			code = ce.Code
		}
		return LeafResult{Verdict: VerdictError, Reasons: []Reason{{
			Code: code, DeviceID: s.DeviceID, Key: c.Key,
			Expected: c.Operator + " " + c.Value, Actual: fmt.Sprint(s.Value),
			Message: err.Error(),
		}}}
	}
	if ok {
		return LeafResult{Verdict: VerdictTrue, Reasons: []Reason{{
			Code: "condition_met", DeviceID: s.DeviceID, Key: c.Key,
			Expected: c.Operator + " " + c.Value, Actual: fmt.Sprint(s.Value),
			Message: "条件成立",
		}}}
	}
	return LeafResult{Verdict: VerdictFalse, Reasons: []Reason{{
		Code: "condition_not_met", DeviceID: s.DeviceID, Key: c.Key,
		Expected: c.Operator + " " + c.Value, Actual: fmt.Sprint(s.Value),
		Message: "条件不成立",
	}}}
}

func matchEventParams(c Condition, s Sample) LeafResult {
	for _, f := range c.EventMatch.Fields {
		actual, ok := lookupPath(s.Params, f.Path)
		if !ok {
			return LeafResult{Verdict: VerdictFalse, Reasons: []Reason{{
				Code: "event_field_missing", DeviceID: s.DeviceID, Key: f.Path,
				Message: "事件参数中不存在该字段",
			}}}
		}
		matched, err := compareValues(f.Operator, f.Value, actual)
		if err != nil {
			code := "invalid_compare"
			var ce *CompareError
			if errors.As(err, &ce) {
				code = ce.Code
			}
			return LeafResult{Verdict: VerdictError, Reasons: []Reason{{
				Code: code, DeviceID: s.DeviceID, Key: f.Path, Message: err.Error(),
			}}}
		}
		if !matched {
			return LeafResult{Verdict: VerdictFalse, Reasons: []Reason{{
				Code: "event_field_not_matched", DeviceID: s.DeviceID, Key: f.Path,
				Expected: f.Operator + " " + f.Value, Actual: fmt.Sprint(actual),
				Message: "事件参数不满足字段条件",
			}}}
		}
	}
	return LeafResult{Verdict: VerdictTrue, Reasons: []Reason{{
		Code: "event_matched", DeviceID: s.DeviceID, Key: c.Key, Message: "事件参数全部命中",
	}}}
}

func lookupPath(data map[string]any, path string) (any, bool) {
	if data == nil {
		return nil, false
	}
	var cur any = data
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// Operators kept from the legacy model. Every one of them has a test case.
const (
	OpEq      = "="
	OpNeq     = "!="
	OpGt      = ">"
	OpLt      = "<"
	OpGte     = ">="
	OpLte     = "<="
	OpBetween = "between"
	OpIn      = "in"
	OpExists  = "exists"
)

// ValidOperators is the frozen allow list.
var ValidOperators = map[string]bool{
	OpEq: true, OpNeq: true, OpGt: true, OpLt: true,
	OpGte: true, OpLte: true, OpBetween: true, OpIn: true, OpExists: true,
}

func compareValues(op, expected string, actual any) (bool, error) {
	switch op {
	case OpExists:
		want := strings.EqualFold(strings.TrimSpace(expected), "true") || expected == "1"
		return (actual != nil) == want, nil
	case OpIn:
		for _, part := range strings.Split(expected, ",") {
			if equalValues(actual, strings.TrimSpace(part)) {
				return true, nil
			}
		}
		return false, nil
	case OpBetween:
		parts := strings.Split(expected, "-")
		if len(parts) != 2 {
			return false, &CompareError{Code: "invalid_range",
				Msg: fmt.Sprintf("between 需要 min-max 形式，实际为 %q", expected)}
		}
		lo, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		hi, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err1 != nil || err2 != nil {
			return false, &CompareError{Code: "invalid_threshold",
				Msg: fmt.Sprintf("between 边界不是数字: %q", expected)}
		}
		n, ok := toFloat(actual)
		if !ok {
			return false, &CompareError{Code: "invalid_number",
				Msg: fmt.Sprintf("实际值 %v 不是数字", actual)}
		}
		return n >= lo && n <= hi, nil
	case OpEq, OpNeq:
		eq := equalValues(actual, expected)
		if op == OpNeq {
			return !eq, nil
		}
		return eq, nil
	case OpGt, OpLt, OpGte, OpLte:
		n, ok := toFloat(actual)
		if !ok {
			return false, &CompareError{Code: "invalid_number",
				Msg: fmt.Sprintf("实际值 %v 不是数字，无法做大小比较", actual)}
		}
		want, err := strconv.ParseFloat(strings.TrimSpace(expected), 64)
		if err != nil {
			return false, &CompareError{Code: "invalid_threshold",
				Msg: fmt.Sprintf("阈值 %q 不是数字", expected)}
		}
		switch op {
		case OpGt:
			return n > want, nil
		case OpLt:
			return n < want, nil
		case OpGte:
			return n >= want, nil
		default:
			return n <= want, nil
		}
	default:
		return false, &CompareError{Code: "bad_operator",
			Msg: fmt.Sprintf("不支持的比较符 %q", op)}
	}
}

func equalValues(actual any, expected string) bool {
	if n, ok := toFloat(actual); ok {
		if e, err := strconv.ParseFloat(strings.TrimSpace(expected), 64); err == nil {
			return nearlyEqual(n, e)
		}
	}
	if b, ok := actual.(bool); ok {
		e := strings.EqualFold(strings.TrimSpace(expected), "true") || expected == "1"
		return b == e
	}
	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(actual)), strings.TrimSpace(expected))
}

func nearlyEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	return d < eps && d > -eps
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
