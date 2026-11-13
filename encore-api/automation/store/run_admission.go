package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"encore.app/automation/domain"
)

// lockRunScope coordinates admission and queue promotion, never outbound I/O.
func lockRunScope(ctx context.Context, tx *sql.Tx, scope string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "automation:run-admission:"+scope)
	return err
}

// AdmitRun commits the admission decision and its frozen targets together.
// A stable ID replays the existing decision, including a busy/limit refusal.
func (s *Store) AdmitRun(ctx context.Context, run *Run, targets []Target, def domain.Definition) error {
	return s.admitRun(ctx, run, targets, def, false)
}

// AdmitEventRun also adopts an existing pre-stable-ID event decision. The exact
// tenant/event/rule/device tuple is checked under the normal admission lock.
// If historical duplicates exist, the first decision is reused; no old action
// is cancelled, rewritten or submitted again by this compatibility path.
func (s *Store) AdmitEventRun(ctx context.Context, run *Run, targets []Target, def domain.Definition) error {
	return s.admitRun(ctx, run, targets, def, true)
}
func (s *Store) admitRun(ctx context.Context, run *Run, targets []Target, def domain.Definition, eventIdentity bool) error {
	replay := false
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := lockRunScope(ctx, tx, run.ScopeKey); err != nil {
			return err
		}
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM auto_runs WHERE id=$1 AND tenant_id=$2 AND rule_id=$3`, run.ID, run.TenantID, run.RuleID).Scan(&existing)
		if err == nil {
			replay = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if eventIdentity && run.TriggerEventID != "" {
			err = tx.QueryRowContext(ctx, `SELECT id FROM auto_runs WHERE tenant_id=$1 AND trigger_event_id=$2 AND rule_id=$3 AND coalesce(device_id,'')=$4 ORDER BY started_at,id LIMIT 1`, run.TenantID, run.TriggerEventID, run.RuleID, run.DeviceID).Scan(&existing)
			if err == nil {
				run.ID = existing
				replay = true
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		var active, queued int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status IN ('accepted','running')), count(*) FILTER(WHERE status='queued'), coalesce(max(generation),0)+1 FROM auto_runs WHERE scope_key=$1`, run.ScopeKey).Scan(&active, &queued, &run.Generation); err != nil {
			return err
		}
		refuse := func(status, code, message string) { run.Status = status; run.ReasonCode = code; run.Reason = message }
		switch def.Mode {
		case domain.ModeSingle:
			if active > 0 {
				refuse("ignored", "mode_single_busy", "已有运行未结束，本次按 single 模式忽略")
			}
		case domain.ModeRestart:
			// A failed target lookup must not cancel otherwise valid active work.
			if active > 0 && run.Status == "accepted" {
				rows, err := tx.QueryContext(ctx, `UPDATE auto_runs SET status='cancelled',reason_code='mode_restart',reason='新触发按 restart 模式替换旧运行',finished_at=now() WHERE scope_key=$1 AND status IN ('accepted','running') RETURNING id`, run.ScopeKey)
				if err != nil {
					return err
				}
				var cancelled []string
				for rows.Next() {
					var id string
					if err := rows.Scan(&id); err != nil {
						rows.Close()
						return err
					}
					cancelled = append(cancelled, id)
				}
				rowErr := rows.Err()
				rows.Close()
				if rowErr != nil {
					return rowErr
				}
				for _, id := range cancelled {
					if _, err := tx.ExecContext(ctx, `UPDATE auto_run_timers SET state='cancelled' WHERE run_id=$1 AND state='pending'`, id); err != nil {
						return err
					}
					if _, err := tx.ExecContext(ctx, `UPDATE auto_outbox SET state='cancelled',updated_at=now() WHERE run_id=$1 AND state IN ('pending','dispatched')`, id); err != nil {
						return err
					}
				}
				if _, err := s.PublishEvent(ctx, tx, run.ID+":restart", run.TenantID, "run.cancelled", run.ScopeKey, map[string]any{"scope": run.ScopeKey, "runs": cancelled, "reason": "mode_restart"}); err != nil {
					return err
				}
			}
		case domain.ModeQueued:
			if active > 0 || queued > 0 {
				limit := 10
				if def.Queue != nil && def.Queue.MaxDepth > 0 {
					limit = def.Queue.MaxDepth
				}
				if queued >= limit {
					refuse("rejected", "queue_overflow", fmt.Sprintf("排队已满（%d），本次触发被拒绝", limit))
				} else if run.Status == "accepted" {
					run.Status = "queued"
				}
			}
		case domain.ModeParallel:
			limit := 4
			if def.Parallel != nil && def.Parallel.MaxConcurrent > 0 {
				limit = def.Parallel.MaxConcurrent
			}
			if active >= limit {
				refuse("rejected", "parallel_limit", fmt.Sprintf("并行上限 %d 已满，本次触发被拒绝", limit))
			}
		}
		var snapshot map[string]any
		if err := json.Unmarshal(run.Snapshot, &snapshot); err != nil {
			return err
		}
		snapshot["generation"] = run.Generation
		if run.Status == "rejected" || run.Status == "ignored" {
			snapshot["reason"] = run.ReasonCode
			targets = nil
			run.Deadline = nil
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		run.Snapshot = encoded
		if err := s.createRunTx(ctx, tx, run); err != nil {
			return err
		}
		if err := s.insertTargetsTx(ctx, tx, run.ID, targets); err != nil {
			return err
		}
		if run.Status == "rejected" || run.Status == "ignored" {
			_, err := s.PublishEvent(ctx, tx, run.ID+":decision", run.TenantID, "run."+run.Status, run.ID, map[string]any{"rule_id": run.RuleID, "device_id": run.DeviceID, "reason": run.ReasonCode})
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if replay {
		prior, err := s.GetRun(ctx, run.TenantID, run.ID)
		if err != nil {
			return err
		}
		*run = *prior
	}
	return nil
}
