// tp-broker-route is a read-only operator view of the SQL012 route state
// machine. It deliberately has no command that can create ACKs or transitions.
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
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const timeout = 5 * time.Second

var errSchemaIncomplete = errors.New("broker route schema is incomplete")

type Cursor struct {
	CreatedAt time.Time `json:"created_at,omitempty"`
	ID        string    `json:"id"`
}
type Change struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenant_id"`
	DeviceID          string     `json:"device_id"`
	SourceCell        string     `json:"source_cell"`
	TargetCell        string     `json:"target_cell"`
	SourceEpoch       int64      `json:"source_epoch"`
	Actor             string     `json:"actor"`
	Phase             string     `json:"phase"`
	CreatedAt         time.Time  `json:"created_at"`
	PreparedAt        *time.Time `json:"prepared_at,omitempty"`
	ActivatedAt       *time.Time `json:"activated_at,omitempty"`
	PlanSHA256        string     `json:"plan_sha256"`
	MembersSHA256     string     `json:"members_sha256"`
	TargetReadySHA256 *string    `json:"target_ready_sha256,omitempty"`
}

func (Change) TableName() string { return "broker_route_changes" }

type Member struct {
	Role           string     `json:"role"`
	InstanceID     string     `json:"instance_id"`
	BootID         string     `json:"boot_id"`
	Generation     string     `json:"generation"`
	Acknowledged   bool       `json:"acknowledged"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
}
type Event struct {
	Sequence  int64     `json:"sequence"`
	Stage     string    `json:"stage"`
	CreatedAt time.Time `json:"created_at"`
}
type Output struct {
	TenantID   string   `json:"tenant_id"`
	Change     *Change  `json:"change,omitempty"`
	Changes    []Change `json:"changes,omitempty"`
	Members    []Member `json:"members,omitempty"`
	Events     []Event  `json:"events,omitempty"`
	NextCursor string   `json:"next_cursor,omitempty"`
}
type opts struct {
	tenant, id, cursor, dsnFile string
	limit                       int
}

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr)) }

func run(parent context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "list" && args[0] != "get" && args[0] != "members" && args[0] != "events") {
		usage(stderr)
		return 64
	}
	action := args[0]
	fs := flag.NewFlagSet("tp-broker-route "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o opts
	fs.StringVar(&o.tenant, "tenant", "", "required tenant ID scope")
	fs.StringVar(&o.id, "change-id", "", "route change UUID (required for get/members/events)")
	fs.IntVar(&o.limit, "limit", 50, "page size (1..100)")
	fs.StringVar(&o.cursor, "cursor", "", "opaque cursor from the previous page")
	fs.StringVar(&o.dsnFile, "dsn-file", "", "read database DSN from a file instead of TP_BROKER_ROUTE_DSN")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 64
	}
	if fs.NArg() != 0 || strings.TrimSpace(o.tenant) == "" || len(o.tenant) > 256 {
		fmt.Fprintln(stderr, "invalid arguments: --tenant is required and positional arguments are not accepted")
		return 64
	}
	if o.limit < 1 || o.limit > 100 {
		fmt.Fprintln(stderr, "invalid --limit: expected an integer from 1 to 100")
		return 64
	}
	if action != "list" {
		if _, err := uuid.Parse(o.id); err != nil {
			fmt.Fprintln(stderr, "invalid arguments: --change-id must be a UUID")
			return 64
		}
	}
	cursor, err := decodeCursor(o.cursor)
	if err != nil {
		fmt.Fprintln(stderr, "invalid --cursor: expected a cursor returned by this command")
		return 64
	}
	if action == "get" && cursor != nil {
		fmt.Fprintln(stderr, "invalid arguments: get does not accept --cursor")
		return 64
	}
	if cursor != nil {
		switch action {
		case "list":
			if cursor.CreatedAt.IsZero() {
				fmt.Fprintln(stderr, "invalid --cursor: list cursor is incomplete")
				return 64
			}
			if _, err := uuid.Parse(cursor.ID); err != nil {
				fmt.Fprintln(stderr, "invalid --cursor: list cursor is malformed")
				return 64
			}
		case "events":
			if _, err := cursorSequence(cursor); err != nil {
				fmt.Fprintln(stderr, "invalid --cursor: events cursor is malformed")
				return 64
			}
		case "members":
			if !strings.Contains(cursor.ID, "\n") {
				fmt.Fprintln(stderr, "invalid --cursor: members cursor is malformed")
				return 64
			}
		}
	}
	dsn, err := readDSN(o.dsnFile, getenv)
	if err != nil {
		fmt.Fprintln(stderr, "database DSN unavailable: set TP_BROKER_ROUTE_DSN or provide a readable --dsn-file")
		return 64
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
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
	out, err := query(ctx, db, action, o, cursor)
	if err != nil {
		switch {
		case errors.Is(err, errSchemaIncomplete):
			fmt.Fprintln(stderr, "broker route schema is incomplete")
			return 78
		case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
			fmt.Fprintln(stderr, "broker route query timed out after 5 seconds")
			return 75
		default:
			fmt.Fprintln(stderr, "broker route query failed")
			return 1
		}
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(stderr, "JSON output failed")
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: tp-broker-route {list|get|members|events} --tenant TENANT_ID [--change-id UUID] [--limit 1..100] [--cursor BASE64URL] [--dsn-file PATH]")
	fmt.Fprintln(w, "Read-only route changes, per-change frozen member barrier snapshots, and event summaries. No transition or ACK writes are available.")
}
func readDSN(file string, getenv func(string) string) (string, error) {
	if file == "" {
		v := strings.TrimSpace(getenv("TP_BROKER_ROUTE_DSN"))
		if v == "" {
			return "", errors.New("unset")
		}
		return v, nil
	}
	b, e := os.ReadFile(file)
	if e != nil {
		return "", errors.New("unreadable")
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", errors.New("empty")
	}
	return v, nil
}
func encodeCursor(c Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeCursor(s string) (*Cursor, error) {
	if s == "" {
		return nil, nil
	}
	if len(s) > 2048 {
		return nil, errors.New("too long")
	}
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil {
		return nil, e
	}
	var c Cursor
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return nil, e
	}
	if c.ID == "" || len(c.ID) > 512 {
		return nil, errors.New("bad fields")
	}
	return &c, nil
}

var required = map[string][]string{
	"broker_route_changes":         {"id", "tenant_id", "device_id", "source_cell", "target_cell", "source_epoch", "actor", "members_sha256", "plan_sha256", "phase", "created_at", "prepared_at", "activated_at", "target_ready_sha256"},
	"broker_route_barrier_members": {"change_id", "role", "instance_id", "boot_id", "generation", "acknowledged_at", "evidence_sha256"},
	"broker_route_change_events":   {"sequence", "change_id", "stage", "evidence", "created_at"},
}

func query(ctx context.Context, db *gorm.DB, action string, o opts, cursor *Cursor) (Output, error) {
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	if tx.Error != nil {
		return Output{}, safeErr(tx.Error)
	}
	defer tx.Rollback()
	if e := tx.Exec("SET LOCAL statement_timeout='5s'").Error; e != nil {
		return Output{}, safeErr(e)
	}
	if e := tx.Exec("SET LOCAL lock_timeout='5s'").Error; e != nil {
		return Output{}, safeErr(e)
	}
	var ro string
	if e := tx.Raw("SHOW transaction_read_only").Scan(&ro).Error; e != nil {
		return Output{}, safeErr(e)
	}
	if ro != "on" {
		return Output{}, errors.New("transaction is not read-only")
	}
	for table, cols := range required {
		var n int64
		if e := tx.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=? AND column_name IN ?`, table, cols).Scan(&n).Error; e != nil {
			return Output{}, safeErr(e)
		}
		if n != int64(len(cols)) {
			return Output{}, errSchemaIncomplete
		}
	}
	out := Output{TenantID: o.tenant}
	switch action {
	case "list":
		var rows []Change
		q := tx.Where("tenant_id=?", o.tenant)
		if cursor != nil {
			q = q.Where("(created_at,id::text)<(?,?)", cursor.CreatedAt, cursor.ID)
		}
		if e := q.Order("created_at DESC,id DESC").Limit(o.limit + 1).Find(&rows).Error; e != nil {
			return Output{}, safeErr(e)
		}
		if len(rows) > o.limit {
			rows = rows[:o.limit]
			last := rows[len(rows)-1]
			out.NextCursor = encodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		}
		out.Changes = rows
	case "get":
		var row Change
		if e := tx.Where("tenant_id=? AND id=?::uuid", o.tenant, o.id).Take(&row).Error; e != nil {
			return Output{}, safeErr(e)
		}
		out.Change = &row
	case "members":
		if e := scopedChange(tx, o.tenant, o.id); e != nil {
			return Output{}, e
		}
		var role, instance string
		if cursor != nil {
			parts := strings.SplitN(cursor.ID, "\n", 2)
			role, instance = parts[0], parts[1]
		}
		var rows []Member
		if e := tx.Raw(`SELECT role,instance_id,boot_id,generation,(acknowledged_at IS NOT NULL) AS acknowledged,acknowledged_at FROM broker_route_barrier_members WHERE change_id=?::uuid AND (?='' OR (role,instance_id)>(?,?)) ORDER BY role,instance_id LIMIT ?`, o.id, role, role, instance, o.limit+1).Scan(&rows).Error; e != nil {
			return Output{}, safeErr(e)
		}
		if len(rows) > o.limit {
			rows = rows[:o.limit]
			last := rows[len(rows)-1]
			out.NextCursor = encodeCursor(Cursor{ID: last.Role + "\n" + last.InstanceID})
		}
		out.Members = rows
	case "events":
		if e := scopedChange(tx, o.tenant, o.id); e != nil {
			return Output{}, e
		}
		var rows []Event
		sequence, _ := cursorSequence(cursor)
		q := tx.Raw(`SELECT sequence,stage,created_at FROM broker_route_change_events WHERE change_id=?::uuid AND (?::bigint IS NULL OR sequence<?::bigint) ORDER BY sequence DESC LIMIT ?`, o.id, sequence, sequence, o.limit+1)
		if e := q.Scan(&rows).Error; e != nil {
			return Output{}, safeErr(e)
		}
		if len(rows) > o.limit {
			rows = rows[:o.limit]
			out.NextCursor = encodeCursor(Cursor{ID: fmt.Sprint(rows[len(rows)-1].Sequence)})
		}
		out.Events = rows
	default:
		return Output{}, errors.New("unsupported operation")
	}
	return out, nil
}
func scopedChange(tx *gorm.DB, tenant, id string) error {
	var n int64
	if e := tx.Model(&Change{}).Where("tenant_id=? AND id=?::uuid", tenant, id).Count(&n).Error; e != nil {
		return safeErr(e)
	}
	if n != 1 {
		return errors.New("route change not found")
	}
	return nil
}
func cursorSequence(c *Cursor) (any, error) {
	if c == nil {
		return nil, nil
	}
	n, e := strconv.ParseInt(c.ID, 10, 64)
	if e != nil || n < 1 {
		return nil, errors.New("invalid event cursor")
	}
	return n, nil
}
func safeErr(err error) error { return fmt.Errorf("route query failed: %w", err) }
