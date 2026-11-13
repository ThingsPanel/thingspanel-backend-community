package automation

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

func TestQ09NewTemplateMemberAndDirectoryFailure(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(e.baseDefinition())
	e.bind(r, "device_config", "cfg-1", nil)
	newID := "new-" + runtime.NewID()
	e.dir.devices[newID] = ports.Device{ID: newID, TenantID: e.tenant, DeviceConfigID: "cfg-1", Online: 1}
	e.ingest(runtime.NewID(), newID, "temperature", 38.0, "")
	if n := len(e.runs(r.ID)); n != 1 {
		t.Fatalf("new bound template member want1 got%d", n)
	}
	e.rt.InvalidateBindings(e.tenant, "")
	e.dir.err = errors.New("QA directory unavailable")
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	if n := len(e.runs(r.ID)); n != 1 {
		t.Fatalf("directory failure widened template membership got%d", n)
	}
}

func TestQ09ForeignBindingCannotSupplyProvenance(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(e.baseDefinition())
	b := &store.Binding{ID: runtime.NewID(), TenantID: "other-" + e.tenant, RuleID: r.ID,
		DeviceID: e.dev1, Source: store.BindingSourceDevice, Revision: r.Revision, Enabled: true}
	b.BindingKey = store.BindingKey(b.TenantID, b.RuleID, b.DeviceID, "")
	if err := e.st.UpsertBinding(e.ctx(), b); err != nil {
		t.Fatal(err)
	}
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	runs := e.runs(r.ID)
	if len(runs) != 1 {
		t.Fatalf("foreign binding changed owned unbound rule behavior got%d", len(runs))
	}
	s := snapshotOf(t, runs[0])
	if s["binding_id"] != "" || s["binding_source"] != "" {
		t.Fatalf("foreign provenance leaked: %v", s)
	}
}

// These regressions use the real isolated Encore PostgreSQL store/runtime.
// Only outbound command delivery and the device directory are test doubles.
func TestQ09SelfScopeDoesNotWatchUnselectedDevice(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(e.baseDefinition())
	e.ingest(runtime.NewID(), e.dev2, "temperature", 38.0, "")
	if n := len(e.runs(r.ID)); n != 0 {
		t.Fatalf("unselected self source created %d runs", n)
	}
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	if n := len(e.runs(r.ID)); n != 1 {
		t.Fatalf("selected self source want1 run got%d", n)
	}
}

func TestQ09ExplicitSourceCanControlDifferentTarget(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.Groups[0].Conditions[0].Source = "device"
	def.Trigger.Groups[0].Conditions[0].DeviceID = e.dev1
	def.Targets.DeviceIDs = []string{e.dev2}
	r := e.createRule(def)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	if n := len(e.runs(r.ID)); n != 1 {
		t.Fatalf("explicit source A must still control B, runs%d", n)
	}
	e.ingest(runtime.NewID(), e.dev2, "temperature", 38.0, "")
	if n := len(e.runs(r.ID)); n != 1 {
		t.Fatalf("action target B is not source A, runs%d", n)
	}
}

func TestQ09TemplateSelfScopeWithoutBinding(t *testing.T) {
	e := newEnv(t)
	other := "outside-" + runtime.NewID()
	e.dir.devices[other] = ports.Device{ID: other, TenantID: e.tenant, DeviceConfigID: "cfg-other", Online: 1}
	def := e.baseDefinition()
	def.Targets.DeviceIDs = nil
	def.Targets.DeviceConfigIDs = []string{"cfg-1"}
	r := e.createRule(def)
	e.ingest(runtime.NewID(), other, "temperature", 38.0, "")
	if n := len(e.runs(r.ID)); n != 0 {
		t.Fatalf("other template triggered%d runs", n)
	}
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	e.ingest(runtime.NewID(), e.dev2, "temperature", 38.0, "")
	if n := len(e.runs(r.ID)); n != 2 {
		t.Fatalf("own template two devices want2 got%d", n)
	}
}

