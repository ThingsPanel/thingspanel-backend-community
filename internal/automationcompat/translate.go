package automationcompat

import (
	"encoding/json"
	"strings"
	"time"
)

// This file is the whole compatibility contract, in one place, with no I/O.
//
// Everything here converts the new read model into the shape the old clients
// already parse. Keeping it pure is not style: the adapter's risk is that a
// translation looks right and drops a field, and a pure function is the only
// kind that can be tested against the exact payloads captured from the App and
// the console (translate_test.go).

// EnabledFlag renders the old "Y"/"N" switch.
func EnabledFlag(enabled bool) string {
	if enabled {
		return "Y"
	}
	return "N"
}

// ---------------------------------------------------------------------------
// Rules → old scene / scene_automation rows
// ---------------------------------------------------------------------------

// SceneRow is one row of the old `GET /api/v1/scene` list.
//
// The console renders name, description, created_at and updated_at; the App adds
// enabled. Nothing else is read, and adding "helpful" extra fields would create
// a second contract that no client asked for.
func SceneRow(legacyID string, r *rule) map[string]any {
	return map[string]any{
		"id":          legacyID,
		"name":        r.Name,
		"description": "",
		"enabled":     EnabledFlag(r.Enabled),
		"created_at":  r.CreatedAt,
		"updated_at":  r.UpdatedAt,
	}
}

// AutomationRow is one row of the old `GET /api/v1/scene_automations/list`.
//
// The old list also carried trigger/action summaries that the App renders as a
// sentence. The new model has no pre-rendered sentence, so the row carries the
// definition and an empty summary: an empty summary is honest ("we did not
// translate this"), a made-up one is not.
func AutomationRow(legacyID string, r *rule) map[string]any {
	return map[string]any{
		"id":          legacyID,
		"name":        r.Name,
		"description": "",
		"enabled":     EnabledFlag(r.Enabled),
		"created_at":  r.CreatedAt,
		"updated_at":  r.UpdatedAt,
	}
}

// legacyAction is one row of the old scene_action_info.
type legacyAction struct {
	ID         string `json:"id"`
	ActionType string `json:"action_type"`
	Target     string `json:"action_target"`
	ParamType  string `json:"action_param_type"`
	Param      string `json:"action_param"`
	Value      string `json:"action_value"`
}

// definition is the part of the new definition the adapter can express in the
// old action list.
type definition struct {
	Targets struct {
		DeviceIDs []string `json:"device_ids"`
	} `json:"targets"`
	Steps []definitionStep `json:"steps"`
}

type definitionStep struct {
	Kind                 string `json:"kind"`
	DeviceRef            string `json:"device_ref"`
	Action               string `json:"action"`
	Identifier           string `json:"identifier"`
	Value                string `json:"value"`
	AlarmPolicyID        string `json:"alarm_policy_id"`
	NotificationPolicyID string `json:"notification_policy_id"`
}

// SceneDetail is the old `GET /api/v1/scene/detail/:id` body.
//
// The second return value is the list of things the old editor cannot show. It
// is returned to the client as `compat_warnings` instead of being swallowed:
// the App's scene editor replaces the action list wholesale on save, so a rule
// with a wait or a branch would come back shorter than it went in if the
// warning were hidden. Writes are refused for migrated objects (handler.go), so
// this is a display problem, not a data-loss one — but the operator still needs
// to know which rules cannot be edited from the phone.
func SceneDetail(legacyID string, r *rule) (map[string]any, []string) {
	row := SceneRow(legacyID, r)
	row["scene_type"] = "manually"

	var def definition
	_ = json.Unmarshal(r.Definition, &def)

	fallbackTarget := ""
	if len(def.Targets.DeviceIDs) > 0 {
		fallbackTarget = def.Targets.DeviceIDs[0]
	}

	actions := make([]legacyAction, 0, len(def.Steps))
	var warnings []string
	for i, s := range def.Steps {
		a, ok := translateStep(s, i, fallbackTarget)
		if !ok {
			warnings = append(warnings, "第 "+itoa(i+1)+" 步是「"+s.Kind+"」，旧编辑器无法表示，这里没有列出")
			continue
		}
		actions = append(actions, a)
	}
	row["actions"] = actions
	return row, warnings
}

func translateStep(s definitionStep, idx int, fallbackTarget string) (legacyAction, bool) {
	target := s.DeviceRef
	if target == "" || target == "target" {
		target = fallbackTarget
	}
	a := legacyAction{ID: "step-" + itoa(idx+1), Target: target}

	switch s.Kind {
	case "action":
		a.ActionType = "10"
		switch s.Action {
		case "telemetry":
			a.ParamType = "telemetry"
		case "attribute":
			a.ParamType = "attributes"
		case "command":
			a.ParamType = "command"
		default:
			// An action the old editor has no vocabulary for. Reporting it as a
			// telemetry write would be worse than admitting it is not shown.
			return a, false
		}
		a.Param = s.Identifier
		a.Value = s.Value
		return a, true
	case "activate_scene":
		a.ActionType = "20"
		a.Target = s.Value
		return a, true
	case "alarm":
		a.ActionType = "30"
		a.Target = s.AlarmPolicyID
		return a, true
	case "notify":
		a.ActionType = "40"
		a.Target = s.NotificationPolicyID
		return a, true
	default:
		// wait / branch / guard / anything newer than this adapter.
		return a, false
	}
}

