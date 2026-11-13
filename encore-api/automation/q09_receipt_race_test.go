package automation

import (
	"context"
	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// The barrier runs after real CandidateRules SQL has read the live receipt's
// candidate. The receipt and current sample are durable; no processing marker
// has been written. A separate Runtime then performs normal restart recovery.
func TestQ09LiveAndRecoveryMustNotEvaluateOneReceiptTwice(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	recovery := runtime.New(e.st, e.cmds, e.notif, e.dir, func() time.Time { return e.now })
	fault := q09FaultRuntime(e)
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	ctx, cancel := context.WithTimeout(e.ctx(), 20*time.Second)
	defer cancel()
	fault.afterCandidate = func() {
		close(reached)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	eventID := runtime.NewID()
	done := make(chan error, 1)
	go func() {
		_, err := e.rt.Ingest(ctx, store.IngestEvent{EventID: eventID, TenantID: e.tenant, DeviceID: e.dev1, Signal: domain.SignalTelemetry, Key: "temperature", Value: 38.0, Quality: domain.QualityGood, ObservedAt: e.now})
		done <- err
	}()
	select {
	case <-reached:
	case <-ctx.Done():
		t.Fatal("live SQL barrier not reached")
	}
	receipts, err := e.st.UnprocessedEvents(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, v := range receipts {
		if v.EventID == eventID {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("want exactly one durable unprocessed source, got %d", found)
	}
	if _, err := recovery.RecoverAfterRestart(ctx, 100); err != nil {
		t.Fatal(err)
	}
	t.Logf("after recovery before live release: runs=%d commands=%d", len(e.runs(rule.ID)), e.cmds.count())
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("live did not finish")
	}
	runs := e.runs(rule.ID)
	for _, v := range runs {
		t.Logf("run=%s status=%s reason=%s", v.ID, v.Status, v.Reason)
	}
	t.Logf("one receipt=%s runs=%d dispatched_commands=%d", eventID, len(runs), e.cmds.count())
	if len(runs) != 1 {
		t.Fatalf("one durable source evaluated twice: want1 run got%d", len(runs))
	}
}

func TestQ09EventRunReplaysAfterActionBeforeProcessed(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	fault := q09FaultRuntime(e)
	fail := true
	fault.beforeExec = func(q string) error {
		if fail && strings.Contains(q, "UPDATE auto_inbox SET processed_at") {
			fail = false
			return errors.New("QA crash before receipt marker")
		}
		return nil
	}
	event := store.IngestEvent{EventID: runtime.NewID(), TenantID: e.tenant, DeviceID: e.dev1, Signal: domain.SignalTelemetry, Key: "temperature", Value: 38.0, Quality: domain.QualityGood, ObservedAt: e.now}
	if _, err := e.rt.Ingest(e.ctx(), event); err == nil {
		t.Fatal("marker fault not exercised")
	}
	runs := e.runs(rule.ID)
	if len(runs) != 1 || e.cmds.count() != 1 {
		t.Fatal("action did not finish before marker fault")
	}
	before := runs[0]
	// Mutable revision changes cannot manufacture another identity for a receipt.
	rule.Definition.Steps[0].Value = "2"
	rule.DefinitionHash = domain.DefinitionHash(rule.Definition)
	if err := e.st.UpdateRule(e.ctx(), rule, rule.Version); err != nil {
		t.Fatal(err)
	}
	recovered := runtime.New(e.st, e.cmds, e.notif, e.dir, func() time.Time { return e.now })
	if _, err := recovered.RecoverAfterRestart(e.ctx(), 100); err != nil {
		t.Fatal(err)
	}
	after := e.runs(rule.ID)
	if len(after) != 1 || e.cmds.count() != 1 || after[0].ID != before.ID || after[0].Revision != before.Revision || after[0].Generation != before.Generation || string(after[0].Snapshot) != string(before.Snapshot) {
		t.Fatal("completed event was reexecuted or its frozen decision changed")
	}
	var outboxes int
	if err := e.st.DB.QueryRowContext(e.ctx(), "SELECT count(*) FROM auto_outbox WHERE run_id=$1", before.ID).Scan(&outboxes); err != nil {
		t.Fatal(err)
	}
	if outboxes != 1 {
		t.Fatalf("want one outbox got%d", outboxes)
	}
}

func TestQ09EventRunReplayPreservesEveryModeDecision(t *testing.T) {
	for _, mode := range []domain.RunMode{domain.ModeSingle, domain.ModeRestart, domain.ModeQueued, domain.ModeParallel} {
		t.Run(string(mode), func(t *testing.T) {
			e := newEnv(t)
			def := e.baseDefinition()
			def.Mode = mode
			def.Steps = []domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(time.Minute)}}
			if mode == domain.ModeQueued {
				def.Queue = &domain.QueueRule{TTL: domain.Duration(5 * time.Minute), MaxDepth: 5}
			}
			if mode == domain.ModeParallel {
				def.Parallel = &domain.ParallelRule{TTL: domain.Duration(5 * time.Minute), MaxConcurrent: 2}
			}
			rule := e.createRule(def)
			e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
			e.ingest(runtime.NewID(), e.dev1, "temperature", 39.0, "")
			before := e.runs(rule.ID)
			if len(before) != 2 {
				t.Fatal("distinct events must retain two mode decisions")
			}
			if _, err := e.st.DB.ExecContext(e.ctx(), "UPDATE auto_inbox SET processed_at=NULL WHERE tenant_id=$1", e.tenant); err != nil {
				t.Fatal(err)
			}
			recovered := runtime.New(e.st, e.cmds, e.notif, e.dir, func() time.Time { return e.now })
			if _, err := recovered.RecoverAfterRestart(e.ctx(), 100); err != nil {
				t.Fatal(err)
			}
			after := e.runs(rule.ID)
			if len(after) != 2 {
				t.Fatal("recovery made extra mode decision")
			}
			for _, old := range before {
				now := e.run(old.ID)
				if now.Status != old.Status || now.Generation != old.Generation || now.ReasonCode != old.ReasonCode || string(now.Snapshot) != string(old.Snapshot) {
					t.Fatalf("%s replay changed mode decision", mode)
				}
			}
		})
	}
}

type q09CrashAfterFirstActionDirectory struct {
	ports.Directory
	commands func() int
	crash    bool
}

func (d *q09CrashAfterFirstActionDirectory) ByIDs(ctx context.Context, tenant string, ids []string) ([]ports.Device, error) {
	if d.crash && d.commands() == 1 {
		d.crash = false
		panic("QA process stops after first rule action")
	}
	return d.Directory.ByIDs(ctx, tenant, ids)
}
func TestQ09PartialMultiRuleReceiptDoesNotRepeatCompletedAction(t *testing.T) {
	e := newEnv(t)
	a := e.createRule(e.baseDefinition())
	b := e.createRule(e.baseDefinition())
	dir := &q09CrashAfterFirstActionDirectory{Directory: e.dir, commands: e.cmds.count, crash: true}
	live := runtime.New(e.st, e.cmds, e.notif, dir, func() time.Time { return e.now })
	crashed := false
	func() {
		defer func() {
			if recover() != nil {
				crashed = true
			}
		}()
		_, err := live.Ingest(e.ctx(), store.IngestEvent{EventID: runtime.NewID(), TenantID: e.tenant, DeviceID: e.dev1, Signal: domain.SignalTelemetry, Key: "temperature", Value: 38.0, Quality: domain.QualityGood, ObservedAt: e.now})
		if err != nil {
			t.Fatal(err)
		}
	}()
	if !crashed || len(e.runs(a.ID))+len(e.runs(b.ID)) != 1 || e.cmds.count() != 1 {
		t.Fatal("partial multi-rule crash seam not reached")
	}
	if _, err := e.rt.RecoverAfterRestart(e.ctx(), 100); err != nil {
		t.Fatal(err)
	}
	if len(e.runs(a.ID)) != 1 || len(e.runs(b.ID)) != 1 || e.cmds.count() != 2 {
		t.Fatal("partial recovery lost or repeated an action")
	}
	if _, err := e.rt.RecoverAfterRestart(e.ctx(), 100); err != nil {
		t.Fatal(err)
	}
	if e.cmds.count() != 2 {
		t.Fatal("completed receipt replayed an action")
	}
}
func TestQ09PendingWindowsRetainIndependentRunIdentities(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(time.Second)
	rule := e.createRule(def)
	for i := 0; i < 2; i++ {
		e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
		e.advance(time.Second)
		e.tick()
		e.ingest(runtime.NewID(), e.dev1, "temperature", 20.0, "")
		e.advance(time.Second)
	}
	runs := e.runs(rule.ID)
	if len(runs) != 2 || runs[0].ID == runs[1].ID || e.cmds.count() != 2 {
		t.Fatal("independent timer windows incorrectly deduplicated")
	}
}

func TestQ09RecoveryEnforcesCausalHopLimit(t *testing.T) {
	e := newEnv(t)
	rule := e.createRule(e.baseDefinition())
	event := store.IngestEvent{EventID: runtime.NewID(), TenantID: e.tenant, DeviceID: e.dev1, Signal: domain.SignalTelemetry, Key: "temperature", Value: 38.0, Quality: domain.QualityGood, ObservedAt: e.now, CausalHop: runtime.MaxCausalHop + 1}
	if _, err := e.st.InsertEvent(e.ctx(), &event); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rt.RecoverAfterRestart(e.ctx(), 100); err != nil {
		t.Fatal(err)
	}
	if len(e.runs(rule.ID)) != 0 || e.cmds.count() != 0 {
		t.Fatal("recovery bypassed live causal-hop guard")
	}
	processed, err := e.st.ReceiptProcessed(e.ctx(), e.tenant, e.dev1, event.EventID)
	if err != nil || !processed {
		t.Fatal("rejected cyclic receipt not finalized")
	}
}