func TestQ09BindingDefinesMembershipAndDetachStopsNewRuns(t *testing.T) {
	for _, param := range []bool{false, true} {
		t.Run(fmt.Sprint(param), func(t *testing.T) {
			e := newEnv(t)
			other := "outside-" + runtime.NewID()
			e.dir.devices[other] = ports.Device{ID: other, TenantID: e.tenant, DeviceConfigID: "cfg-other", Online: 1}
			def := e.baseDefinition()
			if param {
				def = paramRule(e, "30")
			}
			r := e.createRule(def)
			b := e.bind(r, "device_config", "cfg-1", nil)
			e.ingest(runtime.NewID(), other, "temperature", 38.0, "")
			if n := len(e.runs(r.ID)); n != 0 {
				t.Fatalf("nonmember ran bound rule%d", n)
			}
			e.ingest(runtime.NewID(), e.dev2, "temperature", 38.0, "")
			rows := e.runs(r.ID)
			if len(rows) != 1 {
				t.Fatalf("bound template member want1 got%d", len(rows))
			}
			s := snapshotOf(t, rows[0])
			if s["binding_id"] != b.ID || s["binding_source"] != "template" {
				t.Fatalf("binding provenance lost: %v", s)
			}
			if err := e.st.DetachBinding(e.ctx(), e.tenant, b.ID); err != nil {
				t.Fatal(err)
			}
			e.rt.InvalidateBindings(e.tenant, "")
			e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
			if n := len(e.runs(r.ID)); n != 1 {
				t.Fatalf("detached rule reverted to broad self matching, runs%d", n)
			}
			e.bind(r, "device_config", "cfg-1", nil)
			e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
			if n := len(e.runs(r.ID)); n != 2 {
				t.Fatalf("reattach must restore membership runs%d", n)
			}
		})
	}
}

func TestQ09ContinuousAlarmHoldKeepsFirstDeadline(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	def.Trigger.For = domain.Duration(6 * time.Second)
	r := e.createRule(def)
	start := e.now
	scope := runtime.ScopeKey(&r.Definition, r.ID, e.dev1)
	for i := 0; i < 3; i++ {
		e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
		p, err := e.st.GetPending(e.ctx(), scope)
		if err != nil {
			t.Fatal(err)
		}
		if !p.StartedAt.Equal(start) || !p.Deadline.Equal(start.Add(6*time.Second)) {
			t.Fatalf("high sample%d moved window: %+v", i, p)
		}
		e.advance(2 * time.Second)
		e.tick()
	}
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, r.ID, e.dev1, ""); err != nil {
		t.Fatalf("continuous6s did not qualify: %v", err)
	}
}

func TestQ09InterruptedAlarmHoldStartsFreshWindow(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	def.Trigger.For = domain.Duration(6 * time.Second)
	r := e.createRule(def)
	scope := runtime.ScopeKey(&r.Definition, r.ID, e.dev1)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	e.advance(2 * time.Second)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 20.0, "")
	if p, err := e.st.GetPending(e.ctx(), scope); err == nil && p != nil {
		t.Fatal("normal reading did not cancel unqualified alarm hold")
	}
	e.advance(time.Second)
	start := e.now
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	p, err := e.st.GetPending(e.ctx(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Deadline.Equal(start.Add(6 * time.Second)) {
		t.Fatalf("fresh hold inherited old deadline %v", p.Deadline)
	}
	e.advance(3 * time.Second)
	e.tick()
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, r.ID, e.dev1, ""); err == nil {
		t.Fatal("alarm opened before new6s interval")
	}
	e.advance(3 * time.Second)
	e.tick()
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, r.ID, e.dev1, ""); err != nil {
		t.Fatal(err)
	}
}

