// Package dbx is the small database interface the Encore owners are written against. It has the shape of
// encore.dev/storage/sqldb (Exec, Query, QueryRow, Begin; Rows and Row with Scan; ErrNoRows), so converting an
// owner is a type rename, and two implementations: Encore's own handle (PostgreSQL) and a database/sql pool
// (MySQL, opened through pkg/sqldialect so the owners' PostgreSQL-style statements are translated).
package dbx

import (
	"context"
	"database/sql"
	"errors"

	"encore.dev/storage/sqldb"
)

// ErrNoRows is reported by Row.Scan when QueryRow finds nothing. It is the same value as sqldb.ErrNoRows.
var ErrNoRows = sqldb.ErrNoRows

// ExecResult is the outcome of Exec.
type ExecResult interface{ RowsAffected() int64 }

// Row is the result of QueryRow.
type Row interface {
	Scan(dest ...any) error
	Err() error
}

// Rows is the result of Query. Close must be called.
type Rows interface {
	Close()
	Scan(dest ...any) error
	Err() error
	Next() bool
}

// Querier is what both a database and a transaction can do.
type Querier interface {
	Exec(ctx context.Context, query string, args ...any) (ExecResult, error)
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
}

// Tx is a transaction.
type Tx interface {
	Querier
	Commit() error
	Rollback() error
}

// DB is a database handle.
type DB interface {
	Querier
	Begin(ctx context.Context) (Tx, error)
	// Dialect reports "postgres" or "mysql" for the few places that must choose a statement form.
	Dialect() string
	// Stdlib returns the underlying database/sql pool for code (and tests) that need it.
	Stdlib() *sql.DB
}

// ---- Encore (PostgreSQL) -------------------------------------------------------------------------------------

type encoreDB struct{ db *sqldb.Database }

// Encore wraps an Encore database. Its Rows and Row already have the interface's methods.
func Encore(db *sqldb.Database) DB { return encoreDB{db} }

func (e encoreDB) Dialect() string { return "postgres" }
func (e encoreDB) Stdlib() *sql.DB { return e.db.Stdlib() }
func (e encoreDB) Exec(ctx context.Context, q string, a ...any) (ExecResult, error) {
	return e.db.Exec(ctx, q, a...)
}
func (e encoreDB) Query(ctx context.Context, q string, a ...any) (Rows, error) {
	rows, err := e.db.Query(ctx, q, a...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
func (e encoreDB) QueryRow(ctx context.Context, q string, a ...any) Row {
	return e.db.QueryRow(ctx, q, a...)
}
func (e encoreDB) Begin(ctx context.Context) (Tx, error) {
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return encoreTx{tx}, nil
}

type encoreTx struct{ tx *sqldb.Tx }

func (t encoreTx) Commit() error   { return t.tx.Commit() }
func (t encoreTx) Rollback() error { return t.tx.Rollback() }
func (t encoreTx) Exec(ctx context.Context, q string, a ...any) (ExecResult, error) {
	return t.tx.Exec(ctx, q, a...)
}
func (t encoreTx) Query(ctx context.Context, q string, a ...any) (Rows, error) {
	rows, err := t.tx.Query(ctx, q, a...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
func (t encoreTx) QueryRow(ctx context.Context, q string, a ...any) Row {
	return t.tx.QueryRow(ctx, q, a...)
}

// ---- database/sql (MySQL, through sqldialect) ------------------------------------------------------------------

type sqlDB struct {
	db      *sql.DB
	dialect string
}

// SQL wraps a database/sql pool. dialect is "mysql" for a pool from sqldialect.OpenMySQL.
func SQL(db *sql.DB, dialect string) DB { return sqlDB{db, dialect} }

func (s sqlDB) Dialect() string { return s.dialect }
func (s sqlDB) Stdlib() *sql.DB { return s.db }
func (s sqlDB) Exec(ctx context.Context, q string, a ...any) (ExecResult, error) {
	return exec(ctx, s.db, q, a)
}
func (s sqlDB) Query(ctx context.Context, q string, a ...any) (Rows, error) {
	return query(ctx, s.db, q, a)
}
func (s sqlDB) QueryRow(ctx context.Context, q string, a ...any) Row {
	return queryRow(ctx, s.db, q, a)
}
func (s sqlDB) Begin(ctx context.Context) (Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return sqlTx{tx}, nil
}

type sqlTx struct{ tx *sql.Tx }

func (t sqlTx) Commit() error   { return t.tx.Commit() }
func (t sqlTx) Rollback() error { return t.tx.Rollback() }
func (t sqlTx) Exec(ctx context.Context, q string, a ...any) (ExecResult, error) {
	return exec(ctx, t.tx, q, a)
}
func (t sqlTx) Query(ctx context.Context, q string, a ...any) (Rows, error) {
	return query(ctx, t.tx, q, a)
}
func (t sqlTx) QueryRow(ctx context.Context, q string, a ...any) Row {
	return queryRow(ctx, t.tx, q, a)
}

type sqlQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type execResult struct{ n int64 }

func (r execResult) RowsAffected() int64 { return r.n }

func exec(ctx context.Context, q sqlQuerier, statement string, args []any) (ExecResult, error) {
	res, err := q.ExecContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		n = 0
	}
	return execResult{n}, nil
}

type sqlRows struct{ rows *sql.Rows }

func (r sqlRows) Close()                 { _ = r.rows.Close() }
func (r sqlRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r sqlRows) Err() error             { return r.rows.Err() }
func (r sqlRows) Next() bool             { return r.rows.Next() }

func query(ctx context.Context, q sqlQuerier, statement string, args []any) (Rows, error) {
	rows, err := q.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	return sqlRows{rows}, nil
}

// sqlRow defers the query error to Scan, as sqldb.Row does.
type sqlRow struct {
	rows *sql.Rows
	err  error
}

func queryRow(ctx context.Context, q sqlQuerier, statement string, args []any) Row {
	rows, err := q.QueryContext(ctx, statement, args...)
	return &sqlRow{rows: rows, err: err}
}

func (r *sqlRow) Err() error { return r.err }

func (r *sqlRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	defer r.rows.Close()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return ErrNoRows
	}
	return r.rows.Scan(dest...)
}

// IsNoRows reports whether err is the not-found answer of Scan.
func IsNoRows(err error) bool { return errors.Is(err, ErrNoRows) }
