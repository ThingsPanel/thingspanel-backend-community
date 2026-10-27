package automation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/legacy"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A31: planning a migration
// ---------------------------------------------------------------------------
//
// Planning is a pure function of the snapshot the caller supplied. It touches no
// database except to resolve alarm policies, and it writes nothing. That is what
// makes the dry run trustworthy: the plan an operator reads is produced by the
// same code that will later be applied, so "the diff you approved" and "the diff
// that runs" cannot drift apart.
//
// Every old object type ends up as an item, including the ones this release
// cannot convert. An object that is simply absent from the report becomes an
// object nobody notices is missing.

// migrationSnapshot is the part of the request the approval covers.
//
// Apply is excluded on purpose: whether the first batch runs immediately is not
// part of "what was approved", and including it would make the same plan hash
// differently depending on how it was submitted.
type migrationSnapshot struct {
	Scenes        []LegacySceneInput       `json:"scenes"`
	Automations   []LegacyAutomationInput  `json:"automations"`
	AlarmConfigs  []LegacyAlarmConfigInput `json:"alarm_configs"`
	Alarms        []LegacyAlarmInput       `json:"alarms"`
	Devices       []string                 `json:"devices"`
	DeviceConfigs []string                 `json:"device_configs"`
}

// snapshotHash identifies the approved plan.
//
// Device and config lists are sorted before hashing because their order carries
// no meaning; leaving it in would make two identical snapshots hash differently
// just because the caller listed devices in another order.
func snapshotHash(req *MigrationCreateRequest) string {
	snap := migrationSnapshot{
		Scenes:        req.Scenes,
		Automations:   req.Automations,
		AlarmConfigs:  req.AlarmConfigs,
		Alarms:        req.Alarms,
		Devices:       append([]string{}, req.Devices...),
		DeviceConfigs: append([]string{}, req.DeviceConfigs...),
	}
	sort.Strings(snap.Devices)
	sort.Strings(snap.DeviceConfigs)
	body, _ := json.Marshal(snap)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// planMigration turns the snapshot into the rows a run is made of.
//
// The returned error is only for input that cannot be planned at all (a missing
// id, the same old id twice). A single unmigratable object is never an error: it
// becomes an "unsupported" item with a written reason, because one bad scene must
// not stop the other 300 from migrating.
func planMigration(migrationID string, req *MigrationCreateRequest, snap *legacySnapshot,
	policyFor func(legacyID string) (*store.Rule, error)) ([]*store.MigrationItem, error) {

	var items []*store.MigrationItem
	seen := map[string]string{}

	add := func(it *store.MigrationItem) error {
		if it.LegacyID == "" {
			return fmt.Errorf("%s 有一条记录没有 id，无法迁移", it.Kind)
		}
		if prev, ok := seen[it.LegacyID]; ok {
			// The item table is keyed by legacy id. Two objects sharing one id —
			// whether from the same table or across scene_info and alarm_info —
			// would make one of them silently overwrite the other.
			return fmt.Errorf("旧 id %q 同时出现在 %s 和 %s 里，迁移无法区分这两条记录",
				it.LegacyID, prev, it.Kind)
		}
		seen[it.LegacyID] = it.Kind
		it.MigrationID = migrationID
		items = append(items, it)
		return nil
	}

	for i := range req.Scenes {
		if err := add(planScene(&req.Scenes[i], snap)); err != nil {
			return nil, err
		}
	}
	for i := range req.Automations {
		if err := add(planAutomation(&req.Automations[i], snap)); err != nil {
			return nil, err
		}
	}
	// Alarm configurations are planned before the alarms that adopt them: the
	// policy has to exist before an active alarm can attach its identity to it.
	// The order inside this function is the plan order, and the plan is what gets
	// applied, so getting it right here is what keeps "the policy is missing"
	// from being the answer for every alarm on the first run.
	for i := range req.AlarmConfigs {
		if err := add(planAlarmConfig(&req.AlarmConfigs[i], req)); err != nil {
			return nil, err
		}
	}
	for i := range req.Alarms {
		it, err := planAlarm(&req.Alarms[i], policyFor)
		if err != nil {
			return nil, err
		}
		if err := add(it); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// planScene classifies and validates one old scene.
func planScene(in *LegacySceneInput, snap *legacySnapshot) *store.MigrationItem {
	it := &store.MigrationItem{
		LegacyID: in.ID, Kind: store.MigrationKindScene, Name: in.Name,
		Status: store.MigrationPlanned,
	}
	plan := legacy.Build(toLegacyScene(in), snap)
	// A scene that activates other scenes can still loop even when every single
	// action resolves. Check the graph, not just the rows. CheckCycles already
	// puts the path in the message, so it is appended as-is rather than re-wrapped.
	if _, iss := legacy.CheckCycles(in.ID, snap); iss != nil {
		plan.Issues = append(plan.Issues, *iss)
	}
	if len(plan.Issues) > 0 {
		it.Status = store.MigrationUnsupported
		it.ReasonCode = plan.Issues[0].Code
		it.Reason = joinIssues(plan.Issues)
		return it
	}
	def := plan.Definition
	// Validate normalizes in place, so the definition that is hashed here is the
	// same one CreateRule would have stored. Hashing the un-normalized body would
	// make every migrated rule look "changed" on the very first verify.
	if err := domain.Validate(&def); err != nil {
		it.Status = store.MigrationUnsupported
		it.ReasonCode = "definition_invalid"
		it.Reason = err.Error()
		return it
	}
	it.Definition = def
	it.HasDefinition = true
	return it
}

func joinIssues(issues []legacy.Issue) string {
	parts := make([]string, 0, len(issues))
	for _, i := range issues {
		parts = append(parts, i.Message)
	}
	return strings.Join(parts, "；")
}

// planAutomation converts one old automation, or says why it cannot be.
//
// A31 shipped this as "always unsupported" on a false premise: that the operator
// column held a number with no consumer. It holds a string, and the old evaluator
// consumes it, so the mapping in legacy.BuildAutomation is a translation rather
// than an inference. What remains here is the same shape as planScene: collect
// every blocker, write them per row, and only refuse the individual object.
func planAutomation(in *LegacyAutomationInput, snap *legacySnapshot) *store.MigrationItem {
	it := &store.MigrationItem{
		LegacyID: in.ID, Kind: store.MigrationKindAutomation, Name: in.Name,
		Status: store.MigrationPlanned, Enabled: in.Enabled,
	}

	plan := legacy.BuildAutomation(toLegacyAutomation(in), snap)
	if !plan.OK() {
		it.Status = store.MigrationUnsupported
		it.ReasonCode = plan.Issues[0].Code
		it.Reason = joinIssues(plan.Issues)
		return it
	}
	def := plan.Definition
	// Validate normalizes in place, so the definition that is hashed here is the
	// same one CreateRule would store. Hashing the un-normalized body would make
	// every migrated rule look "changed" on the very first verify.
	if err := domain.Validate(&def); err != nil {
		it.Status = store.MigrationUnsupported
		it.ReasonCode = "definition_invalid"
		it.Reason = err.Error()
		return it
	}
	it.Definition = def
	it.HasDefinition = true
	return it
}

// planAlarmConfig turns one old alarm configuration into an event-only policy.
//
// The old table has no condition, so there is nothing to evaluate and nothing to
// guess: the policy declares the event channel and the callers that the old data
// shows raising it. An active alarm can then attach to it by identity, which is
// the whole reason this has to be planned before the alarms.
func planAlarmConfig(in *LegacyAlarmConfigInput, req *MigrationCreateRequest) *store.MigrationItem {
	it := &store.MigrationItem{
		LegacyID: in.ID, Kind: store.MigrationKindAlarmConfig, Name: in.Name,
		Status: store.MigrationPlanned,
	}
	if in.Enabled != nil && !*in.Enabled {
		// Migrating a switched-off configuration as an enabled policy would turn
		// something the customer turned off back on. It is skipped, with the
		// instruction, rather than silently re-enabled.
		it.Status = store.MigrationSkipped
		it.ReasonCode = "alarm_config_disabled"
		it.Reason = "旧告警配置已停用（enabled=N），不迁移；需要保留请先在旧界面启用再迁移"
		return it
	}

	plan := legacy.BuildAlarmConfig(toLegacyAlarmConfig(in), alarmCallersFor(in, req))
	if !plan.OK() {
		it.Status = store.MigrationUnsupported
		it.ReasonCode = plan.Issues[0].Code
		it.Reason = joinIssues(plan.Issues)
		return it
	}
	def := plan.Definition
	if err := domain.Validate(&def); err != nil {
		it.Status = store.MigrationUnsupported
		it.ReasonCode = "definition_invalid"
		it.Reason = err.Error()
		return it
	}
	it.Definition = def
	it.HasDefinition = true
	if in.NotifyGroupID != "" {
		// Said per row rather than once in a document: the group is the one thing
		// this migration cannot carry across, and an operator reading a green
		// report would otherwise assume notifications still work.
		it.Reason = "已建为事件型告警策略；旧通知组 " + in.NotifyGroupID +
			" 未自动绑定，需要在新策略里重新选择通知组"
	}
	return it
}

// alarmCallersFor derives who raises one old alarm configuration.
//
// Two sources, merged: the callers the snapshot states explicitly (which is how
// the upgrade script supplies them, because the scenes are in a later pass) and
// the "触发告警" actions of the scenes and automations that happen to be in the
// same snapshot. Neither is invented — a caller that is not in the old data is
// not added, and one that is there is not dropped.
func alarmCallersFor(in *LegacyAlarmConfigInput, req *MigrationCreateRequest) []string {
	callers := append([]string{}, in.Callers...)
	for _, sc := range req.Scenes {
		for _, a := range sc.Actions {
			if a.Type == legacy.TypeRaiseAlarm && a.Target == in.ID {
				callers = append(callers, "scene:"+sc.ID)
				break
			}
		}
	}
	for _, au := range req.Automations {
		for _, a := range au.Actions {
			if a.Type == legacy.TypeRaiseAlarm && a.Target == in.ID {
				callers = append(callers, "automation:"+au.ID)
				break
			}
		}
	}
	return callers
}

// toLegacyAutomation converts the wire type into the planner's input.
func toLegacyAutomation(in *LegacyAutomationInput) *legacy.Automation {
	out := &legacy.Automation{ID: in.ID, Name: in.Name}
	// Absent means "the caller did not say", and the safe reading of that is
	// "leave it on": arriving disabled would silently stop a rule the customer
	// never turned off.
	out.Enabled = in.Enabled == nil || *in.Enabled
	for _, c := range in.Conditions {
		out.Conditions = append(out.Conditions, legacy.TriggerCondition{
			ID: c.ID, GroupID: c.GroupID, Type: c.Type, Source: c.Source,
			ParamType: c.ParamType, Param: c.Param, Operator: c.Operator, Value: c.Value,
		})
	}
	for _, a := range in.Actions {
		out.Actions = append(out.Actions, legacy.Action{
			ID: a.ID, Target: a.Target, Type: a.Type, ParamType: a.ParamType,
			Param: a.Param, Value: a.Value, ContinueOnFailure: a.ContinueOnFailure,
		})
	}
	return out
}

func toLegacyAlarmConfig(in *LegacyAlarmConfigInput) *legacy.AlarmConfig {
	return &legacy.AlarmConfig{
		ID: in.ID, Name: in.Name, Level: in.Level, NotifyGroupID: in.NotifyGroupID,
	}
}

// planAlarm decides whether one old active alarm can be adopted.
func planAlarm(in *LegacyAlarmInput, policyFor func(string) (*store.Rule, error)) (*store.MigrationItem, error) {
	it := &store.MigrationItem{
		LegacyID: in.ID, Kind: store.MigrationKindAlarm, Status: store.MigrationPlanned,
	}
	status := strings.ToUpper(strings.TrimSpace(in.Status))

	switch status {
	case "N":
		// Recovered. Importing it would resurrect an alarm the customer already
		// dealt with, and the new system has no "already recovered" state to put
		// it in without inventing a recovery that never happened.
		it.Status = store.MigrationSkipped
		it.ReasonCode = "alarm_already_recovered"
		it.Reason = "旧告警已恢复（alarm_status=N），不导入"
		return it, nil
	case "L", "M", "H":
	default:
		it.Status = store.MigrationUnsupported
		it.ReasonCode = "alarm_status_unrecognised"
		it.Reason = fmt.Sprintf("旧告警状态是 %q，不是 L/M/H（活动）或 N（已恢复）之一", in.Status)
		return it, nil
	}

	if strings.TrimSpace(in.Subject) == "" {
		it.Status = store.MigrationUnsupported
		it.ReasonCode = "alarm_subject_missing"
		it.Reason = "旧告警没有主体（设备 id），无法确定它属于谁"
		return it, nil
	}
	if strings.TrimSpace(in.PolicyLegacyID) == "" {
		it.Status = store.MigrationUnsupported
		it.ReasonCode = "alarm_policy_missing"
		it.Reason = "旧告警没有关联的告警配置，无法确定它属于哪条策略"
		return it, nil
	}

	rule, err := policyFor(in.PolicyLegacyID)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		it.Status = store.MigrationUnsupported
		it.ReasonCode = "alarm_policy_not_migrated"
		it.Reason = "告警配置 " + in.PolicyLegacyID +
			" 还没有迁移过来；先迁移策略，否则会为同一个问题再开一个告警"
		return it, nil
	}

	openedAt := time.Now().UTC()
	if s := strings.TrimSpace(in.OpenedAt); s != "" {
		t, perr := time.Parse(time.RFC3339, s)
		if perr != nil {
			it.Status = store.MigrationUnsupported
			it.ReasonCode = "alarm_opened_at_invalid"
			it.Reason = fmt.Sprintf("旧告警的开始时间 %q 不是 RFC3339 格式", in.OpenedAt)
			return it, nil
		}
		openedAt = t.UTC()
	}

	it.Name = rule.Name
	it.Alarm = &store.AlarmAdoption{
		PolicyLegacyID: in.PolicyLegacyID,
		PolicyID:       rule.ID,
		PolicyKey:      rule.PolicyKey(),
		Subject:        in.Subject,
		Dimension:      in.Dimension,
		Level:          rule.AlarmLevel(),
		LegacyLevel:    strings.ToUpper(strings.TrimSpace(in.Level)),
		OpenedAt:       openedAt.Format(time.RFC3339),
		LegacyStatus:   status,
	}
	return it, nil
}
