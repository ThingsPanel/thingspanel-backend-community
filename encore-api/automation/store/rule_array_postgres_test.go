package store_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"encore.app/automation/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
)

// Manual/scheduled rules can serialize groups as JSON null. They must not
// break device filtering or prevent unrelated telemetry rules from running.
func TestDeviceRuleQueriesWithNonArrayTriggers(t *testing.T) {
	dsn := os.Getenv("AUTOMATION_RECEIPT_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated automation fixture not configured")
	}
	cfg, err := automationReceiptFixtureConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	schema := "rule_arrays_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE auto_rules (
 id text PRIMARY KEY, tenant_id text, name text, kind text, enabled boolean,
 revision int DEFAULT 1, definition jsonb, definition_hash text DEFAULT '',
 version int DEFAULT 1, legacy_id text, resource_key text, bundle_hash text,
 created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now(), deleted_at timestamptz);
 CREATE TABLE auto_bindings(rule_id text, enabled boolean, device_id text, device_config_id text);`)
	if err != nil {
		t.Fatal(err)
	}
	defs := []string{
		`{}`, `{"trigger":{"groups":null}}`, `{"trigger":{"groups":{}}}`,
		`{"trigger":{"groups":42}}`, `{"trigger":{"groups":"bad"}}`,
		`{"trigger":{"groups":[null,{}, {"conditions":null}, {"conditions":{}}, {"conditions":false}]}}`,
		`{"trigger":{"groups":null},"targets":{"device_ids":["device-a"]}}`,
		`{"trigger":{"groups":[{"conditions":[{"source":"device","device_id":"device-a","signal":"telemetry","key":"temperature"}]}]}}`,
	}
	for i, def := range defs {
		_, err = db.Exec(`INSERT INTO auto_rules(id,tenant_id,name,kind,enabled,definition) VALUES($1,'tenant-a',$1,'automation',true,$2::jsonb)`, string(rune('a'+i)), def)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Identical trigger in a different tenant must never be returned.
	_, err = db.Exec(`INSERT INTO auto_rules(id,tenant_id,name,kind,enabled,definition) VALUES('foreign','tenant-b','foreign','automation',true,$1::jsonb)`, defs[7])
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	ctx := context.Background()
	f := store.RuleFilter{DeviceID: "device-a", Limit: 10}
	rows, err := s.ListRules(ctx, "tenant-a", f)
	if err != nil || len(rows) != 2 {
		t.Fatalf("list len=%d err=%v", len(rows), err)
	}
	n, err := s.CountRules(ctx, "tenant-a", f)
	if err != nil || n != 2 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	rows, err = s.CandidateRules(ctx, "tenant-a", "device-a", "", "telemetry", "temperature")
	if err != nil || len(rows) != 1 || rows[0].ID != "h" {
		t.Fatalf("candidates=%v err=%v", rows, err)
	}
	rows, err = s.RulesMatchingDevice(ctx, "tenant-a", "device-a")
	if err != nil || len(rows) != 1 || rows[0].ID != "h" {
		t.Fatalf("matching=%v err=%v", rows, err)
	}
	f.DeviceID = "unrelated-device"
	n, err = s.CountRules(ctx, "tenant-a", f)
	if err != nil || n != 0 {
		t.Fatalf("unrelated count=%d err=%v", n, err)
	}
}
