package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

func TestPostgresAuditReadOnlyTenantPaginationAndRedaction(t *testing.T) {
	dsn := auditTestDSN(t)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open QA PostgreSQL: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	schema := "broker_audit_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + schema + `"`
	if err := admin.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		t.Fatalf("create private schema: %v", err)
	}
	var dbSQL *sql.DB
	t.Cleanup(func() {
		if dbSQL != nil {
			_ = dbSQL.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE").Error
		_ = adminSQL.Close()
	})
	isolated, err := gorm.Open(postgres.Open(auditDSNWithSchema(dsn, schema)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		t.Fatal("open private-schema DB")
	}
	dbSQL, err = isolated.DB()
	if err != nil {
		t.Fatal(err)
	}
	dbSQL.SetMaxOpenConns(4)
	for _, path := range brokerMigrations(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		migration := strings.ReplaceAll(string(data), "public.", quoted+".")
		if err := isolated.Exec(migration).Error; err != nil {
			t.Fatalf("apply isolated migration %s: %v", filepath.Base(path), err)
		}
	}
	seedAuditRows(t, isolated)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cursor := (*Cursor)(nil)
	seen := make(map[string]bool)
	var rawAll strings.Builder
	for pageNo := 0; pageNo < 10; pageNo++ {
		args := []string{"--tenant", "tenant-a", "--limit", "2"}
		if cursor != nil {
			args = append(args, "--cursor", encodeCursor(*cursor))
		}
		var cliOut, cliErr strings.Builder
		code := run(ctx, args, func(key string) string {
			if key == "TP_BROKER_AUDIT_DSN" {
				return auditDSNWithSchema(dsn, schema)
			}
			return ""
		}, &cliOut, &cliErr)
		if code != 0 {
			t.Fatalf("audit CLI page %d failed code=%d stderr=%q", pageNo, code, cliErr.String())
		}
		var page Output
		if err := json.Unmarshal([]byte(cliOut.String()), &page); err != nil {
			t.Fatalf("decode CLI output page %d: %v", pageNo, err)
		}
		if page.TenantID != "tenant-a" {
			t.Fatalf("tenant scope leaked: %#v", page)
		}
		b := []byte(cliOut.String())
		rawAll.Write(b)
		for _, row := range page.Records {
			if seen[row.ID] {
				t.Fatalf("duplicate row across pages: %s", row.ID)
			}
			seen[row.ID] = true
			if row.ID == "receipt:tenant-b-row" {
				t.Fatal("cross-tenant row leaked")
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor, err = decodeCursor(page.NextCursor)
		if err != nil {
			t.Fatalf("decode returned cursor: %v", err)
		}
	}
	for _, kind := range []string{"ingress_receipt", "command", "effect", "quarantine", "state_observation", "automation_action"} {
		found := false
		for id := range seen {
			if strings.HasPrefix(id, kind+":") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing kind %s in paged audit results: %v", kind, seen)
		}
	}
	for _, secret := range []string{"private-payload-secret", "voucher-secret", "command-param-secret", "raw-quarantine-body-secret", "unsafe reason includes secret", "owner-token-secret"} {
		if strings.Contains(rawAll.String(), secret) {
			t.Errorf("audit output leaked %q", secret)
		}
	}

	var countBefore int64
	if err := isolated.Raw("SELECT count(*) FROM broker_ingress_receipts WHERE tenant_id='tenant-a'").Scan(&countBefore).Error; err != nil {
		t.Fatal(err)
	}
	ro := isolated.Begin(&sql.TxOptions{ReadOnly: true})
	if ro.Error != nil {
		t.Fatal(ro.Error)
	}
	writeErr := ro.Exec("UPDATE broker_ingress_receipts SET attempts=attempts+1 WHERE tenant_id='tenant-a'").Error
	_ = ro.Rollback().Error
	if writeErr == nil {
		t.Fatal("PostgreSQL accepted a write in a read-only transaction")
	}
	var countAfter int64
	if err := isolated.Raw("SELECT count(*) FROM broker_ingress_receipts WHERE tenant_id='tenant-a'").Scan(&countAfter).Error; err != nil {
		t.Fatal(err)
	}
	if countAfter != countBefore {
		t.Fatalf("audit changed data: before=%d after=%d", countBefore, countAfter)
	}

	missingSchema := "broker_audit_missing_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	missingQuoted := `"` + missingSchema + `"`
	if err := admin.Exec("CREATE SCHEMA " + missingQuoted).Error; err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + missingQuoted + " CASCADE")
	missingDB, err := gorm.Open(postgres.Open(auditDSNWithSchema(dsn, missingSchema)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		t.Fatal("open missing-schema fixture")
	}
	missingSQL, err := missingDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer missingSQL.Close()
	if _, err := query(ctx, missingDB, "tenant-a", 10, nil); !errors.Is(err, errSchemaIncomplete) {
		t.Fatalf("missing schema error=%v, want fixed schema error", err)
	}
	var stdout, stderr strings.Builder
	code := run(context.Background(), []string{"--tenant", "tenant-a"}, func(key string) string {
		if key == "TP_BROKER_AUDIT_DSN" {
			return auditDSNWithSchema(dsn, missingSchema)
		}
		return ""
	}, &stdout, &stderr)
	if code != 78 || stderr.String() != "broker audit schema is incomplete\n" {
		t.Fatalf("missing schema response code=%d stderr=%q", code, stderr.String())
	}
}

func auditTestDSN(t *testing.T) string {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv("TP_BROKER_AUDIT_TEST_DSN")); dsn != "" {
		return dsn
	}
	b, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
	if err != nil {
		t.Skip("PostgreSQL QA DSN unavailable")
	}
	dsn := strings.TrimSpace(string(b))
	if dsn == "" {
		t.Skip("PostgreSQL QA DSN empty")
	}
	return dsn
}

func auditDSNWithSchema(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err == nil {
			q := u.Query()
			q.Set("search_path", schema)
			u.RawQuery = q.Encode()
			return u.String()
		}
	}
	return dsn + " search_path=" + schema
}

func brokerMigrations(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "sql", "broker", "[0-9][0-9][0-9]_*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no broker migrations found")
	}
	return paths
}

func seedAuditRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	const tenant = "tenant-a"
	const timestamp = "2026-10-20T01:02:03Z"
	if err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES (?, ?, 'cell-a', 7, 'active')`, tenant, "device-a").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state,attempts,created_at) VALUES ('00000000-0000-4000-8000-000000000001', ?, 'device-a', 'event', 'cell-a', 7, ?, ?::jsonb, decode(repeat('ab',32),'hex'), 'pending', 3, ?::timestamptz)`, tenant, []byte("private-payload-secret"), `{"voucher":"voucher-secret"}`, timestamp).Error; err != nil {
		t.Fatalf("seed receipt: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state,attempts,created_at) VALUES ('00000000-0000-4000-8000-000000000002', 'tenant-b', 'device-b', 'event', 'cell-b', 1, '\\x01', '{}'::jsonb, decode(repeat('cd',32),'hex'), 'processed', 0, ?::timestamptz)`, timestamp).Error; err != nil {
		t.Fatalf("seed foreign receipt: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_command_requests(id,tenant_id,caller_id,intent,idempotency_key,device_id,target_device_id,route_cell,route_epoch,canonical_request,canonical_sha256,payload,expires_at,state,created_at) VALUES ('00000000-0000-4000-8000-000000000011', ?, 'caller-a', 'manual', 'key-a', 'device-a', 'device-a', 'cell-a', 7, ?, decode(repeat('ef',32),'hex'), ?, now()+interval '1 minute', 'accepted', ?::timestamptz)`, tenant, []byte("command-param-secret"), []byte("owner-token-secret"), timestamp).Error; err != nil {
		t.Fatalf("seed command: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_ingress_effects(id,receipt_id,tenant_id,device_id,effect_kind,payload,state,attempts,created_at) VALUES ('00000000-0000-4000-8000-000000000021','00000000-0000-4000-8000-000000000001',?,'device-a','automation','{}'::jsonb,'pending',2,?::timestamptz)`, tenant, timestamp).Error; err != nil {
		t.Fatalf("seed effect: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_ingress_quarantine(id,tenant_id,device_id,broker_cell,route_epoch,topic,body,reason,occurrences,first_seen,last_seen,expires_at) VALUES ('q-a',?,'device-a','cell-a',7,'secret/topic',?,'unsafe reason includes secret',4,?::timestamptz,?::timestamptz,now()+interval '1 day')`, tenant, []byte("raw-quarantine-body-secret"), timestamp, timestamp).Error; err != nil {
		t.Fatalf("seed quarantine: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES (?,'device-a','cell-a','instance-a','boot-a','conn-a',7,9,true,?::timestamptz,?::timestamptz)`, tenant, timestamp, timestamp).Error; err != nil {
		t.Fatalf("seed state: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_automation_actions(id,effect_id,tenant_id,scene_id,action_id,target_device_id,policy_sha256,state,detail,created_at) VALUES ('00000000-0000-4000-8000-000000000031','00000000-0000-4000-8000-000000000021',?,'scene-a','action-a','device-a',repeat('a',64),'accepted','do not expose',?::timestamptz)`, tenant, timestamp).Error; err != nil {
		t.Fatalf("seed action: %v", err)
	}
}
