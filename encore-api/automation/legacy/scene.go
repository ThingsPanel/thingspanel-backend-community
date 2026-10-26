// Package legacy maps the old ThingsPanel scene objects onto the new typed model.
//
// The point of this package is that the old names do not mean what the new ones
// mean. A "scene" in the old console is sometimes a single "put these devices in
// this state" button and sometimes an ordered list of actions that raises alarms
// and activates other scenes. Renaming both to one new object would quietly turn
// an ordered flow into an unordered state set — which is why the classification
// below is explicit and is reported to the operator instead of being inferred
// silently at migration time.
//
// Nothing here touches the database or the network: it takes a snapshot of the
// legacy rows and produces a plan. That keeps the mapping testable on its own and
// keeps the dry run (A21) and the real migration (A30) sharing one implementation.
package legacy

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"encore.app/automation/domain"
)

// Action types, exactly as the legacy scene_action_info.action_type column spells
// them. They are strings because that is what is stored; parsing them into
// integers would only add a second place to get the mapping wrong.
const (
	TypeSingleDevice  = "10" // 单个设备
	TypeDeviceConfig  = "11" // 单类设备（设备配置）
	TypeActivateScene = "20" // 激活场景
	TypeRaiseAlarm    = "30" // 触发告警
	TypeService       = "40" // 服务
)

// Parameter types of a device action.
const (
	ParamTelemetry = "TEL"
	ParamAttribute = "ATTR"
	ParamCommand   = "CMD"
)

// Action is one row of scene_action_info.
type Action struct {
	ID        string
	Target    string
	Type      string
	ParamType string
	Param     string
	Value     string
	// ContinueOnFailure is the legacy per-action "do not stop on error" switch.
	// It is carried explicitly because the new default is the opposite: a critical
	// failure stops the flow.
	ContinueOnFailure bool
}

// Scene is one legacy scene with its ordered actions.
type Scene struct {
	ID       string
	TenantID string
	Name     string
	// Actions are in the order the old console stored them. Order is part of the
	// meaning: "open the valve, then start the pump" is not the same set as
	// "start the pump, open the valve".
	Actions []Action
}

// Shape is what the scene actually is.
type Shape string

const (
	// ShapeStateScene is "put these devices into this state". It takes effect in
	// one shot and implies no supervision and no automatic rollback.
	ShapeStateScene Shape = "state_scene"
	// ShapeSequence is an ordered flow: several actions, or an action with a side
	// effect on another object (scene, alarm, service).
	ShapeSequence Shape = "sequence"
)

// Issue is one reason the scene cannot be migrated as it stands.
type Issue struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (i Issue) String() string { return i.Code + ": " + i.Message }

// StepMapping records what one legacy action became, so the operator can compare
// before and after instead of trusting a count.
type StepMapping struct {
	Index             int    `json:"index"`
	LegacyActionID    string `json:"legacy_action_id"`
	LegacyType        string `json:"legacy_type"`
	StepKind          string `json:"step_kind"`
	Target            string `json:"target"`
	ContinueOnFailure bool   `json:"continue_on_failure"`
	Note              string `json:"note,omitempty"`
}

// Plan is the migration result for one scene.
type Plan struct {
	SceneID    string            `json:"scene_id"`
	Name       string            `json:"name"`
	Shape      Shape             `json:"shape"`
	Definition domain.Definition `json:"definition"`
	Mapping    []StepMapping     `json:"mapping"`
	// ContinuedSteps lists the legacy actions whose "keep going after a failure"
	// flag was preserved. The new default stops on a critical failure, so every
	// exception is reported rather than assumed.
	ContinuedSteps []string `json:"continued_steps,omitempty"`
	Issues         []Issue  `json:"issues,omitempty"`
}

// OK reports whether the plan can be applied.
func (p *Plan) OK() bool { return len(p.Issues) == 0 }

// Resolver answers what a legacy identifier points at today. The importer never
// guesses: an id that cannot be resolved is an issue, not an empty target.
type Resolver interface {
	SceneExists(sceneID string) (bool, error)
	DeviceExists(deviceID string) (bool, error)
	DeviceConfigExists(deviceConfigID string) (bool, error)
	// AlarmPolicyFor maps a legacy alarm_config id onto the published policy.
	// policyKey is the stable business key that keeps the active alarm identity.
	AlarmPolicyFor(legacyAlarmID string) (policyID, policyKey string, ok bool, err error)
}

// Loader supplies a scene by id, used for the cycle walk.
type Loader interface {
	Load(sceneID string) (*Scene, error)
}

// MaxSceneDepth bounds how many scenes one activation chain may cross. The old
// engine had no limit, so a scene activating itself simply ran until something
// else broke.
const MaxSceneDepth = 8

