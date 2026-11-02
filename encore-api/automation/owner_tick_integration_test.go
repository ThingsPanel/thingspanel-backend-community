package automation

import (
	"encoding/json"
	"encore.app/automation/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This reaches the actual authenticated OwnerTick HTTP handler, Recover, Tick
// and the real PostgreSQL schedule/run/fire/outbox store used by newEnv. The
// command owner is the existing controlled test port, not a device deployment.
func TestOwnerTickHTTPExecutesDueScheduleExactlyOnce(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	schedule := e.createSchedule(rule.ID, domain.ScheduleSpec{Kind: domain.ScheduleCron, Cron: "*/5 * * * *", Timezone: "UTC"})
	e.now = *schedule.NextFireAt
	token := strings.Repeat("o", 32)
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", token)
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "deployment-a")
	svc := e.svc()
	server := httptest.NewServer(http.HandlerFunc(svc.OwnerTick))
	defer server.Close()
	call := func() struct {
		Replayed int           `json:"replayed"`
		Tick     *TickResponse `json:"tick"`
	} { t.Helper(); req, err := http.NewRequest(http.MethodPost, server.URL+"/internal/automation/tick", strings.NewReader("{}")); if err != nil {
		t.Fatal(err)
	}; req.Header.Set("Authorization", "Bearer "+token); req.Header.Set("X-Automation-Deployment", "deployment-a"); req.Header.Set("Content-Type", "application/json"); resp, err := server.Client().Do(req); if err != nil {
		t.Fatal(err)
	}; defer resp.Body.Close(); if resp.StatusCode != 200 {
		t.Fatalf("OwnerTick HTTP status=%d", resp.StatusCode)
	}; var receipt struct {
		Replayed int           `json:"replayed"`
		Tick     *TickResponse `json:"tick"`
	}; if err := json.NewDecoder(resp.Body).Decode(&receipt); err != nil || receipt.Tick == nil {
		t.Fatalf("invalid tick response: %+v %v", receipt, err)
	}; return receipt }
	first := call()
	if first.Tick.ScheduleMaterialized != 1 || first.Tick.ScheduleExecuted != 1 || len(e.runs(rule.ID)) != 1 || e.cmds.count() != 1 {
		t.Fatalf("HTTP tick did not execute real schedule once: %+v", first.Tick)
	}
	second := call()
	if second.Tick.ScheduleExecuted != 0 || len(e.runs(rule.ID)) != 1 || e.cmds.count() != 1 || len(e.fires(schedule.ID)) != 1 {
		t.Fatalf("HTTP retry duplicated real run/command: %+v", second.Tick)
	}
}
