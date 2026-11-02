package store_test

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

type receiptFixture struct {
	mode   string
	mu     sync.Mutex
	calls  []string
	cancel context.CancelFunc
}

func (f *receiptFixture) add(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}
func (f *receiptFixture) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type receiptConnector struct{ state *receiptFixture }

func (c *receiptConnector) Connect(context.Context) (driver.Conn, error) {
	return &receiptConn{state: c.state}, nil
}
func (c *receiptConnector) Driver() driver.Driver { return receiptDriver{} }

type receiptDriver struct{}

func (receiptDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("unexpected fixture open")
}

type receiptConn struct{ state *receiptFixture }

func (*receiptConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*receiptConn) Close() error              { return nil }
func (*receiptConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c *receiptConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(q, "INSERT INTO auto_inbox") {
		c.state.add("unexpected-exec")
		return nil, errors.New("unexpected projection")
	}
	c.state.add("insert")
	if c.state.mode == "insert-error" {
		return nil, errors.New("synthetic secret SQL")
	}
	if c.state.mode == "insert-cancel" {
		return nil, context.Canceled
	}
	if c.state.mode == "insert-deadline" {
		return nil, context.DeadlineExceeded
	}
	if c.state.mode == "rows-error" {
		return receiptResult{rowsErr: errors.New("synthetic secret SQL")}, nil
	}
	if c.state.mode == "rows-invalid" {
		return receiptResult{n: -1}, nil
	}
	if c.state.mode == "new" {
		return receiptResult{n: 1}, nil
	}
	return receiptResult{n: 0}, nil
}
func (c *receiptConn) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "nextval('auto_seq')") {
		c.state.add("next-seq")
		switch c.state.mode {
		case "next-error":
			return nil, errors.New("synthetic secret SQL")
		case "next-cancel":
			return nil, context.Canceled
		case "next-cancel-live":
			c.state.cancel()
			<-ctx.Done()
			return nil, ctx.Err()
		case "next-deadline":
			return nil, context.DeadlineExceeded
		}
		return &receiptRows{value: int64(9)}, nil
	}
	if !strings.Contains(q, "SELECT seq FROM auto_inbox") {
		c.state.add("unexpected-query")
		return nil, errors.New("unexpected query")
	}
	c.state.add("lookup")
	switch c.state.mode {
	case "query-error":
		return nil, errors.New("synthetic secret SQL")
	case "cancel":
		return nil, context.Canceled
	case "cancel-live":
		c.state.cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	case "deadline":
		return nil, context.DeadlineExceeded
	case "no-rows":
		return &receiptRows{empty: true}, nil
	case "scan-error":
		return &receiptRows{value: "not-an-integer"}, nil
	case "zero":
		return &receiptRows{value: int64(0)}, nil
	case "negative":
		return &receiptRows{value: int64(-1)}, nil
	default:
		return &receiptRows{value: int64(42)}, nil
	}
}

type receiptResult struct {
	n       int64
	rowsErr error
}

func (r receiptResult) LastInsertId() (int64, error) { return 0, errors.New("unsupported") }
func (r receiptResult) RowsAffected() (int64, error) { return r.n, r.rowsErr }

type receiptRows struct {
	value       driver.Value
	empty, read bool
}

func (*receiptRows) Columns() []string { return []string{"value"} }
func (*receiptRows) Close() error      { return nil }
func (r *receiptRows) Next(dest []driver.Value) error {
	if r.empty || r.read {
		return io.EOF
	}
	r.read = true
	dest[0] = r.value
	return nil
}