// ---------------------------------------------------------------------------
// Runs → old execution log rows
// ---------------------------------------------------------------------------

// RunLogRow is one row of `GET /api/v1/scene_automations/log`.
//
// The old `execution_result` column had two values, S and F. The new runtime has
// more (running, ignored, unknown, …) and folding them into S or F would revive
// exactly the defect the rewrite removed: a customer reading "成功" for a run
// whose command outcome was never confirmed. "U" is the third value the old
// clients already tolerate (they print the row, they do not branch on it), and
// the readable sentence says what actually happened.
func RunLogRow(r *run) map[string]any {
	result := "U"
	switch r.Status {
	case "succeeded":
		result = "S"
	case "failed":
		result = "F"
	}
	return map[string]any{
		"id":               r.ID,
		"executed_at":      r.StartedAt,
		"execution_result": result,
		"detail":           runSentence(r),
	}
}

// runSentence is what the old log showed as one line of Chinese text.
func runSentence(r *run) string {
	var b strings.Builder
	switch r.Status {
	case "succeeded":
		b.WriteString("执行成功")
	case "failed":
		b.WriteString("执行失败")
	case "running":
		b.WriteString("执行中")
	case "ignored":
		b.WriteString("未执行")
	default:
		b.WriteString("结果未知")
	}
	if r.Reason != "" {
		b.WriteString("：")
		b.WriteString(r.Reason)
	} else if r.ReasonCode != "" {
		b.WriteString("：")
		b.WriteString(r.ReasonCode)
	}
	for _, s := range r.Steps {
		if s.Detail == "" {
			continue
		}
		b.WriteString("；第 ")
		b.WriteString(itoa(s.Idx + 1))
		b.WriteString(" 步 ")
		b.WriteString(s.Status)
		b.WriteString(" ")
		b.WriteString(s.Detail)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Alarms → old alarm_info / alarm_history rows
// ---------------------------------------------------------------------------

// AlarmRow is one row of `GET /api/v1/alarm/info` and `/alarm/info/history`.
//
// The old row is the alarm_history table plus the joined config name and level.
// Two of its columns mean something different from what their names suggest, and
// both are load-bearing for the App's colour coding:
//
//   - alarm_status is L/M/H for an active alarm and N for a recovered one. It is
//     NOT a boolean, and writing "Y" there makes every alarm render as "normal".
//   - alarm_level is the level string, and the App looks it up in its own
//     translation table, so it must stay "H"/"M"/"L" rather than "high"/"medium".
func AlarmRow(a *alarm, policyName string) map[string]any {
	level := strings.ToUpper(strings.TrimSpace(a.Level))
	if level == "" {
		level = "H"
	}
	status := level
	if a.State != "active" {
		status = "N"
	}
	content := a.LastEvidence
	if content == "" {
		content = a.Dimension
	}
	deviceList := make([]map[string]any, 0, 1)
	if a.SubjectID != "" {
		// The old row joined the device name; the new episode only knows the id.
		// An empty name is the honest value — the App navigates by id.
		deviceList = append(deviceList, map[string]any{"id": a.SubjectID, "name": ""})
	}
	row := map[string]any{
		"id":                a.ID,
		"alarm_config_id":   a.PolicyID,
		"alarm_config_name": policyName,
		"name":              policyName,
		"alarm_level":       level,
		"alarm_status":      status,
		"content":           content,
		"alarm_device_list": deviceList,
		"create_at":         a.OpenedAt,
		"description":       "",
	}
	if a.ClearedAt != nil {
		row["recovered_at"] = *a.ClearedAt
	}
	return row
}

// ---------------------------------------------------------------------------
// Pages
// ---------------------------------------------------------------------------

// Page wraps rows in the old `{total, list}` body.
//
// total is the number of matching rows, not len(list). The App dashboard asks for
// one row and prints total as a badge, so passing len(list) would print "1".
func Page(rows []map[string]any, total, page, pageSize int) map[string]any {
	if rows == nil {
		rows = []map[string]any{}
	}
	out := map[string]any{"total": total, "list": rows}
	if page > 0 {
		out["page"] = page
	}
	if pageSize > 0 {
		out["page_size"] = pageSize
	}
	return out
}

// TimeOrZero keeps the JSON shape stable when a timestamp is missing: the old
// clients format these with a date library that renders an empty string as
// "Invalid Date".
func TimeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
