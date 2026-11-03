package sqldialect

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"unicode/utf8"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// ErrUntranslatable is returned, with the statement text, when MySQL is asked to run a statement the
// translator refuses. Nothing is sent to the server in that case.
var ErrUntranslatable = errors.New("statement cannot be translated for MySQL")

// OpenMySQL returns a *sql.DB on a MySQL 8 server whose connections translate PostgreSQL-style
// statements ($n placeholders and the idioms Rewrite knows). Code written against *sql.DB, *sql.Tx and
// *sql.Conn keeps working unchanged; the translation happens below database/sql.
//
// The DSN must set parseTime=true and clientFoundRows=true.
func OpenMySQL(dsn string) (*sql.DB, error) {
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid MySQL DSN: %w", err)
	}
	if !cfg.ParseTime || !cfg.ClientFoundRows {
		return nil, errors.New("the MySQL DSN must set parseTime=true and clientFoundRows=true")
	}
	inner, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector{inner: inner}), nil
}

type connector struct{ inner driver.Connector }

func (c connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &translatingConn{inner: conn}, nil
}

func (c connector) Driver() driver.Driver { return c.inner.Driver() }

type translatingConn struct{ inner driver.Conn }

func untranslatable(query string) error {
	Unsupported.Add(1)
	return fmt.Errorf("%w: %s", ErrUntranslatable, query)
}

func prepareArgs(query string, args []driver.NamedValue) (string, []driver.NamedValue, error) {
	q, idx, ok := MySQL.RewriteIndexed(query, len(args))
	if !ok {
		return "", nil, untranslatable(query)
	}
	out := make([]driver.NamedValue, len(idx))
	for i, n := range idx {
		v := args[n]
		v.Ordinal = i + 1
		v.Name = ""
		out[i] = v
	}
	return q, out, nil
}

// textBytes sends valid UTF-8 byte slices as text. A []byte parameter reaches MySQL in the binary character
// set, which a JSON column refuses; the same bytes as text are stored unchanged in BLOB columns too.
func textBytes(args []driver.NamedValue) {
	for i, a := range args {
		if b, ok := a.Value.([]byte); ok && utf8.Valid(b) {
			args[i].Value = string(b)
		}
	}
}

func (c *translatingConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *translatingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	q, _, ok := MySQL.RewriteIndexed(query, 1<<20)
	if !ok {
		return nil, untranslatable(query)
	}
	preparer, ok := c.inner.(driver.ConnPrepareContext)
	var stmt driver.Stmt
	var err error
	if ok {
		stmt, err = preparer.PrepareContext(ctx, q)
	} else {
		stmt, err = c.inner.Prepare(q)
	}
	if err != nil {
		return nil, err
	}
	return &translatingStmt{inner: stmt, query: query}, nil
}

func (c *translatingConn) Close() error { return c.inner.Close() }

func (c *translatingConn) Begin() (driver.Tx, error) { return c.inner.Begin() } //nolint:staticcheck

func (c *translatingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if begin, ok := c.inner.(driver.ConnBeginTx); ok {
		return begin.BeginTx(ctx, opts)
	}
	return c.inner.Begin() //nolint:staticcheck
}

func (c *translatingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	q, a, err := prepareArgs(query, args)
	if err != nil {
		return nil, err
	}
	textBytes(a)
	return execer.ExecContext(ctx, q, a)
}

func (c *translatingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	q, a, err := prepareArgs(query, args)
	if err != nil {
		return nil, err
	}
	textBytes(a)
	return queryer.QueryContext(ctx, q, a)
}

func (c *translatingConn) Ping(ctx context.Context) error {
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *translatingConn) ResetSession(ctx context.Context) error {
	if r, ok := c.inner.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *translatingConn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (c *translatingConn) CheckNamedValue(nv *driver.NamedValue) error {
	if checker, ok := c.inner.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// translatingStmt re-maps arguments on every execution: a statement that mentions $1 twice takes one
// caller argument for two server placeholders, so the server statement's parameter count differs from
// the caller's and NumInput must not constrain it.
type translatingStmt struct {
	inner driver.Stmt
	query string
}

func (s *translatingStmt) Close() error  { return s.inner.Close() }
func (s *translatingStmt) NumInput() int { return -1 }

func (s *translatingStmt) Exec(args []driver.Value) (driver.Result, error) { //nolint:staticcheck
	return nil, errors.New("sqldialect: use ExecContext")
}

func (s *translatingStmt) Query(args []driver.Value) (driver.Rows, error) { //nolint:staticcheck
	return nil, errors.New("sqldialect: use QueryContext")
}

func (s *translatingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	_, a, err := prepareArgs(s.query, args)
	if err != nil {
		return nil, err
	}
	textBytes(a)
	if exec, ok := s.inner.(driver.StmtExecContext); ok {
		return exec.ExecContext(ctx, a)
	}
	return nil, errors.New("sqldialect: statement does not support ExecContext")
}

func (s *translatingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	_, a, err := prepareArgs(s.query, args)
	if err != nil {
		return nil, err
	}
	textBytes(a)
	if query, ok := s.inner.(driver.StmtQueryContext); ok {
		return query.QueryContext(ctx, a)
	}
	return nil, errors.New("sqldialect: statement does not support QueryContext")
}
