package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRouteCLIValidationAndRedaction(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"list", "--tenant", "t", "--limit", "0"}, {"get", "--tenant", "t"}, {"members", "--tenant", "t", "--change-id", "not-a-uuid"}} {
		var out, stderr bytes.Buffer
		if code := run(context.Background(), args, func(string) string { return "" }, &out, &stderr); code != 64 || out.Len() != 0 {
			t.Errorf("args=%v code=%d stdout=%q stderr=%q", args, code, out.String(), stderr.String())
		}
	}
	const secret = "route-cli-secret-password"
	var out, stderr bytes.Buffer
	dsn := "postgres://audit:" + secret + "@127.0.0.1:1/never?connect_timeout=1"
	if code := run(context.Background(), []string{"list", "--tenant", "tenant-a"}, func(k string) string {
		if k == "TP_BROKER_ROUTE_DSN" {
			return dsn
		}
		return ""
	}, &out, &stderr); code != 69 {
		t.Fatalf("connection failure code=%d", code)
	}
	if strings.Contains(out.String()+stderr.String(), secret) || strings.Contains(out.String()+stderr.String(), dsn) {
		t.Fatalf("database credential leaked: %q %q", out.String(), stderr.String())
	}
	if _, err := decodeCursor(encodeCursor(Cursor{CreatedAt: time.Now().UTC(), ID: "route-id"})); err != nil {
		t.Fatalf("cursor round trip: %v", err)
	}
}

