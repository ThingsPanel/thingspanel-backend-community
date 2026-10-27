package legacy

// This file maps the old 联动 (scene_automations + device_trigger_condition +
// action_info) onto the new typed model.
//
// It exists because A31 first shipped automations as "inventory only": every row
// was recorded as unsupported with a reason. That was wrong, and the reason given
// was wrong too — it claimed the operator column held a number with no consumer
// in the community repository. Both halves are false:
//
//   - internal/model/device_trigger_condition.http.go spells the operators as the
//     strings "=", "!=", ">", "<", ">=", "<=", "between", "in" — exactly the set
//     domain.ValidOperators already accepts;
//   - internal/service/broker_automation.go and automate_telemetry.go read
//     trigger_operator and pass it straight to automateConditionCheckByOperator,
//     so the old engine really does execute these rows.
//
// So the mapping below is a translation of code that exists, not an inference.
// Where it still refuses to convert, the blocker is a real structural difference
// and it is written per row instead of being summarised.

import (
	"fmt"
	"strings"
	"time"

	"encore.app/automation/domain"
)

// Trigger condition types, exactly as trigger_condition_type spells them.
//
// The generated column comment says "2" for the time range, but the constant the
// old engine actually compares against is "22" (device_trigger_condition.http.go)
// and the stored values carry the "137|HH:mm:ss+00:00|HH:mm:ss+00:00" shape that
// comment describes. Reading "2" here would silently classify every time-range
// trigger as unknown.
const (
	CondSingleDevice = "10"
	CondDeviceConfig = "11"
	CondTimeRange    = "22"
)

// Trigger parameter types that the scene mapping does not already declare.
const (
	ParamEvent  = "EVT"
	ParamStatus = "STATUS"
)

// TriggerCondition is one row of device_trigger_condition.
type TriggerCondition struct {
	ID      string
	GroupID string
	Type    string
	Source  string
	// ParamType is TEL / ATTR / EVT / STATUS.
	ParamType string
	// Param is the identifier for TEL/ATTR/EVT. For STATUS it is the state
	// itself ("ON-LINE" / "OFF-LINE" / "ALL"), which is why it is not simply
	// copied into Condition.Key.
	Param    string
	Operator string
	Value    string
}

// Automation is one legacy 联动 with its triggers and its actions.
type Automation struct {
	ID       string
	TenantID string
	Name     string
	// Enabled is scene_automations.enabled ("Y"/"N") already decoded. A
	// disabled automation is still converted; it is migrated disabled rather
	// than dropped, because "off today" is a decision the customer made and not
	// a reason to lose the definition.
	Enabled    bool
	Conditions []TriggerCondition
	Actions    []Action
}

// AutomationPlan is the migration result for one automation.
type AutomationPlan struct {
	ID         string
	Name       string
	Definition domain.Definition
	Mapping    []StepMapping
	Issues     []Issue
}

// OK reports whether the plan can be applied.
func (p *AutomationPlan) OK() bool { return len(p.Issues) == 0 }

func (p *AutomationPlan) addIssue(code, field, msg string) {
	p.Issues = append(p.Issues, Issue{Code: code, Field: field, Message: msg})
}

// condError carries the machine readable code next to the message, so a refusal
// names both the reason code the API reports and the sentence a person reads.
type condError struct {
	code string
	msg  string
}

func (e *condError) Error() string { return e.msg }

func refuse(code, format string, args ...any) *condError {
	return &condError{code: code, msg: fmt.Sprintf(format, args...)}
}