// Build turns one legacy scene into a definition plus the report that explains it.
//
// It never fails on a single bad action: it collects issues so the operator sees
// everything wrong with a scene in one pass instead of fixing one row, re-running,
// and finding the next one.
func Build(scene *Scene, r Resolver) *Plan {
	p := &Plan{SceneID: scene.ID, Name: scene.Name}

	if len(scene.Actions) == 0 {
		p.addIssue("empty_scene", "actions", "场景没有任何动作，迁移后会是一个空流程")
		return p
	}

	p.Shape = classify(scene)
	def := domain.Definition{
		// Both shapes stay a scene: the entry point is still the same button, and
		// turning an ordered flow into an "automation" would move it to another
		// menu and change how it is invoked. What differs is the body — one state
		// write, or an ordered sequence — and that is what Shape records.
		Kind:  domain.KindScene,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeRule,
		// A scene has no trigger: it runs when someone presses the button.
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute),
			MaxGap: domain.Duration(10 * time.Minute),
		},
	}

	// Targets: the union of every device the actions touch. A single-device scene
	// targets exactly that device; a device-config action expands to the config so
	// the new runtime can freeze the membership at acceptance time (A12).
	var deviceIDs, configIDs []string
	seenDevice := map[string]bool{}
	seenConfig := map[string]bool{}

	for i, a := range scene.Actions {
		step, m, err := buildStep(a, r)
		if err != nil {
			p.addIssue("action_unresolved", fmt.Sprintf("actions[%d]", i),
				"动作 "+a.ID+" 无法迁移："+err.Error())
			continue
		}
		def.Steps = append(def.Steps, step)
		p.Mapping = append(p.Mapping, m)
		if a.ContinueOnFailure {
			p.ContinuedSteps = append(p.ContinuedSteps, a.ID)
		}
		switch a.Type {
		case TypeSingleDevice:
			if a.Target != "" && !seenDevice[a.Target] {
				seenDevice[a.Target] = true
				deviceIDs = append(deviceIDs, a.Target)
			}
		case TypeDeviceConfig:
			if a.Target != "" && !seenConfig[a.Target] {
				seenConfig[a.Target] = true
				configIDs = append(configIDs, a.Target)
			}
		}
	}

	sort.Strings(deviceIDs)
	sort.Strings(configIDs)
	def.Targets = domain.TargetSpec{DeviceIDs: deviceIDs, DeviceConfigIDs: configIDs}
	if len(deviceIDs) == 0 && len(configIDs) == 0 {
		p.addIssue("no_targets", "targets",
			"没有任何动作指向设备；迁移后这条规则没有可执行目标")
	}
	p.Definition = def
	return p
}

// classify decides the shape. Anything that is not a pure device state write is a
// sequence, because it either has order semantics or a side effect on another
// object — and calling that a "state scene" would hide the side effect.
func classify(scene *Scene) Shape {
	if len(scene.Actions) > 1 {
		return ShapeSequence
	}
	a := scene.Actions[0]
	switch a.Type {
	case TypeSingleDevice, TypeDeviceConfig:
		switch strings.ToUpper(a.ParamType) {
		case ParamTelemetry, ParamAttribute:
			return ShapeStateScene
		case ParamCommand:
			// A command is an operation, not a state: sending it twice is not the
			// same as setting a value twice.
			return ShapeSequence
		}
	}
	return ShapeSequence
}

func buildStep(a Action, r Resolver) (domain.Step, StepMapping, error) {
	m := StepMapping{
		LegacyActionID: a.ID, LegacyType: a.Type, Target: a.Target,
		ContinueOnFailure: a.ContinueOnFailure,
	}
	switch a.Type {
	case TypeSingleDevice, TypeDeviceConfig:
		if a.Target == "" {
			return domain.Step{}, m, fmt.Errorf("动作目标是空的")
		}
		if a.Param == "" {
			return domain.Step{}, m, fmt.Errorf("动作参数（标识符）是空的")
		}
		ok, err := existsFor(a.Type, a.Target, r)
		if err != nil {
			return domain.Step{}, m, err
		}
		if !ok {
			return domain.Step{}, m, fmt.Errorf("引用的%s %s 不存在或已失效",
				map[string]string{TypeSingleDevice: "设备", TypeDeviceConfig: "设备配置"}[a.Type], a.Target)
		}
		action := "telemetry"
		switch strings.ToUpper(a.ParamType) {
		case ParamAttribute:
			action = "attribute"
		case ParamCommand:
			action = "command"
		case ParamTelemetry:
		default:
			return domain.Step{}, m, fmt.Errorf("未知参数类型 %q", a.ParamType)
		}
		step := domain.Step{
			Kind:       domain.StepAction,
			Action:     action,
			Identifier: a.Param,
			Value:      a.Value,
			// A device-config action fans out to every device of that config, so
			// the step targets the run's target set rather than one id.
			DeviceRef:         pickRef(a.Type, a.Target),
			ContinueOnFailure: a.ContinueOnFailure,
		}
		m.StepKind = string(domain.StepAction)
		m.Note = "设备动作 " + action + " " + a.Param + "=" + a.Value
		return step, m, nil

	case TypeActivateScene:
		if a.Target == "" {
			return domain.Step{}, m, fmt.Errorf("没有指定要激活的场景")
		}
		ok, err := r.SceneExists(a.Target)
		if err != nil {
			return domain.Step{}, m, err
		}
		if !ok {
			return domain.Step{}, m, fmt.Errorf("引用的场景 %s 不存在或已失效", a.Target)
		}
		// The scene id goes in Value, which is where both the validator and the
		// executor read it. Putting it in DeviceRef (as an earlier version of
		// this mapping did) produced a step that failed validation with
		// "missing_scene" and, if it had ever been stored, would have dispatched
		// an activation for scene "".
		step := domain.Step{
			Kind: domain.StepActivateScene, Value: a.Target,
			ContinueOnFailure: a.ContinueOnFailure,
		}
		m.StepKind = string(domain.StepActivateScene)
		m.Note = "激活场景 " + a.Target
		return step, m, nil

	case TypeRaiseAlarm:
		if a.Target == "" {
			return domain.Step{}, m, fmt.Errorf("没有指定要触发的告警")
		}
		policyID, policyKey, ok, err := r.AlarmPolicyFor(a.Target)
		if err != nil {
			return domain.Step{}, m, err
		}
		if !ok {
			return domain.Step{}, m, fmt.Errorf(
				"旧告警 %s 没有对应的告警策略；请先迁移该告警，而不是在场景里另建一份",
				a.Target)
		}
		step := domain.Step{
			Kind: domain.StepAlarm, AlarmPolicyID: policyID,
			DeviceRef: "target", Value: "qualified",
			ContinueOnFailure: a.ContinueOnFailure,
		}
		m.StepKind = string(domain.StepAlarm)
		m.Note = "触发告警策略 " + policyKey + "（事件型受理，稳定动作 ID = 步骤 ID）"
		return step, m, nil

	case TypeService:
		// The old "service" action called a webhook. There is no owner for it in
		// this release, and inventing one would be a second notification path.
		return domain.Step{}, m, fmt.Errorf(
			"服务型动作（type=40）在本次范围内没有承接方；请改用通知策略或外部执行器")
	}
	return domain.Step{}, m, fmt.Errorf("未知动作类型 %q", a.Type)
}