func TestRouteCLIPostgresTenantScopeReadOnlyAndPaging(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_BROKER_DATABASE_DSN"))
	if dsn == "" {
		b, e := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
		if e != nil {
			t.Skip("isolated broker QA PostgreSQL DSN is unavailable")
		}
		dsn = strings.TrimSpace(string(b))
	}
	if dsn == "" {
		t.Skip("isolated broker QA PostgreSQL DSN is empty")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated QA database")
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal("get QA pool")
	}
	adminSQL.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = adminSQL.Close() })
	var dbName string
	if err := admin.Raw("SELECT current_database()").Scan(&dbName).Error; err != nil {
		t.Fatal("inspect database")
	}
	if dbName != "brokerqa" {
		t.Fatal("refusing route CLI test outside brokerqa")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "brokerqa_route_cli_" + hex.EncodeToString(random[:])
	if err := admin.Exec("CREATE SCHEMA \"" + schema + "\"").Error; err != nil {
		t.Fatal("create private schema")
	}
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA \"" + schema + "\" CASCADE").Error })
	testDSN, err := testSearchPath(dsn, schema)
	if err != nil {
		t.Fatal("configure private search path")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated schema")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("get schema pool")
	}
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })
	paths, err := filepath.Glob(filepath.Join("..", "..", "sql", "broker", "[0-9][0-9][0-9]_*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) < 12 || !strings.HasSuffix(paths[11], "012_route_changes.sql") {
		t.Fatalf("broker migrations not present through 012: %d", len(paths))
	}
	for _, path := range paths {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal("read migration")
		}
		if e = db.Exec(strings.ReplaceAll(string(raw), "public.", `"`+schema+`".`)).Error; e != nil {
			t.Fatalf("apply %s: %v", filepath.Base(path), e)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	changeA, changeB := uuid.NewString(), uuid.NewString()
	for _, row := range []struct {
		id, tenant, device, source, target, phase string
		epoch                                     int64
	}{{changeA, "tenant-a", "device-a", "cell-a", "cell-b", "frozen", 4}, {changeB, "tenant-b", "device-b", "cell-x", "cell-y", "active", 8}} {
		if err := db.Exec(`INSERT INTO broker_route_changes(id,tenant_id,device_id,source_cell,target_cell,source_epoch,actor,members_sha256,plan_sha256,phase,created_at) VALUES(?::uuid,?,?,?,?,?,'operator',repeat('a',64),repeat('b',64),?,?)`, row.id, row.tenant, row.device, row.source, row.target, row.epoch, row.phase, now).Error; err != nil {
			t.Fatal("seed route change")
		}
		if err := db.Exec(`INSERT INTO broker_route_barrier_members(change_id,role,instance_id,boot_id,generation,acknowledged_at,evidence_sha256) VALUES(?::uuid,'broker','node-a','boot-a',?::uuid,now(),repeat('c',64))`, row.id, uuid.NewString()).Error; err != nil {
			t.Fatal("seed barrier member")
		}
		for i := 0; i < 2; i++ {
			if err := db.Exec(`INSERT INTO broker_route_change_events(change_id,stage,evidence,created_at) VALUES(?::uuid,?,?::jsonb,?)`, row.id, "stage-"+string(rune('1'+i)), `{"detail":"untrusted-secret"}`, now.Add(time.Duration(i)*time.Second)).Error; err != nil {
				t.Fatal("seed route event")
			}
		}
	}
	if err := db.Exec(`INSERT INTO broker_route_barrier_members(change_id,role,instance_id,boot_id,generation) VALUES(?::uuid,'adapter','node-b','boot-b',?::uuid)`, changeA, uuid.NewString()).Error; err != nil {
		t.Fatal("seed second barrier member")
	}
	getenv := func(k string) string {
		if k == "TP_BROKER_ROUTE_DSN" {
			return testDSN
		}
		return ""
	}
	if _, err := query(context.Background(), db, "list", opts{tenant: "tenant-a", limit: 1}, nil); err != nil {
		t.Fatalf("direct route query: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"list", "--tenant", "tenant-a", "--limit", "1"}, getenv, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list code=%d stderr=%q", code, stderr.String())
	}
	var first Output
	if err := json.Unmarshal(stdout.Bytes(), &first); err != nil {
		t.Fatal("decode first page")
	}
	if len(first.Changes) != 1 || first.Changes[0].TenantID != "tenant-a" || first.Changes[0].ID != changeA || first.NextCursor != "" {
		t.Fatalf("unexpected scoped page: %#v", first)
	}
	// A second same-tenant record proves stable cursor behavior without ever
	// exposing tenant-b's independently seeded route change.
	secondID := uuid.NewString()
	if err := db.Exec(`INSERT INTO broker_route_changes(id,tenant_id,device_id,source_cell,target_cell,source_epoch,actor,members_sha256,plan_sha256,phase,created_at) VALUES(?::uuid,'tenant-a','device-a2','cell-a','cell-b',4,'operator',repeat('d',64),repeat('e',64),'active',?)`, secondID, now.Add(time.Second)).Error; err != nil {
		t.Fatal("seed second scoped record")
	}
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"list", "--tenant", "tenant-a", "--limit", "1"}, getenv, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("paged list code=%d stderr=%q", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Changes) != 1 || first.NextCursor == "" {
		t.Fatalf("expected next cursor: %#v", first)
	}
	cur, err := decodeCursor(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"list", "--tenant", "tenant-a", "--limit", "1", "--cursor", first.NextCursor}, getenv, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("next page code=%d stderr=%q", code, stderr.String())
	}
	var second Output
	if err := json.Unmarshal(stdout.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Changes) != 1 || second.Changes[0].TenantID != "tenant-a" || second.Changes[0].ID == first.Changes[0].ID || second.NextCursor != "" {
		t.Fatalf("bad second page (cursor %v): %#v", cur, second)
	}
	for _, args := range [][]string{{"get", "--tenant", "tenant-b", "--change-id", changeA}, {"members", "--tenant", "tenant-b", "--change-id", changeA}, {"events", "--tenant", "tenant-b", "--change-id", changeA}} {
		stdout.Reset()
		stderr.Reset()
		if code := run(context.Background(), args, getenv, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
			t.Fatalf("cross-tenant command %v code=%d out=%q", args, code, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"members", "--tenant", "tenant-a", "--change-id", changeA, "--limit", "1"}, getenv, &stdout, &stderr); code != 0 {
		t.Fatalf("members code=%d stderr=%q", code, stderr.String())
	}
	var members Output
	if err := json.Unmarshal(stdout.Bytes(), &members); err != nil {
		t.Fatal(err)
	}
	if len(members.Members) != 1 || members.NextCursor == "" {
		t.Fatalf("member summary=%#v", members.Members)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"members", "--tenant", "tenant-a", "--change-id", changeA, "--limit", "1", "--cursor", members.NextCursor}, getenv, &stdout, &stderr); code != 0 {
		t.Fatalf("second members page code=%d stderr=%q", code, stderr.String())
	}
	var nextMembers Output
	if err := json.Unmarshal(stdout.Bytes(), &nextMembers); err != nil {
		t.Fatal(err)
	}
	if len(nextMembers.Members) != 1 || nextMembers.NextCursor != "" || !nextMembers.Members[0].Acknowledged {
		t.Fatalf("second member page=%#v", nextMembers)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"events", "--tenant", "tenant-a", "--change-id", changeA, "--limit", "1"}, getenv, &stdout, &stderr); code != 0 {
		t.Fatalf("events code=%d stderr=%q", code, stderr.String())
	}
	var events Output
	if err := json.Unmarshal(stdout.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 1 || events.NextCursor == "" {
		t.Fatalf("event page=%#v", events)
	}
	if strings.Contains(stdout.String()+stderr.String(), "untrusted-secret") {
		t.Fatal("route event evidence leaked to CLI")
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"events", "--tenant", "tenant-a", "--change-id", changeA, "--limit", "1", "--cursor", events.NextCursor}, getenv, &stdout, &stderr); code != 0 {
		t.Fatalf("second event page code=%d stderr=%q", code, stderr.String())
	}
	var previousEvents Output
	if err := json.Unmarshal(stdout.Bytes(), &previousEvents); err != nil {
		t.Fatal(err)
	}
	if len(previousEvents.Events) != 1 || previousEvents.NextCursor != "" {
		t.Fatalf("second event page=%#v", previousEvents)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		var ro string
		if e := tx.Raw("SHOW transaction_read_only").Scan(&ro).Error; e != nil {
			return e
		}
		if ro != "on" {
			return fmt.Errorf("read-only mode is %q", ro)
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true}); err != nil {
		t.Fatalf("verify read-only transaction: %v", err)
	}
}

func testSearchPath(dsn, schema string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			return "", e
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return dsn + " search_path=" + schema, nil
}
