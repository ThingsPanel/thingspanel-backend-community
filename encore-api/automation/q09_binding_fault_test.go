package automation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// This connector executes against the actual Encore test database. It only
// inserts a one-shot fault or a lifecycle change between two real SQL reads;
// it does not synthesize candidate rows, bindings, pending rows, or run results.
type q09BindingFaultConnector struct {
	db             *sql.DB
	mu             sync.Mutex
	afterCandidate func()
	beforeExec     func(string) error
	failResolution bool
	faults         int
}

func (c *q09BindingFaultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	return &q09BindingFaultConn{conn: conn, owner: c}, nil
}
func (c *q09BindingFaultConnector) Driver() driver.Driver { return c.db.Driver() }

type q09BindingFaultConn struct {
	conn  *sql.Conn
	owner *q09BindingFaultConnector
}

func (c *q09BindingFaultConn) Close() error { return c.conn.Close() }
func (c *q09BindingFaultConn) Prepare(q string) (driver.Stmt, error) {
	return &q09BindingFaultStmt{c: c, q: q}, nil
}

type q09BindingFaultStmt struct {
	c *q09BindingFaultConn
	q string
}

func (s *q09BindingFaultStmt) Close() error  { return nil }
func (s *q09BindingFaultStmt) NumInput() int { return -1 }
func q09Named(v []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(v))
	for i, x := range v {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: x}
	}
	return out
}
func (s *q09BindingFaultStmt) Exec(v []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), q09Named(v))
}
func (s *q09BindingFaultStmt) Query(v []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), q09Named(v))
}
func (s *q09BindingFaultStmt) ExecContext(ctx context.Context, v []driver.NamedValue) (driver.Result, error) {
	return s.c.ExecContext(ctx, s.q, v)
}
func (s *q09BindingFaultStmt) QueryContext(ctx context.Context, v []driver.NamedValue) (driver.Rows, error) {
	return s.c.QueryContext(ctx, s.q, v)
}
func (c *q09BindingFaultConn) Begin() (driver.Tx, error) {
	return c.conn.BeginTx(context.Background(), nil)
}
func (c *q09BindingFaultConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.IsolationLevel(opts.Isolation), ReadOnly: opts.ReadOnly})
}
func (c *q09BindingFaultConn) CheckNamedValue(*driver.NamedValue) error { return nil }
func q09Args(args []driver.NamedValue) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}
func (c *q09BindingFaultConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if c.owner.beforeExec != nil {
		if err := c.owner.beforeExec(q); err != nil {
			return nil, err
		}
	}
	return c.conn.ExecContext(ctx, q, q09Args(args)...)
}
func (c *q09BindingFaultConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.owner.mu.Lock()
	fail := c.owner.failResolution && strings.Contains(q, "FROM auto_bindings") && strings.Contains(q, "ORDER BY (device_id IS NOT NULL)")
	if fail {
		c.owner.failResolution = false
		c.owner.faults++
	}
	c.owner.mu.Unlock()
	if fail {
		return nil, errors.New("QA transient binding lookup failure")
	}
	rows, err := c.conn.QueryContext(ctx, q, q09Args(args)...)
	if err != nil {
		return nil, err
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}
	buffer := &q09BufferedRows{columns: columns}
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return nil, err
		}
		v := make([]driver.Value, len(values))
		for i, x := range values {
			v[i] = x
		}
		buffer.values = append(buffer.values, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if strings.Contains(q, "FROM auto_rules r") && strings.Contains(q, "SELECT DISTINCT") {
		c.owner.mu.Lock()
		hook := c.owner.afterCandidate
		c.owner.afterCandidate = nil
		c.owner.mu.Unlock()
		if hook != nil {
			hook()
		}
	}
	return buffer, nil
}

type q09BufferedRows struct {
	columns []string
	values  [][]driver.Value
	at      int
}

func (r *q09BufferedRows) Columns() []string { return r.columns }
func (r *q09BufferedRows) Close() error      { return nil }
func (r *q09BufferedRows) Next(dest []driver.Value) error {
	if r.at >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.at])
	r.at++
	return nil
}
func q09FaultRuntime(e *testEnv) *q09BindingFaultConnector {
	c := &q09BindingFaultConnector{db: e.st.DB}
	pool := sql.OpenDB(c)
	e.t.Cleanup(func() { pool.Close() })
	e.rt = runtime.New(store.New(pool), e.cmds, e.notif, e.dir, func() time.Time { return e.now })
	return c
}

func TestQ09BindingFaultConnectorExecutesRealRun(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(e.baseDefinition())
	e.bind(r, "device_config", "cfg-1", nil)
	q09FaultRuntime(e)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	if len(e.runs(r.ID)) != 1 {
		rows, err := e.st.DB.QueryContext(e.ctx(), "SELECT payload FROM auto_events WHERE tenant_id=$1 AND kind='rule.evaluation_error'", e.tenant)
		if err != nil {
			t.Log(err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				t.Log(v)
			}
		}
		t.Fatal("no-fault connector failed real run admission")
	}
}

