package automation

import (
	"encore.app/automation/runtime"
	"encore.app/automation/store"
	"sync"
	"testing"
)

func TestConcurrentAlarmAcknowledgementWritesOneFact(t *testing.T) {
	e := newEnv(t)
	id, policy := runtime.NewID(), runtime.NewID()
	if err := e.st.OpenEpisode(e.ctx(), &store.Episode{ID: id, TenantID: serviceTestTenant, PolicyID: policy, PolicyKey: policy, SubjectID: e.dev1, State: "active", Level: "H", Source: "detector", Revision: 1, Epoch: 1, OpenedAt: e.now, FirstSeq: 1, LastSeq: 1}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := e.st.AckEpisode(e.ctx(), serviceTestTenant, id, "first-actor", int64(i+10), runtime.NewID())
			if err != nil || !ok {
				t.Errorf("ack %d: %v %v", i, ok, err)
			}
		}(i)
	}
	wg.Wait()
	var count int
	if err := e.st.DB.QueryRowContext(e.ctx(), `SELECT count(*) FROM alarm_transitions WHERE episode_id=$1 AND kind='ack'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("ack facts=%d err=%v", count, err)
	}
	before, err := e.st.GetActiveEpisodeByID(e.ctx(), serviceTestTenant, id)
	if err != nil || before.AckedAt == nil {
		t.Fatal("missing acknowledgement", err)
	}
	if ok, err := e.st.AckEpisode(e.ctx(), serviceTestTenant, id, "retry-actor", 100, runtime.NewID()); err != nil || !ok {
		t.Fatal("retry", err)
	}
	after, err := e.st.GetActiveEpisodeByID(e.ctx(), serviceTestTenant, id)
	if err != nil || after.AckedBy != before.AckedBy || !after.AckedAt.Equal(*before.AckedAt) {
		t.Fatal("retry rewrote first acknowledgement")
	}
	if ok, err := e.st.AckEpisode(e.ctx(), "other-tenant", id, "foreign", 101, runtime.NewID()); err != nil || ok {
		t.Fatal("foreign acknowledgement was accepted")
	}
}
