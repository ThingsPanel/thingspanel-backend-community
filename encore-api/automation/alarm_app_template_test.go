package automation

import (
	"encoding/json"
	"encore.app/automation/domain"
	"testing"
)

func TestAlarmAppTemplateVariablesPersistAcrossLifecycle(t *testing.T) {
	e := newEnv(t)
	policy := e.alarmPolicy(func(d *domain.Definition) { d.Alarm.NotifyPolicyID = "app-policy"; d.Alarm.NotifyRevision = 1 })
	e.ingest("app-template-open", e.dev1, "temperature", 35.0, "")
	ep := e.activeAlarm(policy.PolicyKey(), e.dev1)
	if ep == nil {
		t.Fatal("alarm did not open")
	}
	e.ingest("app-template-clear", e.dev1, "temperature", 20.0, "")
	for _, kind := range []string{"qualified", "clear"} {
		var raw []byte
		err := e.st.DB.QueryRowContext(e.ctx(), `SELECT payload FROM auto_outbox WHERE scope_key=$1 AND payload->'variables'->>'kind'=$2`, "alarm:"+ep.ID, kind).Scan(&raw)
		if err != nil {
			t.Fatal(kind, err)
		}
		var payload struct {
			Subject   string         `json:"subject"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		v := payload.Variables
		wantSubject := "设备告警触发"
		if kind == "clear" {
			wantSubject = "设备告警恢复"
		}
		if payload.Subject != wantSubject || v["subject_id"] != ep.SubjectID {
			t.Fatalf("%s lost readable title or device identity: %+v", kind, payload)
		}
		if v["alarmId"] != ep.ID || v["alarmKind"] != "history" || v["episode_id"] != ep.ID || v["kind"] != kind {
			t.Fatalf("%s lost App template or native identity: %+v", kind, v)
		}
	}
}
