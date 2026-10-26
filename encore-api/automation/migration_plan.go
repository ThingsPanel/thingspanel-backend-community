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
	Scenes        []LegacySceneInput      `json:"scenes"`
	Automations   []LegacyAutomationInput `json:"automations"`
	Alarms        []LegacyAlarmInput      `json:"alarms"`
	Devices       []string                `json:"devices"`
	DeviceConfigs []string                `json:"device_configs"`
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
		if err := add(planAutomation(&req.Automations[i])); err != nil {
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

// planAutomation writes the disposition for one old automation.
//
// A31 does not convert automations. The reason is recorded per row rather than
// stated once in a document, because "N 条联动未迁移" is not something an operator
// can act on. Each of the three blockers below needs a different human decision:
//
//   - the old trigger_operator is a number (1..8) with no consumer in the
//     community repository, so mapping it is an inference. Reading ">=" as ">"
//     silently changes what a customer's automation does.
//   - a device-config trigger (type 11) means "any device of this config", which
//     the new model expresses as a template rule plus a binding, not a condition.
//   - a time-range trigger (type 2) belongs to a schedule, not a trigger group.
func planAutomation(in *LegacyAutomationInput) *store.MigrationItem {
	it := &store.MigrationItem{
		LegacyID: in.ID, Kind: store.MigrationKindAutomation, Name: in.Name,
		Status: store.MigrationUnsupported, ReasonCode: "automation_trigger_unverified",
	}

	var blockers []string
	ops := map[string]bool{}
	types := map[string]int{}
	for _, c := range in.Conditions {
		types[c.Type]++
		if c.Operator != "" {
			ops[c.Operator] = true
		}
	}
	if len(in.Conditions) == 0 {
		blockers = append(blockers, "没有触发条件，无法判断它原本什么时候执行")
	}
	if n := types["11"]; n > 0 {
		blockers = append(blockers, fmt.Sprintf(
			"有 %d 条触发条件是「单类设备」，新模型要用模板绑定（A19）表达，不是一条条件", n))
	}
	if n := types["2"]; n > 0 {
		blockers = append(blockers, fmt.Sprintf(
			"有 %d 条触发条件是「时间范围」，新模型属于排程（A07），不是触发组", n))
	}
	if n := types["10"]; n > 0 {
		keys := make([]string, 0, len(ops))
		for op := range ops {
			keys = append(keys, op)
		}
		sort.Strings(keys)
		blockers = append(blockers, fmt.Sprintf(
			"有 %d 条设备触发条件，用到的运算符是 %s；旧库把运算符存成数字，社区版代码里没有可核对的映射依据，直接推断会把 >= 读成 > 而不报错",
			n, "["+strings.Join(keys, ", ")+"]"))
	}
	if len(blockers) == 0 {
		blockers = append(blockers, "触发条件为空或类型未知")
	}
	it.Reason = "未转换：" + strings.Join(blockers, "；") +
		"。处理方式：在联动页面按原意重建一条，或先确认旧库运算符含义后补迁移规则"
	return it
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
