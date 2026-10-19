// tp-broker-cutover applies a strictly reviewed plan through brokercontrol's
// existing route migration state machine. Database credentials authorize the
// operator; this CLI never manufactures runtime acknowledgements.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/brokercontract"
	"project/internal/brokercontrol"
)

const (
	commandTimeout = 5 * time.Second
	maxPlanBytes   = 16 << 10
	maxDSNBytes    = 8 << 10
)

type cutoverPlan struct {
	Version               string  `json:"version"`
	Operation             string  `json:"operation"`
	TenantID              string  `json:"tenant_id"`
	DeviceID              string  `json:"device_id"`
	ChangeID              string  `json:"change_id"`
	ExpectedCell          *string `json:"expected_cell,omitempty"`
	TargetCell            *string `json:"target_cell,omitempty"`
	ExpectedEpoch         *int64  `json:"expected_epoch,omitempty"`
	MembersSHA256         *string `json:"members_sha256,omitempty"`
	TargetReadySHA256     *string `json:"target_ready_sha256,omitempty"`
	ExpectedMembersSHA256 *string `json:"expected_members_sha256,omitempty"`
	NewMembersSHA256      *string `json:"new_members_sha256,omitempty"`
	ReasonSHA256          *string `json:"reason_sha256,omitempty"`
}

type commandOutput struct {
	Operation string `json:"operation"`
	TenantID  string `json:"tenant_id"`
	DeviceID  string `json:"device_id"`
	ChangeID  string `json:"change_id"`
	PlanHash  string `json:"plan_hash"`
	Result    string `json:"result"`
}

type membersOutput struct {
	Cell          string                      `json:"cell"`
	Members       []brokercontrol.RouteMember `json:"members"`
	MembersSHA256 string                      `json:"members_sha256"`
}

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr)) }

func run(parent context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || (args[0] != "members" && args[0] != "apply") {
		usage(stderr)
		if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
			return 0
		}
		return 64
	}
	action := args[0]
	fs := flag.NewFlagSet("tp-broker-cutover "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cell, planPath, suppliedHash, actor, dsnFile string
	fs.StringVar(&cell, "cell", "", "required broker cell (members only)")
	fs.StringVar(&planPath, "plan-file", "", "reviewed JSON plan file (apply only)")
	fs.StringVar(&suppliedHash, "sha256", "", "SHA-256 of the exact plan file bytes (apply only)")
	fs.StringVar(&actor, "actor", "", "required operator label (apply only)")
	fs.StringVar(&dsnFile, "dsn-file", "", "0600 regular file containing the database DSN")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 64
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid arguments: positional arguments are not accepted")
		return 64
	}
	var plan cutoverPlan
	var planHash string
	if action == "members" {
		if !validCell(cell) || planPath != "" || suppliedHash != "" || actor != "" {
			fmt.Fprintln(stderr, "invalid arguments: members requires only --cell")
			return 64
		}
	} else {
		if cell != "" || planPath == "" || suppliedHash == "" || !validActor(actor) {
			fmt.Fprintln(stderr, "invalid arguments: apply requires --plan-file, --sha256 and --actor")
			return 64
		}
		raw, err := readPlan(planPath)
		if err != nil {
			fmt.Fprintln(stderr, "invalid plan file: expected a regular JSON file no larger than 16 KiB")
			return 64
		}
		sum := sha256.Sum256(raw)
		planHash = hex.EncodeToString(sum[:])
		if !validHash(suppliedHash) || !strings.EqualFold(suppliedHash, planHash) || suppliedHash != strings.ToLower(suppliedHash) {
			fmt.Fprintln(stderr, "plan SHA-256 does not match the exact file bytes")
			return 64
		}
		parsed, err := parsePlan(raw)
		if err != nil {
			fmt.Fprintln(stderr, "invalid plan: unsupported version, fields or operation parameters")
			return 64
		}
		plan = parsed
	}
	dsn, err := readDSN(dsnFile, getenv)
	if err != nil {
		fmt.Fprintln(stderr, "database DSN unavailable: set TP_BROKER_CUTOVER_DSN or provide a protected --dsn-file")
		return 64
	}
	ctx, cancel := context.WithTimeout(parent, commandTimeout)
	defer cancel()
	db, closeDB, err := openDB(ctx, dsn)
	if err != nil {
		fmt.Fprintln(stderr, "database connection failed")
		return 69
	}
	defer closeDB()
	store := brokercontrol.NewMigrationStore(db)
	if action == "members" {
		members, digest, err := store.Members(ctx, cell)
		if err != nil {
			writeOperationError(stderr, ctx, err)
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(membersOutput{Cell: cell, Members: members, MembersSHA256: digest}); err != nil {
			fmt.Fprintln(stderr, "JSON output failed")
			return 1
		}
		return 0
	}
	if err := applyPlan(ctx, store, plan, planHash, actor); err != nil {
		writeOperationError(stderr, ctx, err)
		return 1
	}
	out := commandOutput{Operation: plan.Operation, TenantID: plan.TenantID, DeviceID: plan.DeviceID, ChangeID: plan.ChangeID, PlanHash: planHash, Result: "applied"}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		fmt.Fprintln(stderr, "JSON output failed")
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  tp-broker-cutover members --cell CELL [--dsn-file PATH]")
	fmt.Fprintln(w, "  tp-broker-cutover apply --plan-file PATH --sha256 HEX --actor OPERATOR [--dsn-file PATH]")
	fmt.Fprintln(w, "Apply runs one validated JSON plan through brokercontrol. It cannot create runtime ACKs or bypass a barrier.")
}

