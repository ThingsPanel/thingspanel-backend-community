package automation

import (
	"errors"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
)

func TestTriggerScopeTemplateSeparatesSubjectsFromActionTargets(t *testing.T) {
	e := newEnv(t)
	other := "other-" + runtime.NewID()
	e.dir.devices[other] = ports.Device{ID: other, TenantID: e.tenant, DeviceConfigID: "other-template"}
	d := e.baseDefinition()
	d.TriggerScope = &domain.TriggerScope{DeviceConfigIDs: []string{"cfg-1"}}
	d.Targets = domain.TargetSpec{DeviceIDs: []string{e.dev2}}
	rule := e.createRule(d)
	// Even an explicit binding to a different subject cannot bypass the scope.
	e.bind(rule, "device", other, nil)
	for _, device := range []string{other, e.dev1, e.dev2, other} {
		e.ingest(runtime.NewID(), device, "temperature", 40.0, "")
	}
	if runs := e.runs(rule.ID); len(runs) != 2 {
		t.Fatalf("target template should have exactly two runs, got %d", len(runs))
	}
	if e.cmds.count() != 2 {
		t.Fatalf("command count=%d", e.cmds.count())
	}
	for _, call := range e.cmds.calls {
		if call.DeviceID != e.dev2 {
			t.Fatalf("action target changed to trigger subject: %#v", call)
		}
	}
	sim, err := e.rt.Simulate(e.ctx(), e.tenant, rule.ID, []runtime.SimSample{{At: "0s", DeviceID: other, Key: "temperature", Value: 40.0}})
	if err != nil || sim.WouldRun != 0 {
		t.Fatalf("out of scope simulation: %+v %v", sim, err)
	}
	explained, err := e.rt.Explain(e.ctx(), rule, other, time.Time{}, time.Time{})
	if err != nil || len(explained.Reasons) == 0 || explained.Reasons[0].Code != "trigger_scope_mismatch" {
		t.Fatalf("out of scope explanation: %+v %v", explained, err)
	}
}

func TestTriggerScopeOmittedPreservesCrossDeviceRules(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition()) // target dev1, trigger dev2
	e.ingest(runtime.NewID(), e.dev2, "temperature", 40.0, "")
	call, ok := e.cmds.last()
	if !ok || call.DeviceID != e.dev1 || len(e.runs(rule.ID)) != 1 {
		t.Fatalf("unscoped cross-device rule changed: %+v", call)
	}
}

func TestTriggerScopeEffectiveDeviceListMatchesRuntimeScope(t *testing.T) {
	e := newEnv(t)
	d := e.baseDefinition()
	d.TriggerScope = &domain.TriggerScope{DeviceConfigIDs: []string{"cfg-1"}}
	rule := e.createRuleIn(serviceTestTenant, d)
	for _, tc := range []struct {
		template string
		want     bool
	}{{"cfg-1", true}, {"other", false}} {
		e.dir.devices[e.dev1] = ports.Device{ID: e.dev1, TenantID: serviceTestTenant, DeviceConfigID: tc.template}
		response, err := e.svc().GetDeviceAutomation(e.ctx(), e.dev1)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range response.Rules {
			if row.RuleID == rule.ID {
				found = true
			}
		}
		if found != tc.want {
			t.Fatalf("template=%s effective=%v want=%v", tc.template, found, tc.want)
		}
	}
}

func TestTriggerScopeRechecksTemplateBeforePendingDeadline(t *testing.T) {
	e := newEnv(t)
	d := e.baseDefinition()
	d.TriggerScope = &domain.TriggerScope{DeviceConfigIDs: []string{"cfg-1"}}
	d.Trigger.For = domain.Duration(10 * time.Second)
	rule := e.createRule(d)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 40.0, "")
	device := e.dir.devices[e.dev1]
	device.DeviceConfigID = "moved-template"
	e.dir.devices[e.dev1] = device
	e.advance(11 * time.Second)
	e.tick()
	if len(e.runs(rule.ID)) != 0 || e.cmds.count() != 0 {
		t.Fatal("pending timer fired after subject moved out of template")
	}
}

func TestTriggerScopeDirectoryFailureAndTenantMismatchFailClosed(t *testing.T) {
	e := newEnv(t)
	d := e.baseDefinition()
	d.TriggerScope = &domain.TriggerScope{DeviceIDs: []string{e.dev1}}
	rule := e.createRule(d)
	e.dir.err = errors.New("directory unavailable")
	e.ingest(runtime.NewID(), e.dev1, "temperature", 40.0, "")
	if _, err := e.rt.RunNow(e.ctx(), rule, e.dev1); err == nil {
		t.Fatal("manual run bypassed unavailable scope")
	}
	e.dir.err = nil
	device := e.dir.devices[e.dev1]
	device.TenantID = "another-tenant"
	e.dir.devices[e.dev1] = device
	e.ingest(runtime.NewID(), e.dev1, "temperature", 40.0, "")
	if len(e.runs(rule.ID)) != 0 || e.cmds.count() != 0 {
		t.Fatal("unverified scope admitted a run")
	}
}

func TestTriggerScopeAlarmExcludesControlDevice(t *testing.T) {
	e := newEnv(t)
	d := e.baseDefinition()
	d.Kind = domain.KindAlarm
	d.Steps = nil
	d.TriggerScope = &domain.TriggerScope{DeviceIDs: []string{e.dev1}}
	d.Alarm = &domain.AlarmSpec{Sources: []domain.AlarmSource{domain.AlarmSourceDetector}, Level: "M"}
	rule := e.createRule(d)
	e.ingest(runtime.NewID(), e.dev2, "temperature", 40.0, "")
	e.ingest(runtime.NewID(), e.dev1, "temperature", 40.0, "")
	episodes, err := e.st.ListEpisodes(e.ctx(), e.tenant, rule.ID, "", "active", 10)
	if err != nil || len(episodes) != 1 || episodes[0].SubjectID != e.dev1 {
		t.Fatalf("alarm scope: %+v %v", episodes, err)
	}
}