func TestQ09ContinuousRecoveryKeepsFirstDeadline(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	def.Recovery.For = domain.Duration(6 * time.Second)
	r := e.createRule(def)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	start := e.now
	scope := runtime.ScopeKey(&r.Definition, r.ID, e.dev1) + ":recovery"
	for i := 0; i < 3; i++ {
		e.ingest(runtime.NewID(), e.dev1, "temperature", 20.0, "")
		p, err := e.st.GetPending(e.ctx(), scope)
		if err != nil {
			t.Fatal(err)
		}
		if !p.StartedAt.Equal(start) || !p.Deadline.Equal(start.Add(6*time.Second)) {
			t.Fatalf("normal sample%d moved recovery window: %+v", i, p)
		}
		e.advance(2 * time.Second)
		e.tick()
	}
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, r.ID, e.dev1, ""); err != store.ErrNotFound {
		t.Fatalf("continuous recovery6s did not clear: %v", err)
	}
}

func TestQ09NeutralInterruptsRecoveryWindow(t *testing.T) {
	e := newEnv(t)
	def := e.alarmDefinition()
	def.Recovery.For = domain.Duration(6 * time.Second)
	r := e.createRule(def)
	scope := runtime.ScopeKey(&r.Definition, r.ID, e.dev1) + ":recovery"
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	e.ingest(runtime.NewID(), e.dev1, "temperature", 20.0, "")
	e.advance(2 * time.Second)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 29.0, "")
	if p, err := e.st.GetPending(e.ctx(), scope); err == nil && p != nil {
		t.Fatal("neutral zone did not cancel recovery hold")
	}
	e.advance(time.Second)
	start := e.now
	e.ingest(runtime.NewID(), e.dev1, "temperature", 20.0, "")
	p, err := e.st.GetPending(e.ctx(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Deadline.Equal(start.Add(6 * time.Second)) {
		t.Fatalf("new recovery inherited old deadline %v", p.Deadline)
	}
	e.advance(3 * time.Second)
	e.tick()
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, r.ID, e.dev1, ""); err != nil {
		t.Fatal("recovery cleared before new6s interval")
	}
	e.advance(3 * time.Second)
	e.tick()
	if _, err := e.st.GetActiveEpisode(e.ctx(), e.tenant, r.ID, e.dev1, ""); err != store.ErrNotFound {
		t.Fatal("recovery did not clear at full interval")
	}
}

func TestQ09DetachedAndDisabledPendingCannotFire(t *testing.T) {
	for _, detach := range []bool{false, true} {
		t.Run(fmt.Sprint(detach), func(t *testing.T) {
			e := newEnv(t)
			def := e.baseDefinition()
			def.Trigger.For = domain.Duration(6 * time.Second)
			r := e.createRule(def)
			b := e.bind(r, "device_config", "cfg-1", nil)
			e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
			if detach {
				if err := e.st.DetachBinding(e.ctx(), e.tenant, b.ID); err != nil {
					t.Fatal(err)
				}
				e.rt.InvalidateBindings(e.tenant, "")
			} else {
				if _, err := e.st.DB.ExecContext(e.ctx(), "UPDATE auto_rules SET enabled=false WHERE id=$1 AND tenant_id=$2", r.ID, e.tenant); err != nil {
					t.Fatal(err)
				}
			}
			e.advance(6 * time.Second)
			e.tick()
			if n := len(e.runs(r.ID)); n != 0 {
				t.Fatalf("removed membership/disabled pending fired%d", n)
			}
		})
	}
}

func TestQ09DeletedRulePendingIsDropped(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(6 * time.Second)
	r := e.createRule(def)
	scope := runtime.ScopeKey(&def, r.ID, e.dev1)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	if _, err := e.st.DB.ExecContext(e.ctx(), "UPDATE auto_rules SET deleted_at=now() WHERE id=$1 AND tenant_id=$2", r.ID, e.tenant); err != nil {
		t.Fatal(err)
	}
	e.advance(6 * time.Second)
	e.tick()
	if _, err := e.st.GetPending(e.ctx(), scope); err != store.ErrNotFound {
		t.Fatalf("deleted rule pending not dropped: %v", err)
	}
	if n := e.cmds.count(); n != 0 {
		t.Fatalf("deleted rule dispatched%d commands", n)
	}
}
