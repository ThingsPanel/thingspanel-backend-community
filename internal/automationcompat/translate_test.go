package automationcompat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The payloads in this file are the shapes the clients actually read, taken from
// the App (app/api/modules/*.js, app/pages/alarms/index.vue) and the console
// (thingspanel-frontend-community/src/views/automation/scene-manage). A
// translation test that invents its own field names proves nothing.

func sampleRule() *rule {
	def := map[string]any{
		"targets": map[string]any{"device_ids": []string{"dev-1"}},
		"steps": []map[string]any{
			{"kind": "action", "action": "telemetry", "identifier": "switch", "value": "1"},
			{"kind": "wait", "wait": "5s"},
			{"kind": "alarm", "alarm_policy_id": "pol-1"},
		},
	}
	raw, _ := json.Marshal(def)
	return &rule{
		ID: "rule-1", Name: "门厅灯光", Kind: "scene", Enabled: true,
		Version: 3, Revision: 2, LegacyID: "legacy-1", Definition: raw,
		CreatedAt: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC),
	}
}

func TestTheSceneRowCarriesWhatTheConsoleRenders(t *testing.T) {
	row := SceneRow("legacy-1", sampleRule())

	for _, key := range []string{"id", "name", "description", "created_at", "updated_at"} {
		if _, ok := row[key]; !ok {
			t.Fatalf("the console renders %q and the row does not carry it: %v", key, row)
		}
	}
	if row["id"] != "legacy-1" {
		t.Fatalf("the row must be addressable by the id the client knows, got %v", row["id"])
	}
	if row["enabled"] != "Y" {
		t.Fatalf("the old switch is the string Y/N, got %v (%T)", row["enabled"], row["enabled"])
	}
}

func TestTheSceneDetailWarnsAboutStepsTheOldEditorCannotShow(t *testing.T) {
	row, warnings := SceneDetail("legacy-1", sampleRule())

	actions, _ := row["actions"].([]legacyAction)
	if len(actions) != 2 {
		t.Fatalf("two of the three steps are expressible, got %d: %+v", len(actions), actions)
	}
	if actions[0].ActionType != "10" || actions[0].ParamType != "telemetry" ||
		actions[0].Param != "switch" || actions[0].Value != "1" {
		t.Fatalf("the telemetry action was translated wrong: %+v", actions[0])
	}
	if actions[0].Target != "dev-1" {
		t.Fatalf("a step without an explicit ref acts on the rule's target, got %q", actions[0].Target)
	}
	if actions[1].ActionType != "30" || actions[1].Target != "pol-1" {
		t.Fatalf("the alarm action was translated wrong: %+v", actions[1])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "wait") {
		t.Fatalf("the wait step must be reported, not dropped silently: %v", warnings)
	}
	if !strings.Contains(warnings[0], "第 2 步") {
		t.Fatalf("the warning must say which step, got %q", warnings[0])
	}
}

func TestAnUnknownActionIsNotRenderedAsATelemetryWrite(t *testing.T) {
	def := map[string]any{"steps": []map[string]any{{"kind": "action", "action": "mqtt_publish"}}}
	raw, _ := json.Marshal(def)
	_, warnings := SceneDetail("legacy-1", &rule{Definition: raw})

	if len(warnings) != 1 {
		t.Fatalf("an action the old editor has no vocabulary for must be reported, got %v", warnings)
	}
}

func TestRunLogDoesNotClaimSuccessForAnUnknownOutcome(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"succeeded", "S"},
		{"failed", "F"},
		{"running", "U"},
		{"ignored", "U"},
		{"unknown", "U"},
	}
	for _, tc := range cases {
		row := RunLogRow(&run{ID: "r1", Status: tc.status})
		if row["execution_result"] != tc.want {
			t.Fatalf("status %q became %v, want %v", tc.status, row["execution_result"], tc.want)
		}
		if row["execution_result"] == "S" && tc.status != "succeeded" {
			t.Fatalf("status %q must never read as success", tc.status)
		}
	}
}

func TestTheRunSentenceNamesTheFailure(t *testing.T) {
	row := RunLogRow(&run{
		Status: "failed", ReasonCode: "command_refused",
		Steps: []runStep{{Idx: 0, Status: "failed", Detail: "设备未确认"}},
	})
	detail, _ := row["detail"].(string)
	if !strings.Contains(detail, "执行失败") || !strings.Contains(detail, "command_refused") ||
		!strings.Contains(detail, "设备未确认") {
		t.Fatalf("the old log is one line of text and it must say what happened: %q", detail)
	}
}

// TestAlarmStatusIsNotABoolean is the bug this translation exists to avoid.
//
// The old column is documented as "L 底 M中 H 高 N 正常". Writing "Y" there makes
// the App render every alarm in the neutral colour, and writing the level into a
// boolean field makes a cleared alarm look active.
func TestAlarmStatusIsNotABoolean(t *testing.T) {
	active := AlarmRow(&alarm{
		ID: "ep-1", PolicyID: "pol-1", SubjectID: "dev-1", State: "active",
		Level: "M", OpenedAt: time.Now(),
	}, "高温告警")
	if active["alarm_status"] != "M" {
		t.Fatalf("an active medium alarm is status M, got %v", active["alarm_status"])
	}
	if active["alarm_level"] != "M" {
		t.Fatalf("the level is a separate column and must stay M, got %v", active["alarm_level"])
	}
	if active["name"] != "高温告警" {
		t.Fatalf("the App prints item.name, got %v", active["name"])
	}

	cleared := time.Now()
	done := AlarmRow(&alarm{
		ID: "ep-2", PolicyID: "pol-1", SubjectID: "dev-1", State: "cleared",
		Level: "H", OpenedAt: time.Now(), ClearedAt: &cleared,
	}, "高温告警")
	if done["alarm_status"] != "N" {
		t.Fatalf("a recovered alarm is status N, got %v", done["alarm_status"])
	}
	if done["alarm_level"] != "H" {
		t.Fatalf("the level of a recovered alarm is still what it was, got %v", done["alarm_level"])
	}
}

func TestAlarmRowKeepsTheDeviceListNavigable(t *testing.T) {
	row := AlarmRow(&alarm{ID: "ep-1", SubjectID: "dev-9", State: "active", Level: "L"}, "")
	list, ok := row["alarm_device_list"].([]map[string]any)
	if !ok || len(list) != 1 || list[0]["id"] != "dev-9" {
		t.Fatalf("the App navigates by device id, got %v", row["alarm_device_list"])
	}
}

func TestPageReportsTheRealTotal(t *testing.T) {
	rows := []map[string]any{{"id": "a"}}
	body := Page(rows, 7, 1, 1)
	if body["total"] != 7 {
		t.Fatalf("total is the number of matches, not the page size: %v", body["total"])
	}
	if body["page"] != 1 || body["page_size"] != 1 {
		t.Fatalf("the page echo is what the client uses to paginate: %v", body)
	}
}

func TestAnEmptyPageIsAnEmptyArrayNotNull(t *testing.T) {
	body := Page(nil, 0, 1, 10)
	list, ok := body["list"].([]map[string]any)
	if !ok || list == nil {
		t.Fatalf("a null list breaks every v-for in the console: %v", body["list"])
	}
}

func TestEnabledFlag(t *testing.T) {
	if EnabledFlag(true) != "Y" || EnabledFlag(false) != "N" {
		t.Fatal("the old switch is Y/N")
	}
}
