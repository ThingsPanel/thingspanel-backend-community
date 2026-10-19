package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	defaultLimit = 50
	maxLimit     = 100
	queryTimeout = 5 * time.Second
)

var errSchemaIncomplete = errors.New("broker audit schema is incomplete")

// Cursor is an opaque, tenant-scoped pagination position ordered by created_at
// and a kind-prefixed record ID.
type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

type Record struct {
	Kind       string    `json:"kind"`
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Count      int64     `json:"count,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	RelatedID  string    `json:"related_id,omitempty"`
	RouteEpoch int64     `json:"route_epoch,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

type Output struct {
	TenantID   string   `json:"tenant_id"`
	Records    []Record `json:"records"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

type options struct {
	tenant  string
	limit   int
	cursor  string
	dsnFile string
}

func main() {
	code := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	os.Exit(code)
}

func run(parent context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tp-broker-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tp-broker-audit --tenant TENANT_ID [--limit 1..100] [--cursor BASE64URL] [--dsn-file PATH]")
		fmt.Fprintln(stderr, "Read-only summary of broker receipts, commands, effects, quarantine, state, and automation actions.")
		fmt.Fprintln(stderr, "Database DSN comes from TP_BROKER_AUDIT_DSN or --dsn-file; it is never printed.")
	}
	var opt options
	fs.StringVar(&opt.tenant, "tenant", "", "required tenant ID scope")
	fs.IntVar(&opt.limit, "limit", defaultLimit, "page size (1..100)")
	fs.StringVar(&opt.cursor, "cursor", "", "opaque cursor returned by the previous page")
	fs.StringVar(&opt.dsnFile, "dsn-file", "", "read DSN from a file instead of TP_BROKER_AUDIT_DSN")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 64
	}
	if fs.NArg() != 0 || strings.TrimSpace(opt.tenant) == "" || len(opt.tenant) > 256 {
		fmt.Fprintln(stderr, "invalid arguments: --tenant is required and positional arguments are not accepted")
		return 64
	}
	if opt.limit < 1 || opt.limit > maxLimit {
		fmt.Fprintln(stderr, "invalid --limit: expected an integer from 1 to 100")
		return 64
	}
	cursor, err := decodeCursor(opt.cursor)
	if err != nil {
		fmt.Fprintln(stderr, "invalid --cursor: expected a valid cursor returned by this command")
		return 64
	}
	dsn, err := readDSN(opt.dsnFile, getenv)
	if err != nil {
		fmt.Fprintln(stderr, "database DSN unavailable: set TP_BROKER_AUDIT_DSN or provide a readable --dsn-file")
		return 64
	}

	ctx, cancel := context.WithTimeout(parent, queryTimeout)
	defer cancel()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		fmt.Fprintln(stderr, "database connection failed")
		return 69
	}
	sqlDB, err := db.DB()
	if err != nil {
		fmt.Fprintln(stderr, "database connection failed")
		return 69
	}
	defer sqlDB.Close()
	if err := sqlDB.PingContext(ctx); err != nil {
		fmt.Fprintln(stderr, "database connection failed")
		return 69
	}

	out, err := query(ctx, db, opt.tenant, opt.limit, cursor)
	if err != nil {
		if errors.Is(err, errSchemaIncomplete) {
			fmt.Fprintln(stderr, "broker audit schema is incomplete")
			return 78
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fmt.Fprintln(stderr, "broker audit query timed out after 5 seconds")
			return 75
		}
		fmt.Fprintln(stderr, "broker audit query failed")
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(stderr, "JSON output failed")
		return 1
	}
	return 0
}

func readDSN(file string, getenv func(string) string) (string, error) {
	if file == "" {
		v := strings.TrimSpace(getenv("TP_BROKER_AUDIT_DSN"))
		if v == "" {
			return "", errors.New("unset")
		}
		return v, nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", errors.New("unreadable")
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", errors.New("empty")
	}
	return v, nil
}

func decodeCursor(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("too long")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var c Cursor
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	if c.ID == "" || len(c.ID) > 512 || c.CreatedAt.IsZero() {
		return nil, errors.New("invalid fields")
	}
	return &c, nil
}

func encodeCursor(c Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

var requiredColumns = map[string][]string{
	"broker_ingress_receipts":        {"id", "tenant_id", "device_id", "route_epoch", "state", "attempts", "created_at"},
	"broker_command_requests":        {"id", "tenant_id", "target_device_id", "route_epoch", "state", "created_at"},
	"broker_ingress_effects":         {"id", "receipt_id", "tenant_id", "state", "attempts", "created_at"},
	"broker_ingress_quarantine":      {"id", "tenant_id", "device_id", "route_epoch", "reason", "occurrences", "first_seen"},
	"broker_connection_observations": {"tenant_id", "device_id", "broker_cell", "broker_instance", "node_boot_id", "connection_id", "route_epoch", "event_seq", "online", "received_at"},
	"broker_automation_actions":      {"id", "effect_id", "tenant_id", "state", "created_at"},
}

func query(ctx context.Context, db *gorm.DB, tenant string, limit int, cursor *Cursor) (Output, error) {
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	if tx.Error != nil {
		return Output{}, sanitizeDBError(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("SET LOCAL statement_timeout = '5s'").Error; err != nil {
		return Output{}, sanitizeDBError(err)
	}
	if err := tx.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
		return Output{}, sanitizeDBError(err)
	}
	var readOnly string
	if err := tx.Raw("SHOW transaction_read_only").Scan(&readOnly).Error; err != nil {
		return Output{}, sanitizeDBError(err)
	}
	if readOnly != "on" {
		return Output{}, errors.New("database transaction is not read-only")
	}

	var missing int64
	for table, columns := range requiredColumns {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name IN ?`, table, columns).Scan(&found).Error; err != nil {
			return Output{}, sanitizeDBError(err)
		}
		if found != int64(len(columns)) {
			missing++
		}
	}
	if missing != 0 {
		return Output{}, errSchemaIncomplete
	}

	const sqlQuery = `
WITH audit_rows AS (
 SELECT 'ingress_receipt'::text AS kind, id::text AS raw_id, state::text AS status,
        attempts::bigint AS count, created_at, ''::text AS related_id, route_epoch::bigint AS route_epoch,
        ''::text AS reason, tenant_id
 FROM broker_ingress_receipts WHERE tenant_id = $1
 UNION ALL
 SELECT 'command'::text, id::text, state::text, 0::bigint, created_at, target_device_id::text, route_epoch::bigint, ''::text, tenant_id
 FROM broker_command_requests WHERE tenant_id = $1
 UNION ALL
 SELECT 'effect'::text, e.id::text, e.state::text, e.attempts::bigint, e.created_at, e.receipt_id::text,
        COALESCE(r.route_epoch, 0)::bigint, ''::text, e.tenant_id
 FROM broker_ingress_effects e LEFT JOIN broker_ingress_receipts r ON r.id=e.receipt_id
 WHERE e.tenant_id = $1
 UNION ALL
 SELECT 'quarantine'::text, id::text, 'rejected'::text, occurrences::bigint, first_seen, device_id::text,
        route_epoch::bigint, 'permanent_ingress_rejection'::text, tenant_id
 FROM broker_ingress_quarantine WHERE tenant_id = $1
 UNION ALL
 SELECT 'state_observation'::text,
        concat_ws(':', device_id, broker_cell, broker_instance, node_boot_id, connection_id, event_seq::text),
        CASE WHEN online THEN 'online' ELSE 'offline' END::text, event_seq::bigint, received_at, device_id::text,
        route_epoch::bigint, ''::text, tenant_id
 FROM broker_connection_observations WHERE tenant_id = $1
 UNION ALL
 SELECT 'automation_action'::text, a.id::text, a.state::text, 0::bigint, a.created_at, a.effect_id::text,
        COALESCE(r.route_epoch, 0)::bigint, ''::text, a.tenant_id
 FROM broker_automation_actions a
 LEFT JOIN broker_ingress_effects e ON e.id=a.effect_id
 LEFT JOIN broker_ingress_receipts r ON r.id=e.receipt_id
 WHERE a.tenant_id = $1
)
SELECT kind, kind || ':' || raw_id AS id, status, count, created_at, related_id, route_epoch, reason
FROM audit_rows
WHERE ($2::timestamptz IS NULL OR (created_at, kind || ':' || raw_id) < ($2::timestamptz, $3::text))
ORDER BY created_at DESC, id DESC
LIMIT $4`
	var rows []Record
	var cursorTime any
	var cursorID any
	if cursor != nil {
		cursorTime, cursorID = cursor.CreatedAt, cursor.ID
	}
	if err := tx.Raw(sqlQuery, tenant, cursorTime, cursorID, limit+1).Scan(&rows).Error; err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Output{}, context.DeadlineExceeded
		}
		return Output{}, sanitizeDBError(err)
	}
	if err := tx.Commit().Error; err != nil {
		return Output{}, sanitizeDBError(err)
	}
	result := Output{TenantID: tenant, Records: rows}
	if len(rows) > limit {
		last := rows[limit-1]
		result.Records = rows[:limit]
		result.NextCursor = encodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return result, nil
}

func sanitizeDBError(error) error { return errors.New("database operation failed") }
