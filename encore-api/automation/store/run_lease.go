package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RunLeaseDuration is how long a worker owns a run after claiming it. It is
// longer than any single step budget on purpose: the lease is not a timeout for
// the action, it is the "who is walking this program" marker. A worker that dies
// leaves an expired lease, and the next pass can resume without guessing.
const RunLeaseDuration = 2 * time.Minute

// ClaimRun leases a run for one worker.
//
// The condition is deliberately narrow. A run may be claimed when it is
// unfinished *and* nobody holds a live lease on it. Two replicas therefore never
// walk the same program, and re-walking never happens just because a timer fired
// twice.
//
// It returns false when another worker holds a live lease. That is not an error:
// doing nothing is the only safe answer, because walking the program again would
// repeat side effects.
func (s *Store) ClaimRun(ctx context.Context, runID, owner string, now, until time.Time) (bool, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `
        UPDATE auto_runs
           SET status='running',
               lease_owner=$2,
               lease_until=$3,
               resume_count = resume_count + CASE WHEN lease_owner IS NOT NULL THEN 1 ELSE 0 END
         WHERE id=$1
           AND status IN ('accepted','running')
           AND (lease_owner IS NULL OR lease_owner=$2 OR lease_until IS NULL OR lease_until < $4)
        RETURNING id`, runID, owner, until, now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ReleaseRun drops the lease. It is scoped to the owner so a worker that already
// lost its lease cannot clear the lease of the worker that took over.
func (s *Store) ReleaseRun(ctx context.Context, runID, owner string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_runs SET lease_owner=NULL, lease_until=NULL
         WHERE id=$1 AND lease_owner=$2`, runID, owner)
	return err
}

// SetRunCursor records the program counter that is about to run.
//
// It is written *before* the step, not after: the crash window is then "the step
// may or may not have happened", which the step and action identity can absorb.
// Writing it afterwards would leave a window where the step happened but the
// counter still points at it, so a resume would look like a fresh execution of an
// already-performed step with no way to tell them apart.
func (s *Store) SetRunCursor(ctx context.Context, runID string, pc int) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_runs SET cursor_pc=$2 WHERE id=$1`, runID, pc)
	return err
}

// ResumableRuns lists unfinished runs that nobody is currently walking.
//
// A run with a pending wait timer is excluded: its continuation is already
// scheduled, and resuming it now would skip the wait.
func (s *Store) ResumableRuns(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT r.id
          FROM auto_runs r
         WHERE r.status IN ('accepted','running')
           AND (r.lease_owner IS NULL OR r.lease_until IS NULL OR r.lease_until < $1)
           AND (r.deadline IS NULL OR r.deadline > $1)
           AND NOT EXISTS (
                 SELECT 1 FROM auto_run_timers t
                  WHERE t.run_id = r.id AND t.state = 'pending')
         ORDER BY r.started_at
         LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ExpireOverdueRuns finishes runs whose budget is spent.
//
// Without this a run that exceeds max_run_duration while nobody is looking stays
// "running" forever, which reads to an operator as "still working" when it is
// actually dead. The status is `expired`, not `failed`: nothing went wrong with
// the rule, the clock ran out.
func (s *Store) ExpireOverdueRuns(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
        WITH overdue AS (
            SELECT id FROM auto_runs
             WHERE status IN ('accepted','queued','running')
               AND deadline IS NOT NULL AND deadline < $1
             ORDER BY deadline
             LIMIT $2
             FOR UPDATE SKIP LOCKED
        )
        UPDATE auto_runs r
           SET status='expired',
               reason_code=coalesce(nullif(r.snapshot->>'deadline_reason',''), 'run_budget_exceeded'),
               reason=CASE coalesce(nullif(r.snapshot->>'deadline_reason',''), 'run_budget_exceeded')
                        WHEN 'parallel_ttl_exceeded' THEN '并行模式超过最长占用时限，已终止'
                        WHEN 'queue_ttl_exceeded'    THEN '排队超过最长等待时限，已终止'
                        ELSE '运行超过最长时限'
                      END,
               finished_at=$1,
               lease_owner=NULL,
               lease_until=NULL
          FROM overdue
         WHERE r.id = overdue.id
        RETURNING r.id`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
