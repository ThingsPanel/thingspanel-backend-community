package store

import (
	"context"
	"database/sql"
)

// dbtx is the small slice of *sql.DB and *sql.Tx this package needs.
//
// It exists so a lifecycle change and the event that announces it can be written
// in one transaction without every helper growing a second copy. The alarm path
// needs that: an episode that clears but whose "cleared" event is lost is exactly
// the kind of half-state that makes a downstream linkage fire for an alarm that
// is already gone.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Store) pick(tx *sql.Tx) dbtx {
	if tx != nil {
		return tx
	}
	return s.DB
}

func (s *Store) exec(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return s.pick(tx).ExecContext(ctx, query, args...)
}

func (s *Store) query(ctx context.Context, tx *sql.Tx, query string, args ...any) (*sql.Rows, error) {
	return s.pick(tx).QueryContext(ctx, query, args...)
}

func (s *Store) row(ctx context.Context, tx *sql.Tx, query string, args ...any) *sql.Row {
	return s.pick(tx).QueryRowContext(ctx, query, args...)
}

// WithTx runs fn inside a transaction. It is the only way the alarm path writes
// more than one row for a single lifecycle decision.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// PolicyKey is the stable business key of an alarm policy.
//
// It falls back to the rule id so every policy created before the key existed
// keeps exactly the identity it already had: an in-flight alarm must not be
// re-opened (or duplicated) by a migration.
func (r *Rule) PolicyKey() string {
	if r == nil {
		return ""
	}
	if r.Definition.Alarm != nil {
		if k := r.Definition.Alarm.Key; k != "" {
			return k
		}
	}
	return r.ID
}

// AlarmLevel returns the declared grade, defaulting to the legacy "M" so a list
// that groups by level never sees an empty bucket for an older policy.
func (r *Rule) AlarmLevel() string {
	if r != nil && r.Definition.Alarm != nil && r.Definition.Alarm.Level != "" {
		return r.Definition.Alarm.Level
	}
	return "M"
}

// AlarmDimension returns the declared dimension of the policy's alarms.
func (r *Rule) AlarmDimension() string {
	if r != nil && r.Definition.Alarm != nil {
		return r.Definition.Alarm.Dimension
	}
	return ""
}