func readPlan(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxPlanBytes {
		return nil, errors.New("invalid plan file")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > maxPlanBytes || len(raw) == 0 {
		return nil, errors.New("invalid plan file")
	}
	return raw, nil
}

func parsePlan(raw []byte) (cutoverPlan, error) {
	var plan cutoverPlan
	if len(raw) == 0 || len(raw) > maxPlanBytes || brokercontract.ValidateUniqueJSON(raw) != nil {
		return plan, errors.New("invalid JSON")
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil || present == nil {
		return plan, errors.New("invalid plan object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return plan, errors.New("trailing JSON")
	}
	if plan.Version != "1" || !validOperation(plan.Operation) || !validIdentity(plan.TenantID, 36) || !validIdentity(plan.DeviceID, 36) {
		return plan, errors.New("invalid common plan fields")
	}
	allowed := map[string]bool{"version": true, "operation": true, "tenant_id": true, "device_id": true, "change_id": true}
	switch plan.Operation {
	case "freeze":
		for _, key := range []string{"expected_cell", "target_cell", "expected_epoch", "members_sha256"} {
			allowed[key] = true
		}
	case "prepare":
		allowed["target_ready_sha256"] = true
	case "rebase":
		for _, key := range []string{"expected_members_sha256", "new_members_sha256", "reason_sha256"} {
			allowed[key] = true
		}
	}
	for key := range present {
		if !allowed[key] {
			return plan, errors.New("irrelevant plan field")
		}
	}
	if _, err := uuid.Parse(plan.ChangeID); err != nil {
		return plan, errors.New("invalid change id")
	}
	switch plan.Operation {
	case "freeze":
		if plan.ExpectedCell == nil || plan.TargetCell == nil || !validCell(*plan.ExpectedCell) || !validCell(*plan.TargetCell) || *plan.ExpectedCell == *plan.TargetCell || plan.ExpectedEpoch == nil || *plan.ExpectedEpoch < 1 || plan.MembersSHA256 == nil || !validHash(*plan.MembersSHA256) || !onlyPlanFields(plan, "freeze") {
			return plan, errors.New("invalid freeze fields")
		}
	case "prepare":
		if plan.TargetReadySHA256 == nil || !validHash(*plan.TargetReadySHA256) || !onlyPlanFields(plan, "prepare") {
			return plan, errors.New("invalid prepare fields")
		}
	case "activate":
		if !onlyPlanFields(plan, "activate") {
			return plan, errors.New("invalid activate fields")
		}
	case "rebase":
		if plan.ExpectedMembersSHA256 == nil || !validHash(*plan.ExpectedMembersSHA256) || plan.NewMembersSHA256 == nil || !validHash(*plan.NewMembersSHA256) || plan.ReasonSHA256 == nil || !validHash(*plan.ReasonSHA256) || !onlyPlanFields(plan, "rebase") {
			return plan, errors.New("invalid rebase fields")
		}
	}
	return plan, nil
}

func onlyPlanFields(p cutoverPlan, operation string) bool {
	freeze := p.ExpectedCell != nil || p.TargetCell != nil || p.ExpectedEpoch != nil || p.MembersSHA256 != nil
	prepare := p.TargetReadySHA256 != nil
	rebase := p.ExpectedMembersSHA256 != nil || p.NewMembersSHA256 != nil || p.ReasonSHA256 != nil
	switch operation {
	case "freeze":
		return !prepare && !rebase
	case "prepare":
		return !freeze && !rebase
	case "activate":
		return !freeze && !prepare && !rebase
	case "rebase":
		return !freeze && !prepare
	default:
		return false
	}
}

func applyPlan(ctx context.Context, store *brokercontrol.MigrationStore, p cutoverPlan, rawPlanHash, actor string) error {
	if p.Operation != "freeze" {
		// Freeze already persists these references on the route change. Later
		// approved steps record their own reviewed file and actor atomically.
		reviewed, err := store.WithOperationAudit(actor, rawPlanHash, p.Operation)
		if err != nil {
			return err
		}
		store = reviewed
	}
	switch p.Operation {
	case "freeze":
		_, err := store.Freeze(ctx, brokercontrol.FreezeRouteRequest{ChangeID: p.ChangeID, TenantID: p.TenantID, DeviceID: p.DeviceID, ExpectedCell: *p.ExpectedCell, TargetCell: *p.TargetCell, ExpectedEpoch: *p.ExpectedEpoch, MembersSHA256: *p.MembersSHA256, PlanSHA256: rawPlanHash, Actor: actor})
		return err
	case "prepare":
		return store.PrepareTarget(ctx, p.TenantID, p.DeviceID, p.ChangeID, *p.TargetReadySHA256)
	case "activate":
		return store.Activate(ctx, p.TenantID, p.DeviceID, p.ChangeID)
	case "rebase":
		return store.RebaseBarrier(ctx, p.TenantID, p.DeviceID, p.ChangeID, *p.ExpectedMembersSHA256, *p.NewMembersSHA256, *p.ReasonSHA256)
	default:
		return errors.New("invalid operation")
	}
}

func openDB(ctx context.Context, dsn string) (*gorm.DB, func(), error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(1)
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		_ = sqlDB.Close()
		return nil, nil, err
	}
	if _, err := conn.ExecContext(ctx, "SET lock_timeout='2s'"); err != nil {
		_ = conn.Close()
		_ = sqlDB.Close()
		return nil, nil, err
	}
	if _, err := conn.ExecContext(ctx, "SET statement_timeout='2s'"); err != nil {
		_ = conn.Close()
		_ = sqlDB.Close()
		return nil, nil, err
	}
	// Pin the command session to this configured connection so the timeout
	// settings apply to the state machine's own transactions.
	session := db.Session(&gorm.Session{NewDB: true})
	session.Statement.ConnPool = sqlConnPool(conn)
	cleanup := func() { _ = conn.Close(); _ = sqlDB.Close() }
	return session, cleanup, nil
}

// sqlConnPool keeps the command's GORM transactions on the connection whose
// lock and statement timeouts were configured above.
func sqlConnPool(conn *sql.Conn) gorm.ConnPool { return conn }

func readDSN(file string, getenv func(string) string) (string, error) {
	if file == "" {
		value := strings.TrimSpace(getenv("TP_BROKER_CUTOVER_DSN"))
		if value == "" || len(value) > maxDSNBytes {
			return "", errors.New("unset or oversized")
		}
		return value, nil
	}
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxDSNBytes || info.Mode().Perm() != 0600 {
		return "", errors.New("DSN file must be regular, 0600 and at most 8 KiB")
	}
	valueBytes, err := os.ReadFile(file)
	if err != nil || len(valueBytes) == 0 || len(valueBytes) > maxDSNBytes {
		return "", errors.New("invalid DSN file")
	}
	value := strings.TrimSpace(string(valueBytes))
	if value == "" {
		return "", errors.New("empty DSN")
	}
	return value, nil
}

func writeOperationError(w io.Writer, ctx context.Context, err error) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		fmt.Fprintln(w, "route operation timed out after 5 seconds")
		return
	}
	fmt.Fprintln(w, "route operation rejected or failed; inspect the read-only route view and database schema")
}

func validOperation(s string) bool {
	return s == "freeze" || s == "prepare" || s == "activate" || s == "rebase"
}
func validCell(s string) bool {
	return s != "" && strings.TrimSpace(s) == s && len(s) <= 128
}
func validIdentity(s string, max int) bool {
	return s != "" && strings.TrimSpace(s) == s && len(s) <= max
}
func validActor(s string) bool { return validIdentity(s, 128) }
func validHash(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

var _ gorm.ConnPool = (*sql.Conn)(nil)
