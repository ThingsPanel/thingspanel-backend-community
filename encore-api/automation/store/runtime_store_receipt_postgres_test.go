package store_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type receiptInsertReplyLoss struct {
	inner   driver.Connector
	inserts atomic.Int32
	lookups atomic.Int32
}

func (c *receiptInsertReplyLoss) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &receiptInsertReplyLossConn{Conn: conn, owner: c}, nil
}
func (c *receiptInsertReplyLoss) Driver() driver.Driver { return c.inner.Driver() }

type receiptInsertReplyLossConn struct {
	driver.Conn
	owner *receiptInsertReplyLoss
}

func (c *receiptInsertReplyLossConn) CheckNamedValue(v *driver.NamedValue) error {
	if check, ok := c.Conn.(driver.NamedValueChecker); ok {
		return check.CheckNamedValue(v)
	}
	return driver.ErrSkip
}
func (c *receiptInsertReplyLossConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "SELECT seq FROM auto_inbox") {
		c.owner.lookups.Add(1)
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func (c *receiptInsertReplyLossConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	result, err := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
	if err == nil && strings.Contains(q, "INSERT INTO auto_inbox") {
		c.owner.inserts.Add(1)
		return nil, errors.New("synthetic secret SQL after committed insert")
	}
	return result, err
}

const automationReceiptLegacyFixture = "postgres://automation_receipt_test:fixture-only-password@127.0.0.1:25674/automation_receipt_test?sslmode=disable"

// Preserve the previous exact fixture while allowing a separately provisioned
// takeover database on an ephemeral loopback port. Neither arbitrary database
// names nor production users become eligible through a configurable DSN.
func automationReceiptFixtureConfig(dsn string) (*pgx.ConnConfig, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("explicit automation receipt fixture configuration required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid automation receipt fixture configuration")
	}
	if dsn != automationReceiptLegacyFixture && (config.Database != "tp_takeover_automation" || config.User != "automation_fixture") {
		return nil, errors.New("refusing non-fixture automation database or user")
	}
	isLoopback := func(host string) bool {
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	}
	if !isLoopback(config.Host) || config.Port == 0 {
		return nil, errors.New("automation receipt fixture requires an explicit loopback host")
	}
	for _, fallback := range config.Fallbacks {
		if fallback == nil || !isLoopback(fallback.Host) || fallback.Port == 0 {
			return nil, errors.New("automation receipt fixture rejects non-loopback fallback hosts")
		}
	}
	return config, nil
}

func TestAutomationReceiptFixtureIsolation(t *testing.T) {
	for _, test := range []struct {
		name    string
		dsn     string
		allowed bool
	}{
		{"empty_configuration", "", false},
		{"original_exact_fixture", automationReceiptLegacyFixture, true},
		{"takeover_ipv4", "postgres://automation_fixture:fixture-only@127.0.0.1:35432/tp_takeover_automation?sslmode=disable", true},
		{"takeover_ipv6", "postgres://automation_fixture:fixture-only@[::1]:35432/tp_takeover_automation?sslmode=disable", true},
		{"takeover_keyword", "host=127.0.0.1 port=35432 user=automation_fixture dbname=tp_takeover_automation sslmode=disable", true},
		{"different_database", "host=127.0.0.1 port=35432 user=automation_fixture dbname=postgres sslmode=disable", false},
		{"different_user", "host=127.0.0.1 port=35432 user=postgres dbname=tp_takeover_automation sslmode=disable", false},
		{"remote_host", "host=192.0.2.1 port=35432 user=automation_fixture dbname=tp_takeover_automation sslmode=disable", false},
		{"dns_host", "host=localhost port=35432 user=automation_fixture dbname=tp_takeover_automation sslmode=disable", false},
		{"socket_host", "host=/tmp port=35432 user=automation_fixture dbname=tp_takeover_automation sslmode=disable", false},
		{"remote_fallback", "host=127.0.0.1,192.0.2.1 port=35432,35432 user=automation_fixture dbname=tp_takeover_automation sslmode=disable", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := automationReceiptFixtureConfig(test.dsn)
			if test.allowed {
				if err != nil || config == nil {
					t.Fatal("isolated fixture configuration rejected")
				}
			} else if err == nil || config != nil {
				t.Fatal("non-isolated fixture configuration accepted")
			}
		})
	}
}