func TestQ09DetachBetweenCandidateAndResolutionDoesNotFallback(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(e.baseDefinition())
	b := e.bind(r, "device_config", "cfg-1", nil)
	c := q09FaultRuntime(e)
	detached := false
	c.afterCandidate = func() {
		if err := e.st.DetachBinding(e.ctx(), e.tenant, b.ID); err != nil {
			t.Fatal(err)
		}
		e.rt.InvalidateBindings(e.tenant, "")
		detached = true
	}
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	if !detached {
		t.Fatal("candidate/resolution fault seam not exercised")
	}
	if n := len(e.runs(r.ID)); n != 0 {
		t.Fatalf("detached candidate fell back to unbound defaults: runs%d", n)
	}
}

func TestQ09ResolutionDBFailureDoesNotFallback(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(e.baseDefinition())
	e.bind(r, "device_config", "cfg-1", nil)
	c := q09FaultRuntime(e)
	c.afterCandidate = func() { c.mu.Lock(); c.failResolution = true; c.mu.Unlock() }
	_, err := e.rt.Ingest(e.ctx(), store.IngestEvent{EventID: runtime.NewID(), TenantID: e.tenant, DeviceID: e.dev1, Signal: domain.SignalTelemetry, Key: "temperature", Value: 38.0, Quality: domain.QualityGood, ObservedAt: e.now})
	if c.faults != 1 {
		t.Fatal("real binding SQL fault was not injected")
	}
	if n := len(e.runs(r.ID)); n != 0 {
		t.Fatalf("failed binding lookup fell back to unbound: runs%d", n)
	}
	if err == nil {
		t.Fatal("binding resolution failure was silently marked processed")
	}
	// The durable receipt is retried after the transient failure; it is not lost
	// and it cannot cause a second run once marked processed.
	if _, err := e.rt.RecoverAfterRestart(e.ctx(), 100); err != nil {
		t.Fatal(err)
	}
	if len(e.runs(r.ID)) != 1 {
		t.Fatal("resolved receipt did not recover exactly one run")
	}
	if _, err := e.rt.RecoverAfterRestart(e.ctx(), 100); err != nil {
		t.Fatal(err)
	}
	if len(e.runs(r.ID)) != 1 {
		t.Fatal("processed receipt was executed twice")
	}
}

func TestQ09PendingDirectoryFailureDefersAndKeepsDeadline(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(6 * time.Second)
	r := e.createRule(def)
	e.bind(r, "device_config", "cfg-1", nil)
	scope := runtime.ScopeKey(&def, r.ID, e.dev1)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	original, err := e.st.GetPending(e.ctx(), scope)
	if err != nil {
		t.Fatal(err)
	}
	e.advance(6 * time.Second)
	e.rt.InvalidateBindings(e.tenant, "")
	e.dir.err = errors.New("QA temporary directory outage")
	result := e.tick()
	p, err := e.st.GetPending(e.ctx(), scope)
	if err != nil || p == nil {
		t.Fatalf("temporary directory error deleted pending: %v", err)
	}
	if !p.Deadline.Equal(original.Deadline) || !p.StartedAt.Equal(original.StartedAt) || result.PendingDeferred < 1 {
		t.Fatal("directory outage moved/did not defer pending")
	}
	if len(e.runs(r.ID)) != 0 {
		t.Fatal("directory outage permitted run")
	}
	e.dir.err = nil
	e.rt.InvalidateBindings(e.tenant, "")
	e.tick()
	if len(e.runs(r.ID)) != 1 {
		t.Fatal("recovery did not finish original due hold exactly once")
	}
}

func TestQ09PendingBindingDBFailureDefersAndKeepsDeadline(t *testing.T) {
	e := newEnv(t)
	def := e.baseDefinition()
	def.Trigger.For = domain.Duration(6 * time.Second)
	r := e.createRule(def)
	e.bind(r, "device_config", "cfg-1", nil)
	c := q09FaultRuntime(e)
	scope := runtime.ScopeKey(&def, r.ID, e.dev1)
	e.ingest(runtime.NewID(), e.dev1, "temperature", 38.0, "")
	original, err := e.st.GetPending(e.ctx(), scope)
	if err != nil {
		t.Fatal(err)
	}
	e.advance(6 * time.Second)
	c.mu.Lock()
	c.failResolution = true
	c.mu.Unlock()
	result := e.tick()
	p, err := e.st.GetPending(e.ctx(), scope)
	if c.faults != 1 {
		t.Fatal("real binding SQL fault was not injected")
	}
	if err != nil || p == nil {
		t.Fatalf("temporary binding DB error deleted pending: %v", err)
	}
	if !p.Deadline.Equal(original.Deadline) || !p.StartedAt.Equal(original.StartedAt) || result.PendingDeferred < 1 {
		t.Fatal("DB outage moved/did not defer pending")
	}
	if len(e.runs(r.ID)) != 0 {
		t.Fatal("DB outage permitted run")
	}
	e.tick()
	if len(e.runs(r.ID)) != 1 {
		t.Fatal("DB recovery did not finish original due hold exactly once")
	}
}