// BuildAutomation turns one legacy automation into a definition plus the report
// that explains it.
//
// It never fails on a single bad condition: every problem is collected so the
// operator sees the whole row at once instead of fixing one field, re-running,
// and finding the next.
func BuildAutomation(a *Automation, r Resolver) *AutomationPlan {
	p := &AutomationPlan{ID: a.ID, Name: a.Name}

	if len(a.Conditions) == 0 {
		p.addIssue("empty_trigger", "conditions", "联动没有触发条件，无法判断它什么时候执行")
		return p
	}

	// group_id is the AND group; the groups themselves are OR-ed. Both orders are
	// preserved rather than sorted, because that is the order the old console
	// showed the author.
	var order []string
	byGroup := map[string][]TriggerCondition{}
	for _, c := range a.Conditions {
		g := c.GroupID
		if g == "" {
			g = "default"
		}
		if _, seen := byGroup[g]; !seen {
			order = append(order, g)
		}
		byGroup[g] = append(byGroup[g], c)
	}

	condDevices := map[string]bool{}
	condConfigs := map[string]bool{}
	var trig domain.Trigger

	for _, gid := range order {
		g := domain.Group{ID: gid}
		for i, c := range byGroup[gid] {
			cond, err := buildCondition(c, r, condDevices, condConfigs)
			if err != nil {
				code, msg := "trigger_unresolved", err.Error()
				if ce, ok := err.(*condError); ok {
					code = ce.code
				}
				p.addIssue(code, fmt.Sprintf("conditions[%s][%d]", gid, i), msg)
				continue
			}
			g.Conditions = append(g.Conditions, cond)
		}
		if len(g.Conditions) > 0 {
			trig.Groups = append(trig.Groups, g)
		}
	}
	if len(trig.Groups) == 0 {
		return p
	}

	def := domain.Definition{
		Kind:  domain.KindAutomation,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		// The old engine fired on the message it had just received, so it is
		// never acting on a stale sample. The window is kept generous (the same
		// ten minutes the scene mapping uses) because refusing a fresh-looking
		// sample the old engine would have acted on is the change that would be
		// noticed first.
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Trigger: trig,
	}

	actionDevices := map[string]bool{}
	actionConfigs := map[string]bool{}
	for i, act := range a.Actions {
		step, m, err := buildStep(act, r)
		if err != nil {
			p.addIssue("action_unresolved", fmt.Sprintf("actions[%d]", i),
				"动作 "+act.ID+" 无法迁移："+err.Error())
			continue
		}
		def.Steps = append(def.Steps, step)
		p.Mapping = append(p.Mapping, m)
		switch act.Type {
		case TypeSingleDevice:
			if act.Target != "" {
				actionDevices[act.Target] = true
			}
		case TypeDeviceConfig:
			if act.Target != "" {
				actionConfigs[act.Target] = true
			}
		}
	}
	if len(def.Steps) == 0 {
		p.addIssue("empty_steps", "actions", "联动没有任何可执行的动作")
		return p
	}

	p.Definition = def
	applyTargets(p, &def, condDevices, condConfigs, actionDevices, actionConfigs)
	p.Definition = def
	return p
}

// applyTargets decides which set of devices a run of this rule is for.
//
// This is the part that cannot be guessed. In the old model a trigger on a device
// config meant "any device of that config", and the new model expresses the same
// thing as a target set of the config plus a condition on "self". That only works
// while the trigger and the actions agree on which set is in play: mixing an
// explicit device trigger with a config-wide action would fan the action out to
// devices the trigger never selected, which is a behaviour change nobody asked
// for. Those rows are refused with the reason written out.
func applyTargets(p *AutomationPlan, def *domain.Definition,
	condDevices, condConfigs, actionDevices, actionConfigs map[string]bool) {

	switch {
	case len(condConfigs) > 0 && len(condDevices) > 0:
		p.addIssue("mixed_trigger_sources", "conditions",
			"触发条件同时用了「单个设备」和「单类设备」；新模型里这两种是两种目标集，不能放进同一条规则")
		return
	case len(condConfigs) > 1:
		p.addIssue("multiple_config_triggers", "conditions",
			fmt.Sprintf("触发条件引用了 %d 个设备配置；一条规则只能有一个目标集", len(condConfigs)))
		return
	case len(condConfigs) == 1:
		var only string
		for c := range condConfigs {
			only = c
		}
		for c := range actionConfigs {
			if c != only {
				p.addIssue("action_config_differs_from_trigger", "actions",
					"动作作用在设备配置 "+c+" 上，而触发条件是设备配置 "+only+
						"；动作的「单类设备」在新模型里就是运行目标集，两者必须一致")
				return
			}
		}
		def.Targets.DeviceConfigIDs = []string{only}
		return
	}

	// Device triggers: the run belongs to the device that produced the event, so
	// both the condition devices and the action devices are targets. A config-wide
	// action cannot be expressed here — there is no set to fan out to.
	if len(actionConfigs) > 0 {
		names := make([]string, 0, len(actionConfigs))
		for c := range actionConfigs {
			names = append(names, c)
		}
		p.addIssue("action_config_needs_config_trigger", "actions",
			"动作作用在设备配置 ["+strings.Join(names, ", ")+
				"] 上，但触发条件是单个设备；新模型里「整类设备动作」需要一个目标集，请改用设备配置触发")
		return
	}
	ids := make([]string, 0, len(condDevices)+len(actionDevices))
	for d := range condDevices {
		ids = append(ids, d)
	}
	for d := range actionDevices {
		if !condDevices[d] {
			ids = append(ids, d)
		}
	}
	def.Targets.DeviceIDs = ids
}

