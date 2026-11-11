package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RunTimer is a durable continuation point for a wait step.
type RunTimer struct {
	RunID string
	PC    int
	DueAt time.Time
	State string
}

// ScheduleRunTimer records the continuation of a run.
func (s *Store) ScheduleRunTimer(ctx context.Context, tx *sql.Tx, runID string, pc int, dueAt time.Time) error {
	_, err := tx.ExecContext(ctx, `
        INSERT INTO auto_run_timers (run_id, pc, due_at, state)
        VALUES ($1,$2,$3,'pending')
        ON CONFLICT (run_id, pc) DO UPDATE SET due_at=EXCLUDED.due_at, state='pending'`,
		runID, pc, dueAt)
	return err
}

// DueRunTimers claims run continuations that are due.
func (s *Store) DueRunTimers(ctx context.Context, now time.Time, limit int) ([]*RunTimer, error) {
	if s.MySQL {
		return s.dueRunTimersMySQL(ctx, now, limit)
	}
	rows, err := s.DB.QueryContext(ctx, `
        WITH due AS (
            SELECT run_id, pc FROM auto_run_timers
             WHERE state='pending' AND due_at <= $1
             ORDER BY due_at LIMIT $2
             FOR UPDATE SKIP LOCKED
        )
        UPDATE auto_run_timers t SET state='fired'
          FROM due WHERE t.run_id=due.run_id AND t.pc=due.pc
        RETURNING t.run_id, t.pc, t.due_at, t.state`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RunTimer
	for rows.Next() {
		var t RunTimer
		if err := rows.Scan(&t.RunID, &t.PC, &t.DueAt, &t.State); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// CancelRunTimers drops pending continuations of a run (restart mode).
func (s *Store) CancelRunTimers(ctx context.Context, runID string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_run_timers SET state='cancelled' WHERE run_id=$1 AND state='pending'`, runID)
	return err
}

// RunByIDTx is used inside a transaction when finishing a run.
func (s *Store) RunStatus(ctx context.Context, runID string) (string, error) {
	var st string
	err := s.DB.QueryRowContext(ctx, `SELECT status FROM auto_runs WHERE id=$1`, runID).Scan(&st)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return st, err
}

// dueRunTimersMySQL claims timers without UPDATE ... FROM / RETURNING: lock the due rows with SKIP LOCKED,
// mark exactly those rows fired, and return them with the state they now have.
func (s *Store) dueRunTimersMySQL(ctx context.Context, now time.Time, limit int) ([]*RunTimer, error) {
	var out []*RunTimer
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		out = nil
		rows, err := tx.QueryContext(ctx, `
            SELECT run_id, pc, due_at FROM auto_run_timers
             WHERE state='pending' AND due_at <= $1
             ORDER BY due_at LIMIT $2
             FOR UPDATE SKIP LOCKED`, now, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			t := &RunTimer{State: "fired"}
			if err := rows.Scan(&t.RunID, &t.PC, &t.DueAt); err != nil {
				rows.Close()
				return err
			}
			out = append(out, t)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil || len(out) == 0 {
			return err
		}
		var args []any
		marks := make([]string, len(out))
		for i, t := range out {
			args = append(args, t.RunID, t.PC)
			marks[i] = fmt.Sprintf("($%d,$%d)", 2*i+1, 2*i+2)
		}
		_, err = tx.ExecContext(ctx, `UPDATE auto_run_timers SET state='fired' WHERE (run_id, pc) IN (`+strings.Join(marks, ",")+`)`, args...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
