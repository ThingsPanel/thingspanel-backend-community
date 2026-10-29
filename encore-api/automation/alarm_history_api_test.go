package automation

import (
	"encore.app/automation/runtime"
	"encore.app/automation/store"
	"encore.dev/beta/errs"
	"testing"
)

func TestAlarmHistoryIncludesRecoveredEpisodesAndRejectsOtherTenants(t *testing.T) {
	e := newEnv(t)
	for _, state := range []string{"active", "cleared"} {
		id := runtime.NewID()
		policy := runtime.NewID()
		if err := e.st.OpenEpisode(e.ctx(), &store.Episode{ID: id, TenantID: serviceTestTenant, PolicyID: policy, PolicyKey: policy, SubjectID: e.dev1, State: "active", Level: "H", Source: "detector", Revision: 1, Epoch: 1, OpenedAt: e.now, FirstSeq: 1, LastSeq: 1}); err != nil {
			t.Fatal(err)
		}
		if state == "cleared" {
			if _, err := e.st.DB.ExecContext(e.ctx(), `UPDATE alarm_episodes SET state='cleared',cleared_at=now() WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := e.st.AddTransition(e.ctx(), nil, &store.Transition{ID: runtime.NewID(), EpisodeID: id, TenantID: serviceTestTenant, Kind: "qualified", Seq: 1, Revision: 1, Epoch: 1, Evidence: map[string]any{"fixture": true}}); err != nil {
			t.Fatal(err)
		}
		result, err := e.svc().ListAlarmTransitions(e.ctx(), id)
		if err != nil || len(result.Transitions) != 1 {
			t.Fatalf("%s history missing: %v", state, err)
		}
	}
	foreign := runtime.NewID()
	policy := runtime.NewID()
	if err := e.st.OpenEpisode(e.ctx(), &store.Episode{ID: foreign, TenantID: "other-tenant", PolicyID: policy, PolicyKey: policy, SubjectID: e.dev1, Level: "H", Source: "detector", Revision: 1, Epoch: 1, OpenedAt: e.now, FirstSeq: 1, LastSeq: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc().ListAlarmTransitions(e.ctx(), foreign); errs.Code(err) != errs.NotFound {
		t.Fatalf("foreign history did not return not found: %v", err)
	}
}
