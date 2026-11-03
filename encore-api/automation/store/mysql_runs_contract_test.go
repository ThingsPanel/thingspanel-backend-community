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
)

// TestRunRuntimeBehavesTheSameOnMySQLAsOnPostgres drives the claim, lease, upsert and RETURNING-style
// statements through the same scenario on a real PostgreSQL and a real MySQL and compares the step lists.
func TestRunRuntimeBehavesTheSameOnMySQLAsOnPostgres(t *testing.T) {
	pg, my := contractDatabases(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	run := func(s *Store) []string {
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		ids := func(v []string) string { sort.Strings(v); return strings.Join(v, ",") }
		for _, r := range []*Rule{
			rule("r1", "t1", "Boiler", domain.KindAutomation, func(d *domain.Definition) { d.Targets.DeviceIDs = []string{"d1"} }),
			rule("r2", "t1", "Scene", domain.KindScene, nil),
		} {
			if err := s.CreateRule(ctx, r); err != nil {
				t.Fatal(err)
			}
		}
		mk := func(id, scope, status string, deadline *time.Time, snapshot string) {
			err := s.CreateRun(ctx, &Run{ID: id, TenantID: "t1", RuleID: "r1", Revision: 1, DefinitionHash: "h", ScopeKey: scope,
				Generation: 1, DeviceID: "d1", Status: status, Snapshot: json.RawMessage(snapshot), StartedAt: base, Deadline: deadline})
			if err != nil {
				t.Fatalf("create run %s: %v", id, err)
			}
		}
		past, future := base.Add(-time.Hour), base.Add(time.Hour)

		// --- latest state: newer or equal wins, older is ignored.
		for i, tc := range []struct {
			at    time.Duration
			value any
		}{{10 * time.Second, 5}, {0, 3}, {20 * time.Second, 9}, {20 * time.Second, 11}} {
			e := &IngestEvent{EventID: fmt.Sprintf("ev%d", i), TenantID: "t1", DeviceID: "d1", Signal: domain.SignalTelemetry,
				Key: "temp", Value: tc.value, Unit: "C", ObservedAt: base.Add(tc.at)}
			_, err := s.InsertEvent(ctx, e)
			if err != nil {
				t.Logf("InsertEvent: %v", err)
			}
			note("insert event %d: %s", i, kind(err))
			note("apply state %d: %s", i, kind(s.ApplyState(ctx, e)))
			sample, found, err := s.LatestState(ctx, "d1", domain.SignalTelemetry, "temp")
			note("  latest: %s found=%v value=%v at=%d", kind(err), found, sample.Value, sample.ObservedAt.Unix()-base.Unix())
		}
		latest, ok, err := s.LatestObservedAt(ctx, "d1", domain.SignalTelemetry, []string{"humidity", "temp"})
		note("latest observed over keys: %s ok=%v at=%d", kind(err), ok, latest.Unix()-base.Unix())
		_, ok, _ = s.LatestObservedAt(ctx, "d1", domain.SignalTelemetry, []string{"humidity"})
		note("latest observed with an unknown key: %v", ok)

		// --- leases.
		mk("a1", "s1", "accepted", &future, `{}`)
		mk("a2", "s1", "running", &future, `{}`)
		mk("a3", "s2", "accepted", &future, `{}`)
		got, err := s.ClaimRun(ctx, "a1", "w1", base, base.Add(2*time.Minute))
		note("claim a1 by w1: %v %s", got, kind(err))
		got, _ = s.ClaimRun(ctx, "a1", "w2", base.Add(time.Second), base.Add(3*time.Minute))
		note("claim a1 by w2 while w1 holds a live lease: %v", got)
		got, _ = s.ClaimRun(ctx, "a1", "w1", base.Add(time.Second), base.Add(3*time.Minute))
		note("claim a1 again by its owner: %v", got)
		r, _ := s.GetRunByID(ctx, "a1")
		note("a1 status=%s resume=%d", r.Status, r.ResumeCount)
		got, _ = s.ClaimRun(ctx, "a1", "w2", base.Add(10*time.Minute), base.Add(12*time.Minute))
		note("claim a1 by w2 after the lease expired: %v", got)
		r, _ = s.GetRunByID(ctx, "a1")
		note("a1 resume=%d", r.ResumeCount)
		note("release by a stranger: %s", kind(s.ReleaseRun(ctx, "a1", "w1")))
		got, _ = s.ClaimRun(ctx, "a1", "w3", base.Add(time.Minute), base.Add(5*time.Minute))
		note("claim after a stranger's release: %v", got)
		note("release by the owner: %s", kind(s.ReleaseRun(ctx, "a1", "w2")))
		got, _ = s.ClaimRun(ctx, "missing", "w1", base, base.Add(time.Minute))
		note("claim a missing run: %v", got)
		got, _ = s.ClaimRun(ctx, "a2", "w1", base, base.Add(time.Minute))
		r, _ = s.GetRunByID(ctx, "a2")
		note("claim a2 (never leased): %v resume=%d", got, r.ResumeCount)

		var wg sync.WaitGroup
		wins := make(chan string, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ok, err := s.ClaimRun(ctx, "a3", fmt.Sprintf("racer%d", i), base, base.Add(time.Minute))
				if err != nil {
					t.Error(err)
				}
				if ok {
					wins <- "x"
				}
			}(i)
		}
		wg.Wait()
		close(wins)
		note("8 racers claim a3, winners=%d", len(wins))

		resumable, _ := s.ResumableRuns(ctx, base.Add(time.Hour*3), 10)
		note("resumable far in the future: %s", ids(resumable))

		// --- overdue runs.
		mk("o1", "s3", "running", &past, `{}`)
		mk("o2", "s3", "accepted", &past, `{"deadline_reason":"parallel_ttl_exceeded"}`)
		mk("o3", "s3", "queued", &past, `{"deadline_reason":"queue_ttl_exceeded"}`)
		mk("o4", "s3", "running", &future, `{}`)
		expired, err := s.ExpireOverdueRuns(ctx, base, 10)
		note("expire overdue: %s %s", kind(err), ids(expired))
		for _, id := range []string{"o1", "o2", "o3", "o4"} {
			x, _ := s.GetRunByID(ctx, id)
			note("  %s status=%s code=%s reason=%s finished=%v lease=%q", id, x.Status, x.ReasonCode, x.Reason, x.FinishedAt != nil, "")
		}
		again, _ := s.ExpireOverdueRuns(ctx, base, 10)
		note("expire again: %d", len(again))
		mk("o5", "s3", "running", &past, `{}`)
		mk("o6", "s3", "running", &past, `{}`)
		limited, _ := s.ExpireOverdueRuns(ctx, base, 1)
		note("expire with limit 1: %d", len(limited))

		// --- timers.
		for i := 0; i < 5; i++ {
			err := s.WithTx(ctx, func(tx *sql.Tx) error {
				return s.ScheduleRunTimer(ctx, tx, "a1", i, base.Add(time.Duration(i-10)*time.Second))
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := s.WithTx(ctx, func(tx *sql.Tx) error { return s.ScheduleRunTimer(ctx, tx, "a1", 9, base.Add(time.Hour)) }); err != nil {
			t.Fatal(err)
		}
		first, err := s.DueRunTimers(ctx, base, 2)
		note("due timers limit 2: %s n=%d", kind(err), len(first))
		for _, x := range first {
			note("  pc=%d state=%s due=%d", x.PC, x.State, x.DueAt.Unix()-base.Unix())
		}
		claimed := make(chan int, 16)
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := s.DueRunTimers(ctx, base, 1)
				if err != nil {
					t.Error(err)
				}
				for _, x := range got {
					claimed <- x.PC
				}
			}()
		}
		wg.Wait()
		close(claimed)
		var pcs []int
		for pc := range claimed {
			pcs = append(pcs, pc)
		}
		sort.Ints(pcs)
		note("6 racers drain the rest: %v", pcs)
		none, _ := s.DueRunTimers(ctx, base, 10)
		note("nothing left due: %d", len(none))

		// --- cancel.
		cancelled, err := s.CancelActiveRuns(ctx, "s1", "restart", "replaced")
		note("cancel scope s1: %s %s", kind(err), ids(cancelled))
		cancelled, _ = s.CancelActiveRuns(ctx, "s1", "restart", "replaced")
		note("cancel scope s1 again: %d", len(cancelled))
		x, _ := s.GetRunByID(ctx, "a2")
		note("a2 status=%s code=%s", x.Status, x.ReasonCode)

		// --- paged listing with a status filter.
		page, total, err := s.ListRunsPaged(ctx, RunQuery{TenantID: "t1", Statuses: []string{"cancelled", "expired"}, Limit: 100})
		var listed []string
		for _, p := range page {
			listed = append(listed, p.ID)
		}
		note("list by statuses: %s total=%d %s", kind(err), total, ids(listed))

		// --- steps.
		id1, attempt1, err := s.UpsertStep(ctx, &Step{ID: "st1", RunID: "a3", Idx: 0, Kind: "action", Status: "pending", IdempotencyKey: "k1"})
		note("step upsert: %s id=%s attempt=%d", kind(err), id1, attempt1)
		id2, attempt2, err := s.UpsertStep(ctx, &Step{ID: "st-other", RunID: "a3", Idx: 0, Kind: "wait", Status: "pending", IdempotencyKey: "k1"})
		note("step upsert again: %s id=%s attempt=%d", kind(err), id2, attempt2)
		note("update step with detail: %s", kind(s.UpdateStepStatus(ctx, "st1", "done", `{"ok":true}`)))
		note("update step without detail: %s", kind(s.UpdateStepStatus(ctx, "st1", "done", "")))
		note("update a missing step: %s", kind(s.UpdateStepStatus(ctx, "nope", "done", "")))
		steps, _ := s.ListSteps(ctx, "a3")
		for _, st := range steps {
			note("  step %s kind=%s status=%s attempt=%d detail=%s", st.ID, st.Kind, st.Status, st.Attempt, string(st.Detail))
		}

		// --- outbox.
		enqueue := func(id, key string) (bool, error) {
			var created bool
			err := s.WithTx(ctx, func(tx *sql.Tx) error {
				var err error
				created, err = s.EnqueueOutbox(ctx, tx, &OutboxItem{ID: id, TenantID: "t1", RunID: "a3", Kind: "command", ScopeKey: "s2",
					Generation: 1, IdempotencyKey: key, Payload: json.RawMessage(`{"n":1}`)})
				return err
			})
			return created, err
		}
		created, err := enqueue("ob1", "ik1")
		note("enqueue: %v %s", created, kind(err))
		created, err = enqueue("ob2", "ik1")
		note("enqueue with the same idempotency key: %v %s", created, kind(err))
		for i := 3; i <= 6; i++ {
			_, _ = enqueue(fmt.Sprintf("ob%d", i), fmt.Sprintf("ik%d", i))
		}
		batch, err := s.ClaimOutbox(ctx, 2)
		note("claim outbox limit 2: %s n=%d", kind(err), len(batch))
		for _, o := range batch {
			note("  state=%s attempt=%d payload=%s", o.State, o.Attempt, o.Payload)
		}
		var claimedOutbox sync.Map
		dupes := 0
		var mu sync.Mutex
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := s.ClaimOutbox(ctx, 1)
				if err != nil {
					t.Error(err)
				}
				for _, o := range got {
					if _, loaded := claimedOutbox.LoadOrStore(o.ID, true); loaded {
						mu.Lock()
						dupes++
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
		count := 0
		claimedOutbox.Range(func(_, _ any) bool { count++; return true })
		note("5 racers claim outbox: claimed=%d duplicates=%d", count, dupes)
		more, _ := s.ClaimOutbox(ctx, 10)
		note("claim after all are leased: %d", len(more))
		note("mark with detail: %s", kind(s.MarkOutboxWithDetail(ctx, "ob1", "accepted", "ref-1", "", []byte(`{"status":"ok","ids":[1,2]}`), base)))
		items, _ := s.OutboxForRun(ctx, "a3")
		for _, o := range items {
			if o.ID != "ob1" {
				continue
			}
			var payload map[string]any
			_ = json.Unmarshal(o.Payload, &payload)
			b, _ := json.Marshal(payload)
			note("  ob1 state=%s ref=%s payload=%s", o.State, o.AcceptedRef, b)
		}

		// --- schedules and fires.
		for i := 0; i < 3; i++ {
			due := base.Add(time.Duration(i-5) * time.Minute)
			err := s.CreateSchedule(ctx, &Schedule{ID: fmt.Sprintf("sc%d", i), TenantID: "t1", RuleID: "r1", Kind: "cron", Cron: "* * * * *",
				Timezone: "UTC", MisfirePolicy: "fire_once", Enabled: true, NextFireAt: &due})
			if err != nil {
				t.Fatal(err)
			}
		}
		due, err := s.ClaimDueSchedules(ctx, base, 2, "sched-1", 90*time.Second)
		note("claim schedules limit 2: %s n=%d", kind(err), len(due))
		for _, sc := range due {
			note("  %s owner=%s lease=%d", sc.ID, sc.LeaseOwner, sc.LeaseUntil.Unix()-base.Unix())
		}
		due, _ = s.ClaimDueSchedules(ctx, base, 10, "sched-2", 90*time.Second)
		note("claim schedules again: n=%d", len(due))
		for i := 0; i < 3; i++ {
			ok, err := s.InsertFire(ctx, &Fire{ID: fmt.Sprintf("f%d", i), ScheduleID: "sc0", TenantID: "t1", RuleID: "r1",
				ScheduledFor: base.Add(time.Duration(i%2) * time.Minute), State: FireClaimed})
			note("insert fire %d: %v %s", i, ok, kind(err))
		}
		fires, err := s.ClaimFires(ctx, base, 10, "fire-1", time.Minute)
		note("claim fires: %s n=%d", kind(err), len(fires))
		for _, f := range fires {
			note("  state=%s attempt=%d", f.State, f.Attempt)
		}
		fires, _ = s.ClaimFires(ctx, base, 10, "fire-2", time.Minute)
		note("claim fires again: n=%d", len(fires))

		// --- bundle imports.
		stage := func(id string) (string, bool, error) {
			b, created, err := s.StageBundleImport(ctx, &BundleImport{ID: id, TenantID: "t1", BundleKey: "pack", Version: "1", ContentHash: "hash",
				Status: BundleStaged, Manifest: []byte(`{"a":1}`)})
			if b == nil {
				return "", created, err
			}
			return b.ID, created, err
		}
		stored, created2, err := stage("bi1")
		if err != nil {
			t.Logf("StageBundleImport: %v", err)
		}
		note("stage bundle: %s created=%v %s", stored, created2, kind(err))
		stored, created2, err = stage("bi2")
		note("stage the same bundle again: %s created=%v %s", stored, created2, kind(err))

		// --- references, device matching, binding summary.
		note("rules matching a device: %s", func() string {
			rules, err := s.RulesMatchingDevice(ctx, "t1", "d1")
			var names []string
			for _, r := range rules {
				names = append(names, r.ID)
			}
			return kind(err) + " " + ids(names)
		}())
		refer := rule("r3", "t1", "Caller", domain.KindAutomation, func(d *domain.Definition) {
			d.Steps = []domain.Step{{Kind: "activate_scene", Value: "r2"}}
		})
		note("create referrer: %s", kind(s.CreateRule(ctx, refer)))
		referrers, err := s.ReferencingRules(ctx, "t1", "r2")
		var names []string
		for _, r := range referrers {
			names = append(names, r.ID)
		}
		note("rules referencing r2: %s %s", kind(err), ids(names))
		referrers, _ = s.ReferencingRules(ctx, "t1", "r_")
		note("a wildcard-looking id references nothing: %d", len(referrers))
		note("bind: %s", kind(s.UpsertBinding(ctx, &Binding{ID: "b1", TenantID: "t1", RuleID: "r1", Revision: 1, Source: BindingSourceTemplate,
			DeviceConfigID: "00000000-0000-0000-0000-0000000000c1", Params: map[string]string{"a": "1", "b": "2"}, Enabled: true})))
		summary, err := s.SummarizeBindings(ctx, "t1", "00000000-0000-0000-0000-0000000000c1")
		note("binding summary: %s n=%d", kind(err), len(summary))
		for _, b := range summary {
			note("  %s %s %s enabled=%v params=%d", b.RuleID, b.RuleName, b.Source, b.Enabled, b.Params)
		}
		return out
	}
	want, got := run(pg), run(my)
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
