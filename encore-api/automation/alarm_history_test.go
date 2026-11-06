package automation

import (
	"encore.app/automation/runtime"
	"encore.app/automation/store"
	"fmt"
	"testing"
	"time"
)

func TestAlarmHistoryPaginationFiltersAndTenantIsolation(t *testing.T) {
	e := newEnv(t)
	p := e.alarmPolicy(nil)
	for i := 0; i < 205; i++ {
		ep := &store.Episode{ID: runtime.NewID(), TenantID: e.tenant, PolicyID: p.ID, PolicyKey: fmt.Sprintf("history-%d", i), SubjectID: e.dev1, Level: "H", Revision: 1, Epoch: 1, OpenedAt: e.now.Add(time.Duration(i) * time.Second)}
		if err := e.st.OpenEpisode(e.ctx(), ep); err != nil {
			t.Fatal(err)
		}
	}
	q := store.EpisodeQuery{Limit: 200}
	first, err := e.st.QueryEpisodes(e.ctx(), e.tenant, q)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 205 || first.ActiveDevices != 1 || len(first.Episodes) != 200 {
		t.Fatalf("bad first page: total=%d devices=%d len=%d", first.Total, first.ActiveDevices, len(first.Episodes))
	}
	q.Offset = 200
	last, err := e.st.QueryEpisodes(e.ctx(), e.tenant, q)
	if err != nil || last.Total != 205 || len(last.Episodes) != 5 {
		t.Fatalf("last page=%+v err=%v", last, err)
	}
	if !first.Episodes[199].OpenedAt.After(last.Episodes[0].OpenedAt) {
		t.Fatal("pages overlap or order is unstable")
	}
	q.Offset = 400
	empty, err := e.st.QueryEpisodes(e.ctx(), e.tenant, q)
	if err != nil || empty.Total != 205 || len(empty.Episodes) != 0 {
		t.Fatalf("empty page lost total: %+v %v", empty, err)
	}
	q = store.EpisodeQuery{Limit: 10, SubjectID: e.dev1, Start: e.now.Add(200 * time.Second), End: e.now.Add(204 * time.Second), Level: "H"}
	filtered, err := e.st.QueryEpisodes(e.ctx(), e.tenant, q)
	if err != nil || filtered.Total != 5 {
		t.Fatalf("time/subject/level filter: %+v %v", filtered, err)
	}
	for _, change := range []func(*store.EpisodeQuery){func(q *store.EpisodeQuery) { q.SubjectID = e.dev2 }, func(q *store.EpisodeQuery) { q.Level = "L" }, func(q *store.EpisodeQuery) { q.State = "cleared" }} {
		other := q
		change(&other)
		page, err := e.st.QueryEpisodes(e.ctx(), e.tenant, other)
		if err != nil || page.Total != 0 {
			t.Fatalf("filter leaked episodes: %+v %v", page, err)
		}
	}
	other, err := e.st.QueryEpisodes(e.ctx(), "other-tenant", q)
	if err != nil || other.Total != 0 || other.ActiveDevices != 0 {
		t.Fatalf("cross tenant leak: %+v %v", other, err)
	}
	// Acknowledgement preserves active state; recovery keeps the episode in history.
	id := first.Episodes[0].ID
	_, err = e.st.DB.ExecContext(e.ctx(), "UPDATE alarm_episodes SET state='cleared', cleared_at=now() WHERE id=$1", id)
	if err != nil {
		t.Fatal(err)
	}
	q.State = "cleared"
	recovered, err := e.st.QueryEpisodes(e.ctx(), e.tenant, q)
	if err != nil || recovered.Total != 1 || recovered.ActiveDevices != 0 || recovered.Episodes[0].ID != id {
		t.Fatalf("recovered history: %+v %v", recovered, err)
	}
}

func TestAlarmHistoryRejectsInvalidFilters(t *testing.T) {
	for _, req := range []AlarmHistoryRequest{{Page: -1}, {PageSize: 201}, {State: "resolved"}, {Level: "critical"}, {StartTime: "yesterday"}, {StartTime: "2026-11-07T00:00:00Z", EndTime: "2026-11-06T00:00:00Z"}} {
		if _, err := historyQuery(&req); err == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
}