func pickRef(actionType, target string) string {
	if actionType == TypeDeviceConfig {
		// The target set already expands the config; "target" keeps the fan-out.
		return "target"
	}
	return target
}

func existsFor(actionType, target string, r Resolver) (bool, error) {
	if actionType == TypeDeviceConfig {
		return r.DeviceConfigExists(target)
	}
	return r.DeviceExists(target)
}

func (p *Plan) addIssue(code, field, msg string) {
	p.Issues = append(p.Issues, Issue{Code: code, Field: field, Message: msg})
}

// CheckCycles walks the activation graph from root and reports the first cycle.
//
// A cycle is reported as a path, not just "there is a cycle", because the operator
// has to know which edge to cut. Depth is bounded as well: a chain long enough to
// be unreadable is a design problem even when it terminates.
func CheckCycles(root string, l Loader) (path []string, issue *Issue) {
	visiting := map[string]bool{}
	var stack []string

	var walk func(id string) *Issue
	walk = func(id string) *Issue {
		if visiting[id] {
			// Record the loop, from the first occurrence of id to here.
			start := 0
			for i, s := range stack {
				if s == id {
					start = i
					break
				}
			}
			path = append(append([]string{}, stack[start:]...), id)
			return &Issue{Code: "scene_cycle", Field: "actions",
				Message: "场景激活形成环：" + strings.Join(path, " -> ")}
		}
		if len(stack) >= MaxSceneDepth {
			path = append([]string{}, stack...)
			return &Issue{Code: "scene_depth_exceeded", Field: "actions",
				Message: fmt.Sprintf("场景激活链超过 %d 层：%s", MaxSceneDepth, strings.Join(stack, " -> "))}
		}
		visiting[id] = true
		stack = append(stack, id)
		defer func() {
			delete(visiting, id)
			stack = stack[:len(stack)-1]
		}()

		scene, err := l.Load(id)
		if err != nil || scene == nil {
			return nil // a missing scene is reported by Build, not here
		}
		for _, a := range scene.Actions {
			if a.Type != TypeActivateScene || a.Target == "" {
				continue
			}
			if iss := walk(a.Target); iss != nil {
				return iss
			}
		}
		return nil
	}
	if iss := walk(root); iss != nil {
		return path, iss
	}
	return nil, nil
}

// Summary is a tenant level report of a migration dry run.
type Summary struct {
	Total      int            `json:"total"`
	StateScene int            `json:"state_scene"`
	Sequence   int            `json:"sequence"`
	Failed     int            `json:"failed"`
	Continued  int            `json:"continued_on_failure"`
	ByIssue    map[string]int `json:"by_issue"`
}

// Summarize folds plans into the numbers an operator checks before applying.
func Summarize(plans []*Plan) *Summary {
	s := &Summary{ByIssue: map[string]int{}}
	for _, p := range plans {
		s.Total++
		switch p.Shape {
		case ShapeStateScene:
			s.StateScene++
		case ShapeSequence:
			s.Sequence++
		}
		if !p.OK() {
			s.Failed++
		}
		s.Continued += len(p.ContinuedSteps)
		for _, i := range p.Issues {
			s.ByIssue[i.Code]++
		}
	}
	return s
}
