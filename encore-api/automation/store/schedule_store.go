package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Schedule is one durable timed trigger.
type Schedule struct {
	ID            string
	TenantID      string
	RuleID        string
	Kind          string
	Cron          string
	Timezone      string
	RunAt         *time.Time
	MisfirePolicy string
	Enabled       bool
	NextFireAt    *time.Time
	LastFireAt    *time.Time
	LeaseOwner    string
	LeaseUntil    *time.Time
	CreatedAt     time.Time
}

// Fire states. "done" is written only after the run finished.
const (
	FireClaimed = "claimed"
	FireRunning = "running"
	FireDone    = "done"
	FireFailed  = "failed"
	FireSkipped = "skipped"
)

// Fire is one attempted occurrence.
type Fire struct {
	ID           string
	ScheduleID   string
	TenantID     string
	RuleID       string
	ScheduledFor time.Time
	State        string
	RunID        string
	Attempt      int
	Missed       int
	Error        string
	CreatedAt    time.Time
}

const scheduleColumns = `id, tenant_id, rule_id, kind, cron_expr, timezone, run_at,
        misfire_policy, enabled, next_fire_at, last_fire_at, lease_owner, lease_until, created_at`

func scanSchedule(row interface{ Scan(...any) error }) (*Schedule, error) {
	var (
		s        Schedule
		runAt    sql.NullTime
		nextFire sql.NullTime
		lastFire sql.NullTime
		leaseTo  sql.NullTime
		owner    sql.NullString
	)
	if err := row.Scan(&s.ID, &s.TenantID, &s.RuleID, &s.Kind, &s.Cron, &s.Timezone, &runAt,
		&s.MisfirePolicy, &s.Enabled, &nextFire, &lastFire, &owner, &leaseTo, &s.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if runAt.Valid {
		s.RunAt = &runAt.Time
	}
	if nextFire.Valid {
		s.NextFireAt = &nextFire.Time
	}
	if lastFire.Valid {
		s.LastFireAt = &lastFire.Time
	}
	s.LeaseOwner = owner.String
	if leaseTo.Valid {
		s.LeaseUntil = &leaseTo.Time
	}
	return &s, nil
}

// CreateSchedule inserts a schedule. next_fire_at is computed by the caller so
// the stored due time and the response agree.
func (s *Store) CreateSchedule(ctx context.Context, sc *Schedule) error {
	_, err := s.DB.ExecContext(ctx, `
        INSERT INTO auto_schedules (id, tenant_id, rule_id, kind, cron_expr, timezone, run_at,
                                    misfire_policy, enabled, next_fire_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		sc.ID, sc.TenantID, sc.RuleID, sc.Kind, sc.Cron, sc.Timezone, sc.RunAt,
		sc.MisfirePolicy, sc.Enabled, sc.NextFireAt)
	return err
}

// UpdateSchedule rewrites the authored fields and the due time.
func (s *Store) UpdateSchedule(ctx context.Context, sc *Schedule) error {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_schedules
           SET kind=$2, cron_expr=$3, timezone=$4, run_at=$5, misfire_policy=$6,
               enabled=$7, next_fire_at=$8, lease_owner=NULL, lease_until=NULL,
               updated_at=now()
         WHERE id=$1 AND deleted_at IS NULL`,
		sc.ID, sc.Kind, sc.Cron, sc.Timezone, sc.RunAt, sc.MisfirePolicy,
		sc.Enabled, sc.NextFireAt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetScheduleEnabled turns a schedule on or off.
//
// Disabling clears the due time, so a disabled schedule cannot fire even if the
// flag is read stale. Enabling takes the recomputed due time from the caller:
// leaving the old value would fire once for every instant that passed while the
// schedule was off, and leaving it NULL would mean the schedule never fires
// again at all.
func (s *Store) SetScheduleEnabled(ctx context.Context, tenantID, id string, enabled bool, next *time.Time) error {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_schedules
           SET enabled=$3,
               next_fire_at = CASE WHEN $3 THEN $4::timestamptz ELSE NULL END,
               lease_owner = NULL, lease_until = NULL, updated_at=now()
         WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, id, tenantID, enabled, next)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSchedule soft deletes a schedule and clears its due time in the same
// statement, so there is no window where a deleted schedule can still fire.
func (s *Store) DeleteSchedule(ctx context.Context, tenantID, id string) error {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_schedules
           SET deleted_at=now(), enabled=false, next_fire_at=NULL,
               lease_owner=NULL, lease_until=NULL
         WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CancelSchedulesForRule disables every schedule of a rule. It is called when a
// rule is disabled or deleted, so no ghost job survives its owner.
func (s *Store) CancelSchedulesForRule(ctx context.Context, ruleID string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_schedules
           SET enabled=false, next_fire_at=NULL, lease_owner=NULL, lease_until=NULL,
               updated_at=now()
         WHERE rule_id=$1 AND deleted_at IS NULL`, ruleID)
	return err
}

// GetSchedule reads one schedule.
func (s *Store) GetSchedule(ctx context.Context, tenantID, id string) (*Schedule, error) {
	return scanSchedule(s.DB.QueryRowContext(ctx, `
        SELECT `+scheduleColumns+` FROM auto_schedules
         WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, id, tenantID))
}

// ListSchedules lists schedules, optionally filtered by rule.
func (s *Store) ListSchedules(ctx context.Context, tenantID, ruleID string, limit int) ([]*Schedule, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := `SELECT ` + scheduleColumns + ` FROM auto_schedules
           WHERE tenant_id=$1 AND deleted_at IS NULL`
	args := []any{tenantID}
	if ruleID != "" {
		args = append(args, ruleID)
		q += ` AND rule_id=$2`
	}
	q += ` ORDER BY created_at DESC LIMIT ` + itoa(limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// ClaimDueSchedules leases up to limit schedules whose due time has passed.
//
// SKIP LOCKED lets several workers share the scan without ever handing the same
// schedule to two of them. The lease is time based, so a worker that dies stops
// blocking the schedule once the lease expires.
func (s *Store) ClaimDueSchedules(ctx context.Context, now time.Time, limit int, owner string, lease time.Duration) ([]*Schedule, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
        WITH due AS (
            SELECT s.id
              FROM auto_schedules s
              JOIN auto_rules r ON r.id = s.rule_id
             WHERE s.enabled AND s.deleted_at IS NULL
               AND s.next_fire_at IS NOT NULL AND s.next_fire_at <= $1
               AND (s.lease_until IS NULL OR s.lease_until < $1)
               AND r.enabled AND r.deleted_at IS NULL
             ORDER BY s.next_fire_at
             LIMIT $2
             FOR UPDATE OF s SKIP LOCKED
        )
        UPDATE auto_schedules s
           SET lease_owner=$3, lease_until=$1 + $4::interval, updated_at=now()
          FROM due
         WHERE s.id = due.id
        RETURNING `+prefixed(scheduleColumns, "s"), now, limit, owner, lease.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// AdvanceSchedule records the outcome of a materialized occurrence and moves the
// due time forward.
func (s *Store) AdvanceSchedule(ctx context.Context, id string, next *time.Time, last time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_schedules
           SET next_fire_at=$2, last_fire_at=$3, lease_owner=NULL, lease_until=NULL,
               enabled = CASE WHEN $2::timestamptz IS NULL THEN false ELSE enabled END,
               updated_at=now()
         WHERE id=$1`, id, next, last)
	return err
}

// ReleaseSchedule drops the lease without touching the due time, so a transient
// failure is retried on the next pass instead of disabling the schedule.
func (s *Store) ReleaseSchedule(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_schedules
           SET lease_owner=NULL, lease_until=NULL, updated_at=now()
         WHERE id=$1`, id)
	return err
}

// InsertFire records an occurrence. A duplicate occurrence is reported instead
// of being inserted, which is what makes a repeated wake-up harmless.
func (s *Store) InsertFire(ctx context.Context, f *Fire) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
        INSERT INTO auto_schedule_fires (id, schedule_id, tenant_id, rule_id, scheduled_for,
                                         state, missed, lease_owner, lease_until)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
        ON CONFLICT (schedule_id, scheduled_for) DO NOTHING`,
		f.ID, f.ScheduleID, f.TenantID, f.RuleID, f.ScheduledFor,
		f.State, f.Missed, nil, nil)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ClaimFires leases pending work: never-started fires and fires whose worker
// died. The state is moved to running so the crash is visible as an attempt
// that started but never finished.
func (s *Store) ClaimFires(ctx context.Context, now time.Time, limit int, owner string, lease time.Duration) ([]*Fire, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
        WITH work AS (
            SELECT f.id
              FROM auto_schedule_fires f
              JOIN auto_schedules s ON s.id = f.schedule_id
              JOIN auto_rules r ON r.id = f.rule_id
             WHERE f.state IN ('claimed','running')
               AND (f.lease_until IS NULL OR f.lease_until < $1)
               AND s.deleted_at IS NULL
               AND r.deleted_at IS NULL
             ORDER BY f.scheduled_for
             LIMIT $2
             FOR UPDATE OF f SKIP LOCKED
        )
        UPDATE auto_schedule_fires f
           SET state='running', lease_owner=$3, lease_until=$1 + $4::interval,
               attempt=f.attempt+1, updated_at=now()
          FROM work
         WHERE f.id = work.id
        RETURNING f.id, f.schedule_id, f.tenant_id, f.rule_id, f.scheduled_for,
                  f.state, coalesce(f.run_id,''), f.attempt, f.missed, coalesce(f.error,''), f.created_at`,
		now, limit, owner, lease.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Fire
	for rows.Next() {
		var f Fire
		if err := rows.Scan(&f.ID, &f.ScheduleID, &f.TenantID, &f.RuleID, &f.ScheduledFor,
			&f.State, &f.RunID, &f.Attempt, &f.Missed, &f.Error, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

// FinishFire records the terminal state of an occurrence.
func (s *Store) FinishFire(ctx context.Context, id, state, runID, errMsg string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_schedule_fires
           SET state=$2, run_id=COALESCE(NULLIF($3,''), run_id), error=NULLIF($4,''),
               lease_owner=NULL, lease_until=NULL, updated_at=now()
         WHERE id=$1`, id, state, runID, errMsg)
	return err
}

// GetFire reads one occurrence.
func (s *Store) GetFire(ctx context.Context, id string) (*Fire, error) {
	var f Fire
	err := s.DB.QueryRowContext(ctx, `
        SELECT id, schedule_id, tenant_id, rule_id, scheduled_for, state,
               coalesce(run_id,''), attempt, missed, coalesce(error,''), created_at
          FROM auto_schedule_fires WHERE id=$1`, id).
		Scan(&f.ID, &f.ScheduleID, &f.TenantID, &f.RuleID, &f.ScheduledFor, &f.State,
			&f.RunID, &f.Attempt, &f.Missed, &f.Error, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &f, err
}

// ListFires lists the occurrences of a schedule, newest first.
func (s *Store) ListFires(ctx context.Context, scheduleID string, limit int) ([]*Fire, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
        SELECT id, schedule_id, tenant_id, rule_id, scheduled_for, state,
               coalesce(run_id,''), attempt, missed, coalesce(error,''), created_at
          FROM auto_schedule_fires WHERE schedule_id=$1
         ORDER BY scheduled_for DESC LIMIT `+strconv.Itoa(limit), scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Fire
	for rows.Next() {
		var f Fire
		if err := rows.Scan(&f.ID, &f.ScheduleID, &f.TenantID, &f.RuleID, &f.ScheduledFor,
			&f.State, &f.RunID, &f.Attempt, &f.Missed, &f.Error, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

// FindRunByTrigger finds a run created for a specific trigger event, so a
// retried fire reuses it instead of starting a second execution.
func (s *Store) FindRunByTrigger(ctx context.Context, ruleID, triggerEventID string) (*Run, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `
        SELECT id FROM auto_runs
         WHERE rule_id=$1 AND trigger_event_id=$2
         ORDER BY started_at LIMIT 1`, ruleID, triggerEventID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetRunByID(ctx, id)
}

// prefixed qualifies a column list with a table alias, so the same column
// constant can be used in a plain SELECT and in an UPDATE ... RETURNING.
func prefixed(cols, alias string) string {
	var out []string
	for _, field := range strings.Split(cols, ",") {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, alias+"."+field)
		}
	}
	return strings.Join(out, ", ")
}
