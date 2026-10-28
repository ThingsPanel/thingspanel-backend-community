package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CountRunsByStatus counts runs of a scope in a status.
func (s *Store) CountRunsByStatus(ctx context.Context, scopeKey, status string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM auto_runs WHERE scope_key=$1 AND status=$2`, scopeKey, status).Scan(&n)
	return n, err
}

// GetRunByID loads a run without tenant scoping (internal worker path).
func (s *Store) GetRunByID(ctx context.Context, runID string) (*Run, error) {
	var (
		r          Run
		deviceID   sql.NullString
		triggerID  sql.NullString
		reasonCode sql.NullString
		reason     sql.NullString
		snapshot   []byte
		finishedAt sql.NullTime
		deadline   sql.NullTime
	)
	err := s.DB.QueryRowContext(ctx, `
        SELECT id, tenant_id, rule_id, revision, definition_hash, scope_key, generation,
               device_id, trigger_event_id, status, reason_code, reason, snapshot,
               started_at, finished_at, deadline,
               coalesce(causal_root,''), causal_hop, coalesce(cause_event_id,''),
               cursor_pc, resume_count
          FROM auto_runs WHERE id=$1`, runID).
		Scan(&r.ID, &r.TenantID, &r.RuleID, &r.Revision, &r.DefinitionHash, &r.ScopeKey,
			&r.Generation, &deviceID, &triggerID, &r.Status, &reasonCode, &reason, &snapshot,
			&r.StartedAt, &finishedAt, &deadline, &r.CausalRoot, &r.CausalHop, &r.CauseEventID,
			&r.CursorPC, &r.ResumeCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.DeviceID = deviceID.String
	r.TriggerEventID = triggerID.String
	r.ReasonCode = reasonCode.String
	r.Reason = reason.String
	r.Snapshot = snapshot
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
	}
	if deadline.Valid {
		r.Deadline = &deadline.Time
	}
	return &r, nil
}

// MarkRunRunning moves a run into the running state (idempotent).
// MarkRunRunning starts a run that was admitted.
func (s *Store) MarkRunRunning(ctx context.Context, runID string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_runs SET status='running'
         WHERE id=$1 AND status IN ('accepted','queued')`, runID)
	return err
}

// StartQueuedRun starts a run that waited, and restarts its clock.
//
// A queued run's budget must begin when it actually runs, otherwise a run that
// waited most of its TTL would have almost no time left to execute the steps it
// was admitted for.
func (s *Store) StartQueuedRun(ctx context.Context, runID string, deadline time.Time) error {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_runs SET status='running', deadline=$2
         WHERE id=$1 AND status='queued'`, runID, deadline)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// NextQueuedRun returns the oldest queued run of a scope.
//
// The order is the scope's generation, which increases with every admission, so
// it is the arrival order. Ordering by started_at would be wrong: two triggers
// that arrive in the same millisecond share a timestamp, and the tie-break would
// then be a random uuid — the queue would look ordered but fire out of order.
func (s *Store) NextQueuedRun(ctx context.Context, scopeKey string) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `
        SELECT id FROM auto_runs
         WHERE scope_key=$1 AND status='queued'
         ORDER BY generation, id LIMIT 1`, scopeKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// ScheduleWait records the continuation of a waiting run.
func (s *Store) ScheduleWait(ctx context.Context, runID string, pc int, dueAt time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
        INSERT INTO auto_run_timers (run_id, pc, due_at, state)
        VALUES ($1,$2,$3,'pending')
        ON CONFLICT (run_id, pc) DO UPDATE SET due_at=EXCLUDED.due_at, state='pending'`,
		runID, pc, dueAt)
	return err
}

// CancelRunOutbox marks not-yet-dispatched intents of a cancelled run.
func (s *Store) CancelRunOutbox(ctx context.Context, runID string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_outbox SET state='cancelled', updated_at=now()
         WHERE run_id=$1 AND state='pending'`, runID)
	return err
}

// CancelRun closes one run because it was superseded, and stops the work that
// belongs to it. It only touches runs that are still active: a finished run must
// keep the outcome it actually reached.
func (s *Store) CancelRun(ctx context.Context, runID, reason string, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
        UPDATE auto_runs
           SET status='cancelled', reason_code='superseded', reason=$2,
               finished_at=$3, lease_owner=NULL, lease_until=NULL
         WHERE id=$1 AND status IN ('accepted','running','queued')`, runID, reason, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE auto_run_timers SET state='cancelled'
         WHERE run_id=$1 AND state='pending'`, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE auto_outbox SET state='cancelled', updated_at=now()
         WHERE run_id=$1 AND state IN ('pending','dispatched')`, runID); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateTargetStatus records a per-device result.
func (s *Store) UpdateTargetStatus(ctx context.Context, runID, deviceID, status, reasonCode, reason string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_targets SET status=$3, reason_code=NULLIF($4,''), reason=NULLIF($5,''), updated_at=now()
         WHERE run_id=$1 AND device_id=$2`, runID, deviceID, status, reasonCode, reason)
	return err
}

// TargetStatusCounts aggregates per-status counts for a run.
func (s *Store) TargetStatusCounts(ctx context.Context, runID string) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT status, count(*) FROM auto_targets WHERE run_id=$1 GROUP BY status`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var (
			st string
			n  int
		)
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}