// buildCondition maps one device_trigger_condition row.
func buildCondition(c TriggerCondition, r Resolver,
	devices, configs map[string]bool) (domain.Condition, error) {

	switch c.Type {
	case CondTimeRange:
		// A time range is not a condition on a device; it is when the rule is
		// allowed to run. The new model has schedules (A07) for that, and copying
		// it into a trigger group would turn "only between 08:00 and 18:00" into
		// "whenever the clock reads that" with no device involved.
		return domain.Condition{}, refuse("time_range_trigger_needs_schedule",
			"触发条件是「时间范围」(type=22，取值 %q)，新模型属于排程，不是触发组；请在排程里重建", c.Value)
	case CondSingleDevice, CondDeviceConfig:
	default:
		return domain.Condition{}, refuse("unknown_trigger_type",
			"未知触发条件类型 %q", c.Type)
	}

	if strings.TrimSpace(c.Source) == "" {
		return domain.Condition{}, refuse("trigger_source_missing",
			"触发条件没有指定设备或设备配置")
	}

	sig, ok := signalFor(c.ParamType)
	if !ok {
		return domain.Condition{}, refuse("unsupported_trigger_param_type",
			"不支持的触发参数类型 %q（只能是 TEL/ATTR/EVT/STATUS）", c.ParamType)
	}

	cond := domain.Condition{Signal: sig}
	switch c.Type {
	case CondSingleDevice:
		exists, err := r.DeviceExists(c.Source)
		if err != nil {
			return domain.Condition{}, err
		}
		if !exists {
			return domain.Condition{}, refuse("trigger_device_missing",
				"触发条件引用的设备 %s 不存在或已失效", c.Source)
		}
		// An explicit device, not "self": the old row named the device that must
		// report, and reading it as "whichever device this run is for" would
		// widen the rule.
		cond.Source = "device"
		cond.DeviceID = c.Source
		devices[c.Source] = true
	case CondDeviceConfig:
		exists, err := r.DeviceConfigExists(c.Source)
		if err != nil {
			return domain.Condition{}, err
		}
		if !exists {
			return domain.Condition{}, refuse("trigger_config_missing",
				"触发条件引用的设备配置 %s 不存在或已失效", c.Source)
		}
		cond.Source = "self"
		configs[c.Source] = true
	}

	switch sig {
	case domain.SignalStatus:
		// The old engine reads the login state, ignores the stored operator, and
		// treats "ALL" as "any transition". The new model has no "any", so only
		// the two explicit states convert.
		switch strings.ToUpper(strings.TrimSpace(c.Param)) {
		case "ON-LINE", "ONLINE":
			cond.Key, cond.Operator, cond.Value = "online", domain.OpEq, "1"
		case "OFF-LINE", "OFFLINE":
			cond.Key, cond.Operator, cond.Value = "online", domain.OpEq, "0"
		case "ALL":
			return domain.Condition{}, refuse("status_any_transition_unsupported",
				"状态条件用的是「全部」(ALL)，即任意上下线变化；新模型没有这个条件，请人工重建")
		default:
			return domain.Condition{}, refuse("status_value_unrecognised",
				"状态条件的取值 %q 不是 ON-LINE/OFF-LINE/ALL", c.Param)
		}

	case domain.SignalEvent:
		// The old event condition may carry a field-level match config in
		// trigger_value (automateEventParamConditionCheck unmarshals it). That is
		// the new EventMatch, and converting it needs the field paths, so it is
		// reported rather than flattened into a string comparison that would
		// match everything.
		if strings.HasPrefix(strings.TrimSpace(c.Value), "{") {
			return domain.Condition{}, refuse("event_field_match_unsupported",
				"事件条件用的是字段级匹配配置，需要人工按字段重建")
		}
		if strings.TrimSpace(c.Param) == "" {
			return domain.Condition{}, refuse("trigger_param_missing",
				"事件条件没有指定事件标识符")
		}
		cond.Key, cond.Operator, cond.Value = c.Param, domain.OpEq, c.Value

	default:
		if strings.TrimSpace(c.Param) == "" {
			return domain.Condition{}, refuse("trigger_param_missing",
				"触发条件没有指定指标标识符")
		}
		op := strings.TrimSpace(c.Operator)
		if op == "" {
			// The old engine defaults a missing operator to "="
			// (automate_telemetry.go: if cond.TriggerOperator == nil).
			op = domain.OpEq
		}
		if !domain.ValidOperators[op] {
			return domain.Condition{}, refuse("unsupported_operator",
				"比较符 %q 不在新模型支持的范围内", op)
		}
		cond.Key, cond.Operator, cond.Value = c.Param, op, c.Value
	}
	return cond, nil
}