func TestReceiptConflictVisibilityPostgres(t *testing.T) {
	dsn := os.Getenv("AUTOMATION_RECEIPT_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTOMATION_RECEIPT_TEST_DSN not supplied")
	}
	fixtureConfig, err := automationReceiptFixtureConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*fixtureConfig)
	defer admin.Close()
	var database, user string
	if err := admin.QueryRow("SELECT current_database(),current_user").Scan(&database, &user); err != nil || database != fixtureConfig.Database || user != fixtureConfig.User {
		t.Fatal("fixture identity mismatch")
	}
	schema := "auto_receipt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			t.Error("isolated schema cleanup failed")
		}
	}()
	selected := fixtureConfig.Copy()
	if selected.RuntimeParams == nil {
		selected.RuntimeParams = map[string]string{}
	}
	selected.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*selected)
	defer db.Close()
	db.SetMaxOpenConns(8)
	if _, err := db.Exec(`CREATE SEQUENCE auto_seq`); err != nil {
		t.Fatal("create fixture sequence")
	}
	_, err = db.Exec(`CREATE TABLE auto_inbox (
		event_id text PRIMARY KEY,tenant_id text NOT NULL,device_id text NOT NULL,signal text NOT NULL,key text NOT NULL,
		value jsonb,unit text,quality text NOT NULL,observed_at timestamptz NOT NULL,seq bigint NOT NULL,source text NOT NULL,
		params jsonb,input_class text,gateway_id text,causal_root text,causal_hop integer NOT NULL DEFAULT 0)`)
	if err != nil {
		t.Fatal("create limited receipt fixture")
	}
	type result struct {
		receipt *store.Receipt
		err     error
	}
	for _, commit := range []bool{true, false} {
		name := "rollback"
		if commit {
			name = "commit"
		}
		t.Run(name, func(t *testing.T) {
			id := "event-" + name
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = tx.Exec(`INSERT INTO auto_inbox(event_id,tenant_id,device_id,signal,key,value,quality,observed_at,seq,source) VALUES($1,'tenant','device','telemetry','temp','1','good',now(),42,'fixture')`, id)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			done := make(chan result, 1)
			go func() {
				r, e := store.New(db).InsertEvent(ctx, &store.IngestEvent{EventID: id, TenantID: "tenant", DeviceID: "device", Signal: domain.SignalTelemetry, Key: "temp", Value: 1, ObservedAt: time.Now()})
				done <- result{r, e}
			}()
			deadline := time.Now().Add(5 * time.Second)
			waiting := false
			for time.Now().Before(deadline) {
				var n int
				if err := db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query ILIKE '%INSERT INTO auto_inbox%' AND pid<>pg_backend_pid()`).Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n > 0 {
					waiting = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("conflicting insert did not wait on a PostgreSQL lock")
			}
			if commit {
				err = tx.Commit()
			} else {
				err = tx.Rollback()
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-done:
				if got.err != nil || got.receipt == nil || got.receipt.Duplicate != commit || got.receipt.Seq <= 0 || (commit && got.receipt.Seq != 42) {
					t.Fatalf("receipt=%+v err=%v", got.receipt, got.err)
				}
			case <-ctx.Done():
				t.Fatal("waiting insert did not finish")
			}
		})
	}
	// A trigger suppresses insertion, causing a real zero-row insert and a real
	// SELECT with no rows; it exercises the missing-row branch, not a conflict.
	if _, err := db.Exec(`CREATE FUNCTION fixture_suppress_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_id='missing-after-insert' THEN RETURN NULL; END IF; RETURN NEW; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER suppress_receipt BEFORE INSERT ON auto_inbox FOR EACH ROW EXECUTE FUNCTION fixture_suppress_receipt()`); err != nil {
		t.Fatal(err)
	}
	e := store.IngestEvent{EventID: "missing-after-insert", TenantID: "tenant", DeviceID: "device", Signal: domain.SignalTelemetry, Key: "temp", Value: 1, ObservedAt: time.Now(), Seq: 777}
	receipt, err := store.New(db).InsertEvent(context.Background(), &e)
	if err == nil || receipt != nil || e.Seq != 777 {
		t.Fatalf("missing lookup receipt=%+v seq=%d err=%v", receipt, e.Seq, err)
	}
	// Delegate to a real autocommit PG insert, then lose its successful reply.
	loss := &receiptInsertReplyLoss{inner: stdlib.GetConnector(*selected)}
	lossDB := sql.OpenDB(loss)
	defer lossDB.Close()
	lost := store.IngestEvent{EventID: "lost-insert-response", TenantID: "tenant", DeviceID: "device", Signal: domain.SignalTelemetry, Key: "temp", Value: 1, ObservedAt: time.Now(), Seq: 777}
	lostReceipt, lostErr := store.New(lossDB).InsertEvent(context.Background(), &lost)
	if lostErr == nil || lostReceipt != nil || lost.Seq != 777 || strings.Contains(lostErr.Error(), "synthetic secret") || loss.inserts.Load() != 1 || loss.lookups.Load() != 0 {
		t.Fatalf("lost reply receipt=%+v seq=%d err=%v", lostReceipt, lost.Seq, lostErr)
	}
	var count int
	var persistedSeq int64
	if err := db.QueryRow(`SELECT count(*), COALESCE(MAX(seq), 0) FROM auto_inbox WHERE event_id=$1`, lost.EventID).Scan(&count, &persistedSeq); err != nil || count != 1 || persistedSeq <= 0 {
		t.Fatalf("committed row count=%d seq=%d err=%v", count, persistedSeq, err)
	}
}