func TestInsertEventReceiptOutcomes(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		duplicate bool
		seq       int64
		wantErr   error
	}{
		{"new", false, 9, nil}, {"duplicate", true, 42, nil}, {"query-error", false, 0, errors.New("failure")},
		{"cancel", false, 0, context.Canceled}, {"cancel-live", false, 0, context.Canceled}, {"deadline", false, 0, context.DeadlineExceeded},
		{"no-rows", false, 0, errors.New("failure")}, {"scan-error", false, 0, errors.New("failure")},
		{"zero", false, 0, errors.New("failure")}, {"negative", false, 0, errors.New("failure")},
		{"rows-error", false, 0, errors.New("failure")}, {"rows-invalid", false, 0, errors.New("failure")},
		{"insert-error", false, 0, errors.New("failure")}, {"insert-cancel", false, 0, context.Canceled}, {"insert-deadline", false, 0, context.DeadlineExceeded},
		{"next-error", false, 0, errors.New("failure")}, {"next-cancel", false, 0, context.Canceled}, {"next-cancel-live", false, 0, context.Canceled}, {"next-deadline", false, 0, context.DeadlineExceeded},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f := &receiptFixture{mode: tc.mode}
			db := sql.OpenDB(&receiptConnector{state: f})
			defer db.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.cancel = cancel
			e := store.IngestEvent{EventID: "event", TenantID: "tenant", DeviceID: "device", Signal: domain.SignalTelemetry, Key: "temp", Value: 1, ObservedAt: time.Now(), Seq: 777}
			receipt, err := store.New(db).InsertEvent(ctx, &e)
			if tc.wantErr == nil {
				if err != nil || receipt == nil || receipt.Seq != tc.seq || receipt.Duplicate != tc.duplicate || e.Seq != tc.seq {
					t.Fatalf("receipt=%+v seq=%d err=%v", receipt, e.Seq, err)
				}
				return
			}
			if err == nil || receipt != nil || e.Seq != 777 || strings.Contains(err.Error(), "synthetic secret") {
				t.Fatalf("receipt=%+v seq=%d err=%v", receipt, e.Seq, err)
			}
			if (tc.mode == "cancel" || tc.mode == "cancel-live" || tc.mode == "deadline" || tc.mode == "insert-cancel" || tc.mode == "insert-deadline" || tc.mode == "next-cancel" || tc.mode == "next-cancel-live" || tc.mode == "next-deadline") && !errors.Is(err, tc.wantErr) {
				t.Fatalf("context error not preserved: %v", err)
			}
			if (tc.mode == "rows-error" || tc.mode == "rows-invalid" || strings.HasPrefix(tc.mode, "insert-")) && strings.Contains(strings.Join(f.snapshot(), ","), "lookup") {
				t.Fatal("queried duplicate after invalid affected count")
			}
			if strings.HasPrefix(tc.mode, "next-") && (len(f.snapshot()) != 1 || f.snapshot()[0] != "next-seq" || !strings.HasPrefix(err.Error(), "event sequence unavailable")) {
				t.Fatalf("sequence error crossed INSERT boundary: calls=%v err=%v", f.snapshot(), err)
			}
		})
	}
}

func TestIngestReceiptFailureDoesNotProject(t *testing.T) {
	for _, mode := range []string{"query-error", "cancel", "deadline", "no-rows", "scan-error", "zero", "negative", "rows-error", "rows-invalid", "insert-error", "insert-cancel", "insert-deadline", "next-error", "next-cancel", "next-deadline"} {
		t.Run(mode, func(t *testing.T) {
			f := &receiptFixture{mode: mode}
			db := sql.OpenDB(&receiptConnector{state: f})
			defer db.Close()
			rt := runtime.New(store.New(db), nil, nil, nil, nil)
			receipt, err := rt.Ingest(context.Background(), store.IngestEvent{EventID: "event", TenantID: "tenant", DeviceID: "device", Signal: domain.SignalTelemetry, Key: "temp", Value: 1})
			if err == nil || receipt != nil {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
			if strings.Contains(strings.Join(f.snapshot(), ","), "unexpected") {
				t.Fatal("runtime reached projection or evaluation")
			}
			if strings.HasPrefix(mode, "next-") && (len(f.snapshot()) != 1 || f.snapshot()[0] != "next-seq") {
				t.Fatalf("runtime crossed sequence boundary: %v", f.snapshot())
			}
		})
	}
}

type maliciousReceiptValue struct{}

func (maliciousReceiptValue) MarshalJSON() ([]byte, error) {
	return nil, errors.New("synthetic secret from marshaler")
}

func TestIngestEncodingFailurePrecedesAllSQL(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		params map[string]any
	}{
		{"value-marshaler", maliciousReceiptValue{}, nil},
		{"value-unsupported", make(chan int), nil},
		{"params-marshaler", 1, map[string]any{"bad": maliciousReceiptValue{}}},
		{"params-unsupported", 1, map[string]any{"bad": make(chan int)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &receiptFixture{mode: "new"}
			db := sql.OpenDB(&receiptConnector{state: f})
			defer db.Close()
			e := store.IngestEvent{EventID: "event", TenantID: "tenant", DeviceID: "device", Signal: domain.SignalTelemetry, Key: "temp", Value: tc.value, Params: tc.params, Seq: 777}
			receipt, err := store.New(db).InsertEvent(context.Background(), &e)
			if err == nil || err.Error() != "event payload encoding failed" || receipt != nil || e.Seq != 777 || len(f.snapshot()) != 0 {
				t.Fatalf("receipt=%+v seq=%d err=%v calls=%v", receipt, e.Seq, err, f.snapshot())
			}
			rt := runtime.New(store.New(db), nil, nil, nil, nil)
			receipt, err = rt.Ingest(context.Background(), e)
			if err == nil || err.Error() != "event payload encoding failed" || receipt != nil || len(f.snapshot()) != 0 {
				t.Fatalf("runtime receipt=%+v err=%v calls=%v", receipt, err, f.snapshot())
			}
		})
	}
}