// signalFor maps the legacy parameter type onto the new signal kind.
func signalFor(paramType string) (domain.SignalKind, bool) {
	switch strings.ToUpper(strings.TrimSpace(paramType)) {
	case ParamTelemetry, "TELEMETRY":
		return domain.SignalTelemetry, true
	case ParamAttribute, "ATTRIBUTES":
		return domain.SignalAttribute, true
	case ParamEvent, "EVENT":
		return domain.SignalEvent, true
	case ParamStatus:
		return domain.SignalStatus, true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Alarm configurations
// ---------------------------------------------------------------------------

// AlarmConfig is one row of alarm_config.
//
// It carries no condition: the community schema has name, description, level,
// notification group and an enabled flag, and nothing else. The old alarms were
// raised either by a 联动/场景 "触发告警" action or by an outside caller. So the
// truthful new object is an alarm policy whose only intake is the event channel —
// not a detector policy with an invented threshold.
type AlarmConfig struct {
	ID       string
	TenantID string
	Name     string
	// Level is H/M/L, the same vocabulary the new AlarmSpec keeps.
	Level string
	// NotifyGroupID is the old notification_groups id. It is deliberately not
	// copied into AlarmSpec.NotifyPolicyID: the notification owner has its own
	// policy ids, and writing a foreign id there would produce a policy that
	// looks wired up and sends nothing.
	NotifyGroupID string
}

// BuildAlarmConfig turns one old alarm configuration into an event-only policy.
//
// callers are the identities allowed to raise it, derived from the 联动 and 场景
// rows whose "触发告警" action points at this configuration. They are the old
// system's equivalent of the new allowed_sources list, and they are derived
// rather than guessed: a caller that is not in the old data is not invented.
func BuildAlarmConfig(c *AlarmConfig, callers []string) *AutomationPlan {
	p := &AutomationPlan{ID: c.ID, Name: c.Name}

	level := strings.ToUpper(strings.TrimSpace(c.Level))
	switch level {
	case "H", "M", "L":
	case "":
		// The column is nullable. An alarm with no grade is not "medium"; it is
		// a row whose grade was never set, and picking one would change how it
		// is filtered and displayed.
		p.addIssue("alarm_level_missing", "level", "旧告警配置没有告警等级，无法确定迁移后的等级")
		return p
	default:
		p.addIssue("alarm_level_unrecognised", "level",
			fmt.Sprintf("旧告警等级是 %q，不是 H/M/L 之一", c.Level))
		return p
	}

	allowed := normaliseCallers(callers)
	def := domain.Definition{
		Kind:  domain.KindAlarm,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
		Alarm: &domain.AlarmSpec{
			// The key is what keeps an adopted active alarm attached to this
			// policy across re-publishing and re-running the migration. Deriving
			// it from the old id is what makes "the same alarm" a fact.
			Key:            "legacy-alarm-" + c.ID,
			Sources:        []domain.AlarmSource{domain.AlarmSourceEvent},
			AllowedSources: allowed,
			Level:          level,
		},
		// No trigger groups: an event-only policy is never evaluated by the
		// detector (submitDetectorVerdict refuses it before reading the trigger),
		// and the old row had no condition to convert. Validation allows the
		// empty trigger for exactly this shape.
	}
	p.Definition = def
	return p
}

// normaliseCallers sorts and de-duplicates the derived caller list.
//
// A configuration nothing references still needs a policy, because its active
// alarms have to attach to something. Such a policy gets the explicit caller
// "legacy-import" — a name that grants exactly one thing, and only to a caller
// that identifies itself as such. It is not a wildcard.
func normaliseCallers(callers []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(callers)+1)
	for _, c := range callers {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		out = append(out, "legacy-import")
	}
	return out
}
