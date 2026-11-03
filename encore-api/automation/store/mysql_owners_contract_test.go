package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/executor"
)

// TestAlarmExecutorMigrationBehaveTheSameOnMySQLAsOnPostgres compares the alarm lifecycle, the executor
// registry and the legacy migration records on a real PostgreSQL and a real MySQL.
func TestAlarmExecutorMigrationBehaveTheSameOnMySQLAsOnPostgres(t *testing.T) {
	pg, my := contractDatabases(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	run := func(s *Store) []string {
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		raw := func(v any) string { b, _ := json.Marshal(v); return string(b) }
		const migID = "00000000-0000-0000-0000-0000000000b1"
		const ruleUUID = "00000000-0000-0000-0000-0000000000a1"
		legacy := rule(ruleUUID, "t1", "Legacy scene", domain.KindScene, nil)
		legacy.LegacyID = "L-1"
		if err := s.CreateRule(ctx, legacy); err != nil {
			t.Fatal(err)
		}

		// --- alarm episodes.
		open := func(id string) error {
			return s.OpenEpisode(ctx, &Episode{ID: id, TenantID: "t1", PolicyID: "p1", PolicyKey: "pk", SubjectID: "dev1", Dimension: "temp",
				Level: "H", Source: "rule", Revision: 1, Epoch: 1, OpenedAt: base, FirstSeq: 1, LastSeq: 1})
		}
		note("open: %s", kind(open("e1")))
		note("open a second active episode for the same subject: %s", kind(open("e1b")))
		ep, err := s.GetActiveEpisode(ctx, "t1", "pk", "dev1", "temp")
		note("active: %s id=%s state=%s count=%d", kind(err), ep.ID, ep.State, ep.SampleCount)
		ok, err := s.RefreshEpisode(ctx, nil, "e1", 5, []byte(`{"v":1}`))
		note("refresh seq 5: %v %s", ok, kind(err))
		ok, _ = s.RefreshEpisode(ctx, nil, "e1", 5, []byte(`{"v":2}`))
		note("refresh seq 5 again: %v", ok)
		ok, _ = s.RefreshEpisode(ctx, nil, "e1", 4, nil)
		note("refresh older seq: %v", ok)
		ok, _ = s.RefreshEpisode(ctx, nil, "e1", 6, nil)
		note("refresh without evidence keeps the old one: %v", ok)
		ep, _ = s.GetActiveEpisode(ctx, "t1", "pk", "dev1", "temp")
		note("  last_seq=%d samples=%d evidence=%s", ep.LastSeq, ep.SampleCount, ep.LastEvidence)
		ok, _ = s.AdvanceEpisodeRevision(ctx, nil, "e1", 2, 7, []byte(`{"v":3}`))
		note("advance revision: %v", ok)
		ok, _ = s.AdvanceEpisodeRevision(ctx, nil, "e1", 2, 8, nil)
		note("advance the same revision: %v", ok)
		note("touch: %s", kind(s.TouchEpisode(ctx, "e1", 3)))
		ep, _ = s.GetActiveEpisode(ctx, "t1", "pk", "dev1", "temp")
		note("  revision=%d epoch=%d last_seq=%d evidence=%s", ep.Revision, ep.Epoch, ep.LastSeq, ep.LastEvidence)
		for i, tr := range []*Transition{
			{ID: "tr1", EpisodeID: "e1", TenantID: "t1", Kind: "open", Seq: 1, Revision: 1, Epoch: 1, Evidence: map[string]any{"a": 1}},
			{ID: "tr2", EpisodeID: "e1", TenantID: "t1", Kind: "open", Seq: 1, Revision: 1, Epoch: 1, Evidence: map[string]any{"a": 2}},
			{ID: "tr3", EpisodeID: "e1", TenantID: "t1", Kind: "refresh", Seq: 5, Revision: 1, Epoch: 1},
		} {
			ok, err := s.AddTransition(ctx, nil, tr)
			note("transition %d: %v %s", i, ok, kind(err))
		}
		trs, err := s.ListTransitions(ctx, "e1")
		note("transitions: %s n=%d", kind(err), len(trs))
		for _, tr := range trs {
			note("  %s %s seq=%d evidence=%s", tr.ID, tr.Kind, tr.Seq, raw(tr.Evidence))
		}
		last, err := s.LastTransition(ctx, "e1")
		note("last transition: %s %s", kind(err), last.ID)
		ok, _ = s.AckEpisode(ctx, "t2", "e1", "ann", 9)
		note("ack from another tenant: %v", ok)
		ok, _ = s.AckEpisode(ctx, "t1", "e1", "ann", 9)
		note("ack: %v", ok)
		for _, c := range []struct {
			seq int64
			rev int
		}{{3, 2}, {100, 1}, {100, 2}, {101, 2}} {
			res, err := s.ClearEpisode(ctx, nil, "e1", c.seq, c.rev)
			note("clear seq=%d rev=%d: %s %s", c.seq, c.rev, res, kind(err))
		}
		res, _ := s.ClearEpisode(ctx, nil, "missing", 1, 1)
		note("clear a missing episode: %s", res)
		ok, _ = s.AckEpisode(ctx, "t1", "e1", "bob", 102)
		note("ack a cleared episode: %v", ok)
		note("open again after the clear: %s", kind(open("e2")))
		eps, err := s.ListEpisodes(ctx, "t1", "pk", "", "", 10)
		var epIDs []string
		for _, e := range eps {
			epIDs = append(epIDs, e.ID+":"+e.State)
		}
		sort.Strings(epIDs)
		note("episodes by policy key: %s %v", kind(err), epIDs)
		eps, _ = s.ListEpisodes(ctx, "t1", "p1", "dev1", "cleared", 10)
		note("cleared episodes by policy id: %d", len(eps))
		active, _ := s.ListActiveEpisodes(ctx, 10)
		note("active episodes: %d", len(active))
		dups, err := s.CountDuplicateActiveAlarms(ctx, "t1")
		note("duplicate active alarms: %d %s", dups, kind(err))
		stats, err := s.AlarmStatsOf(ctx, "t1")
		note("stats: %s active=%d total=%d transitions=%d policy=%s state=%s level=%s", kind(err), stats.ActiveEpisodes, stats.TotalEpisodes,
			stats.Transitions, raw(stats.ByPolicy), raw(stats.ByState), raw(stats.ByLevel))
		byID, err := s.EpisodeByID(ctx, "t1", "e1")
		note("episode by id: %s state=%s acked=%v", kind(err), byID.State, byID.AckedAt != nil)

		// --- alarm intake.
		in := &AlarmIntake{TenantID: "t1", PolicyKey: "pk", ActionID: "act1", Source: "rule", SubjectID: "dev1", Dimension: "temp", Kind: "open", PayloadHash: "h1"}
		got, created, err := s.ReserveAlarmIntake(ctx, in)
		note("reserve intake: created=%v outcome=%s %s", created, got.Outcome, kind(err))
		got, created, err = s.ReserveAlarmIntake(ctx, &AlarmIntake{TenantID: "t1", PolicyKey: "pk", ActionID: "act1", PayloadHash: "other", Source: "x", SubjectID: "y", Kind: "z"})
		note("reserve again: created=%v hash=%s %s", created, got.PayloadHash, kind(err))
		note("finish intake: %s", kind(s.FinishAlarmIntake(ctx, nil, "t1", "pk", "act1", "done", "ok", "e1", "")))
		note("release a finished intake is a no-op: %s", kind(s.ReleaseAlarmIntake(ctx, "t1", "pk", "act1")))
		fin, err := s.AlarmIntake(ctx, "t1", "pk", "act1")
		note("intake after release: %s outcome=%s episode=%s transition=%q", kind(err), fin.Outcome, fin.EpisodeID, fin.TransitionID)
		_, _, _ = s.ReserveAlarmIntake(ctx, &AlarmIntake{TenantID: "t1", PolicyKey: "pk", ActionID: "act2", PayloadHash: "h2", Source: "x", SubjectID: "y", Kind: "z"})
		note("release an in-flight intake: %s", kind(s.ReleaseAlarmIntake(ctx, "t1", "pk", "act2")))
		_, err = s.AlarmIntake(ctx, "t1", "pk", "act2")
		note("released intake is gone: %s", kind(err))

		// --- alarm events relay.
		err = s.WithTx(ctx, func(tx *sql.Tx) error {
			for i := 0; i < 3; i++ {
				if _, err := s.PublishEvent(ctx, tx, fmt.Sprintf("evt%d", i), "t1", "alarm.opened", "dev1", map[string]any{"n": i}); err != nil {
					return err
				}
			}
			_, err := s.PublishEvent(ctx, tx, "evt-other", "t1", "run.finished", "dev1", map[string]any{})
			return err
		})
		note("publish events: %s", kind(err))
		pending, err := s.UnrelayedAlarmEvents(ctx, 10)
		note("unrelayed alarm events: %s n=%d", kind(err), len(pending))
		note("mark relayed: %s", kind(s.MarkEventRelayed(ctx, nil, "evt0")))
		pending, _ = s.UnrelayedAlarmEvents(ctx, 10)
		var pendingIDs []string
		for _, p := range pending {
			pendingIDs = append(pendingIDs, p.ID+raw(p.Payload))
		}
		note("unrelayed after one is marked: %v", pendingIDs)

		// --- executors.
		contract := executor.Contract{Engine: executor.EngineGeneric}
		ex := &Executor{ID: "x1", TenantID: "t1", Name: "Flow", Engine: "generic", Contract: contract, ContractHash: "ch1", Epoch: 1, Enabled: true, CredentialID: "cred"}
		note("create executor: %s", kind(s.CreateExecutor(ctx, ex)))
		got2, err := s.GetExecutor(ctx, "t1", "x1")
		note("get executor: %s name=%s epoch=%d enabled=%v cred=%s seen=%v contract=%s", kind(err), got2.Name, got2.Epoch, got2.Enabled, got2.CredentialID, got2.LastSeenAt != nil, raw(got2.Contract))
		_, err = s.GetExecutor(ctx, "t2", "x1")
		note("get across tenants: %s", kind(err))
		list, total, err := s.ListExecutors(ctx, "t1", 10, 0)
		note("list executors: %s n=%d total=%d", kind(err), len(list), total)
		note("touch: %s", kind(s.TouchExecutor(ctx, "t1", "x1", base)))
		note("disable: %s", kind(s.SetExecutorEnabled(ctx, "t1", "x1", false)))
		got2, _ = s.GetExecutor(ctx, "t1", "x1")
		note("  enabled=%v", got2.Enabled)
		for _, seq := range []int64{1, 1, 0, 2} {
			ok, err := s.AdvanceExecutorSeq(ctx, "t1", "x1", 1, seq)
			note("advance seq %d: %v %s", seq, ok, kind(err))
		}
		ok, _ = s.AdvanceExecutorSeq(ctx, "t1", "x1", 7, 99)
		note("advance with the wrong epoch: %v", ok)
		var wg sync.WaitGroup
		var wins int
		var mu sync.Mutex
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := s.AdvanceExecutorSeq(ctx, "t1", "x1", 1, 50)
				if err != nil {
					t.Error(err)
				}
				if ok {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		note("8 racers advance to seq 50, winners=%d", wins)
		rebound, err := s.RebindExecutorContract(ctx, "t1", "x1", executor.Contract{Engine: executor.EngineFlink}, "ch2")
		if rebound == nil {
			t.Fatalf("rebind (mysql=%v): %v", s.MySQL, err)
		}
		note("rebind: %s epoch=%d last_seq=%d engine=%s hash=%s", kind(err), rebound.Epoch, rebound.LastSeq, rebound.Engine, rebound.ContractHash)
		_, err = s.RebindExecutorContract(ctx, "t1", "missing", contract, "x")
		note("rebind a missing executor: %s", kind(err))
		for i := 0; i < 2; i++ {
			ok, err := s.RecordExecutorMessage(ctx, &ExecutorMessage{TenantID: "t1", ExecutorID: "x1", IdempotencyKey: "m1", Direction: "in", Epoch: 2, Seq: int64(i), Outcome: "accepted"})
			note("record message %d: %v %s", i, ok, kind(err))
		}
		msg, err := s.LookupExecutorMessage(ctx, "t1", "x1", "m1")
		note("lookup message: %s seq=%d outcome=%s", kind(err), msg.Seq, msg.Outcome)
		_, err = s.LookupExecutorMessage(ctx, "t1", "x1", "nope")
		note("lookup a missing message: %s", kind(err))
		msgs, total, err := s.ListExecutorMessages(ctx, "t1", "x1", 10, 0)
		note("list messages: %s n=%d total=%d", kind(err), len(msgs), total)

		// --- legacy migration.
		items := []*MigrationItem{
			{MigrationID: migID, LegacyID: "L-1", Kind: MigrationKindScene, Name: "scene", Status: MigrationPlanned, HasDefinition: true,
				Definition: domain.Definition{Kind: domain.KindScene}},
			{MigrationID: migID, LegacyID: "L-2", Kind: MigrationKindAlarm, Name: "alarm", Status: MigrationPlanned,
				Alarm: &AlarmAdoption{PolicyLegacyID: "p", PolicyKey: "pk", Subject: "dev1", OpenedAt: "2026-01-01T00:00:00Z", LegacyStatus: "open"}},
			{MigrationID: migID, LegacyID: "L-3", Kind: MigrationKindAutomation, Name: "odd", Status: MigrationUnsupported, ReasonCode: "x", Reason: "no"},
		}
		note("create migration: %s", kind(s.CreateMigration(ctx, &Migration{ID: migID, TenantID: "t1", Name: "run", Status: MigrationStatusPlanned, SnapshotHash: "sh"}, items)))
		m, err := s.GetMigration(ctx, "t1", migID)
		if m == nil {
			t.Fatalf("migration missing (mysql=%v): %v", s.MySQL, err)
		}
		note("get migration: %s total=%d unresolved=%d status=%s", kind(err), m.Total, m.Unresolved, m.Status)
		batch, err := s.NextMigrationBatch(ctx, migID, 10)
		var batchIDs []string
		for _, it := range batch {
			batchIDs = append(batchIDs, it.LegacyID+"/"+it.Kind)
		}
		note("next batch: %s %v", kind(err), batchIDs)
		note("item status with a rule: %s", kind(s.SetMigrationItemStatus(ctx, migID, "L-1", MigrationApplied, ruleUUID, "", "")))
		note("item status without a rule keeps the old one: %s", kind(s.SetMigrationItemStatus(ctx, migID, "L-1", MigrationApplied, "", "note", "again")))
		note("alarm outcome: %s", kind(s.SetMigrationAlarmOutcome(ctx, migID, "L-2", MigrationFailed, "e1", "boom", "failed")))
		item, err := s.MigrationItemByLegacyID(ctx, migID, "L-1")
		note("item L-1: %s status=%s rule=%s reason=%s/%s hasDef=%v", kind(err), item.Status, item.RuleID, item.ReasonCode, item.Reason, item.HasDefinition)
		item, _ = s.MigrationItemByLegacyID(ctx, migID, "L-2")
		note("item L-2: status=%s episode=%s alarm=%s", item.Status, item.EpisodeID, raw(item.Alarm))
		pendingCount, err := s.PendingMigrationCount(ctx, migID)
		note("pending: %d %s", pendingCount, kind(err))
		m, err = s.RecomputeMigrationCounts(ctx, migID)
		note("recompute: %s applied=%d skipped=%d failed=%d unresolved=%d", kind(err), m.Applied, m.Skipped, m.Failed, m.Unresolved)
		note("status: %s", kind(s.SetMigrationStatus(ctx, migID, MigrationStatusApplied)))
		listed, err := s.ListMigrationItems(ctx, migID, "", 10)
		note("list items: %s n=%d", kind(err), len(listed))
		listed, _ = s.ListMigrationItems(ctx, migID, MigrationFailed, 10)
		note("list failed items: %d", len(listed))
		byLegacy, err := s.RulesByLegacyIDs(ctx, "t1", []string{"L-1", "L-9"})
		note("rules by legacy ids: %s n=%d", kind(err), len(byLegacy))
		runs, err := s.RuleRunCount(ctx, "t1", "r1")
		note("rule run count: %d %s", runs, kind(err))
		return out
	}
	want, got := run(pg), run(my)
	failures := 0
	for _, line := range want {
		if strings.HasSuffix(line, " error") {
			failures++
		}
	}
	t.Logf("%d steps compared, %d of them expected failures (unique violations)", len(want), failures)
	if len(want) != len(got) {
		t.Errorf("PostgreSQL produced %d steps and MySQL %d", len(want), len(got))
	}
	for i := 0; i < len(want) && i < len(got); i++ {
		if want[i] != got[i] {
			t.Errorf("step %d differs\n  postgres: %s\n  mysql:    %s", i+1, want[i], got[i])
		}
	}
	if t.Failed() {
		t.Logf("PostgreSQL steps:\n%s", strings.Join(want, "\n"))
	}
}
