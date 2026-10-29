package automation

import (
	"strings"
	"testing"
	"time"

	"encore.app/automation/domain"
)

func TestParallelCommandsKeepIndependentOwnerGenerations(t *testing.T) {
	e := newEnv(t)
	def := e.withMode(domain.ModeParallel, func(d *domain.Definition) {
		d.Parallel = &domain.ParallelRule{MaxConcurrent: 2, TTL: domain.Duration(5 * time.Minute)}
		d.Steps = append([]domain.Step{{Kind: domain.StepWait, Wait: domain.Duration(time.Minute)}}, d.Steps...)
	})
	rule := e.createRule(def)
	e.ingest("parallel-owner-1", e.dev1, "temperature", 35.0, "")
	e.ingest("parallel-owner-2", e.dev1, "temperature", 36.0, "")
	e.ingest("parallel-owner-rejected", e.dev1, "temperature", 37.0, "")
	e.advance(2 * time.Minute)
	e.tick()

	e.cmds.mu.Lock()
	defer e.cmds.mu.Unlock()
	if len(e.cmds.calls) != 2 {
		t.Fatalf("want two legal commands, got %d", len(e.cmds.calls))
	}
	// The owner validates again after acceptance. Model its highest generation
	// per scope, then validate both queued commands after both were accepted.
	latest := map[string]int64{}
	for _, req := range e.cmds.calls {
		if req.Generation > latest[req.ScopeKey] {
			latest[req.ScopeKey] = req.Generation
		}
	}
	for _, req := range e.cmds.calls {
		if latest[req.ScopeKey] != req.Generation {
			t.Fatalf("legitimate parallel command fenced after acceptance: %+v", req)
		}
		runID := strings.SplitN(req.IdempotencyKey, ":", 2)[0]
		run, err := e.st.GetRunByID(e.ctx(), runID)
		if err != nil || run.RuleID != rule.ID || req.ScopeKey != run.ScopeKey+":run:"+run.ID {
			t.Fatalf("command scope must use the stable run identity: %+v, %v", req, err)
		}
	}
}

func TestSequentialModesKeepSharedOwnerGenerationScope(t *testing.T) {
	for _, mode := range []domain.RunMode{domain.ModeSingle, domain.ModeRestart, domain.ModeQueued} {
		t.Run(string(mode), func(t *testing.T) {
			e := newEnv(t)
			rule := e.createRule(e.withMode(mode, nil))
			e.ingest("shared-generation-scope", e.dev1, "temperature", 35.0, "")
			req, ok := e.cmds.last()
			runs := e.runs(rule.ID)
			if !ok || len(runs) != 1 || req.ScopeKey != runs[0].ScopeKey {
				t.Fatalf("%s must retain shared generation fencing: %+v", mode, req)
			}
		})
	}
}
