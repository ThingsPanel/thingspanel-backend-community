package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"encore.app/automation/domain"
	"encore.app/pkg/sqldialect"
)

// contractDatabases builds the automation schema on a real PostgreSQL (from the original migrations) and on a
// real MySQL (from the generated ones) and returns a store for each.
func contractDatabases(t *testing.T) (pg, my *Store) {
	t.Helper()
	pgDSN, myDSN := os.Getenv("TP_AUTO_SCHEMA_PG_DSN"), os.Getenv("TP_AUTO_SCHEMA_MYSQL_DSN")
	if pgDSN == "" || myDSN == "" {
		t.Skip("TP_AUTO_SCHEMA_PG_DSN and TP_AUTO_SCHEMA_MYSQL_DSN required")
	}
	config, err := pgx.ParseConfig(pgDSN)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*config)
	schema := "auto_rules_" + strconv.Itoa(os.Getpid())
	_, _ = admin.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`)
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	config.RuntimeParams["search_path"] = schema
	pgDB := stdlib.OpenDB(*config)
	pgDB.SetMaxOpenConns(8)
	t.Cleanup(func() { pgDB.Close() })
	files, _ := filepath.Glob(filepath.Join("..", "migrations", "*.up.sql"))
	sort.Slice(files, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.SplitN(filepath.Base(files[i]), "_", 2)[0])
		b, _ := strconv.Atoi(strings.SplitN(filepath.Base(files[j]), "_", 2)[0])
		return a < b
	})
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		if _, err := pgDB.Exec(string(raw)); err != nil {
			t.Fatalf("PostgreSQL migration %s: %v", f, err)
		}
	}

	myDB, err := sqldialect.OpenMySQL(myDSN)
	if err != nil {
		t.Fatal(err)
	}
	myDB.SetMaxOpenConns(8)
	conn, err := myDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 0")
	rows, _ := conn.QueryContext(context.Background(), "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()")
	var existing []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		existing = append(existing, n)
	}
	rows.Close()
	for _, n := range existing {
		_, _ = conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+n)
	}
	_, _ = conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 1")
	for _, name := range []string{"010_automation.sql", "011_automation_partial_unique.sql"} {
		for i, statement := range statementsOf(t, filepath.Join("..", "..", "..", "sql", "mysql", name)) {
			if _, err := conn.ExecContext(context.Background(), statement); err != nil {
				t.Fatalf("MySQL %s statement %d: %v", name, i+1, err)
			}
		}
	}
	conn.Close()
	t.Cleanup(func() { myDB.Close() })
	return New(pgDB), NewMySQL(myDB)
}

func kind(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrNotFound):
		return "not-found"
	case errors.Is(err, ErrConflict):
		return "conflict"
	}
	return "error"
}

func rule(id, tenant, name string, k domain.RuleKind, mutate func(*domain.Definition)) *Rule {
	// Real rules always carry a group list; "groups": null would make the PostgreSQL device filter fail.
	def := domain.Definition{Kind: k, Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{}}}}}
	if mutate != nil {
		mutate(&def)
	}
	return &Rule{ID: id, TenantID: tenant, Name: name, Kind: k, Definition: def, DefinitionHash: "h-" + id}
}

func TestRuleStoreBehavesTheSameOnMySQLAsOnPostgres(t *testing.T) {
	pg, my := contractDatabases(t)
	ctx := context.Background()
	run := func(s *Store) []string {
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		raw := func(v any) string { b, _ := json.Marshal(v); return string(b) }

		// Sequence numbers: strictly increasing, unique under concurrency.
		first, err := s.NextSeq(ctx)
		second, err2 := s.NextSeq(ctx)
		note("NextSeq increases: %v %v", err == nil && err2 == nil, second > first)
		var wg sync.WaitGroup
		seen := make(chan int64, 20)
		for i := 0; i < cap(seen); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v, err := s.NextSeq(ctx)
				if err != nil {
					t.Error(err)
				}
				seen <- v
			}()
		}
		wg.Wait()
		close(seen)
		unique := map[int64]bool{}
		for v := range seen {
			unique[v] = true
		}
		note("20 concurrent NextSeq are distinct: %v", len(unique) == 20)

		// Idempotent request records.
		_, found, err := s.LookupRequest(ctx, "t1", "k1")
		note("lookup before save: %s found=%v", kind(err), found)
		note("save: %s", kind(s.SaveRequest(ctx, "t1", "k1", "create", []byte(`{"id":"r1"}`))))
		note("save again with another response: %s", kind(s.SaveRequest(ctx, "t1", "k1", "create", []byte(`{"id":"other"}`))))
		resp, found, err := s.LookupRequest(ctx, "t1", "k1")
		var parsed map[string]any
		_ = json.Unmarshal(resp, &parsed)
		note("lookup keeps the first response: %s found=%v id=%v", kind(err), found, parsed["id"])
		_, found, _ = s.LookupRequest(ctx, "t2", "k1")
		note("another tenant does not see it: found=%v", found)
		n, err := s.PruneRequests(ctx, 24)
		note("prune keeps fresh rows: %s removed=%d", kind(err), n)
		if _, err := s.DB.ExecContext(ctx, `UPDATE auto_requests SET created_at = now() - interval '48 hours'`); err != nil {
			t.Fatal(err)
		}
		n, err = s.PruneRequests(ctx, 24)
		note("prune removes old rows: %s removed=%d", kind(err), n)

		// Rules.
		dev := func(ids ...string) func(*domain.Definition) {
			return func(d *domain.Definition) { d.Targets.DeviceIDs = ids }
		}
		cfg := func(ids ...string) func(*domain.Definition) {
			return func(d *domain.Definition) { d.Targets.DeviceConfigIDs = ids }
		}
		for _, r := range []*Rule{
			rule("r1", "t1", "Boiler Alarm", domain.KindAutomation, dev("d1", "d2")),
			rule("r2", "t1", "boiler spare", domain.KindScene, dev("d3")),
			rule("r3", "t1", "Café lamp", domain.KindAutomation, cfg("c1")),
			rule("r4", "t2", "Boiler Alarm", domain.KindAutomation, dev("d1")),
		} {
			note("create %s: %s", r.ID, kind(s.CreateRule(ctx, r)))
		}
		dup := rule("r1", "t1", "same id", domain.KindScene, nil)
		note("create with a taken id: %s", map[bool]string{true: "rejected", false: "accepted"}[s.CreateRule(ctx, dup) != nil])
		got, err := s.GetRule(ctx, "t1", "r1")
		if err == nil {
			note("get: revision=%d version=%d kind=%s name=%s targets=%s", got.Revision, got.Version, got.Kind, got.Name, raw(got.Definition.Targets.DeviceIDs))
		} else {
			note("get: %s", kind(err))
		}
		_, err = s.GetRule(ctx, "t2", "r1")
		note("get across tenants: %s", kind(err))
		updated := *got
		updated.Name = "Boiler Alarm v2"
		updated.DefinitionHash = "h2"
		note("update with a stale version: %s", kind(s.UpdateRule(ctx, &updated, 99)))
		note("update: %s revision=%d version=%d", kind(s.UpdateRule(ctx, &updated, got.Version)), updated.Revision, updated.Version)
		rev1, err := s.GetRuleRevision(ctx, "r1", 1)
		if err == nil {
			note("revision 1 is readable: name=%s hash=%s", rev1.Name, rev1.DefinitionHash)
		}
		v, err := s.SetEnabled(ctx, "t1", "r1", true, updated.Version)
		note("enable: %s version=%d", kind(err), v)
		_, err = s.SetEnabled(ctx, "t1", "r1", false, 1)
		note("enable with a stale version: %s", kind(err))

		// Listing and filters.
		ids := func(rules []*Rule, err error) string {
			var got []string
			for _, r := range rules {
				got = append(got, r.ID)
			}
			sort.Strings(got)
			return fmt.Sprintf("%s %v", kind(err), got)
		}
		yes := true
		note("list all of t1: %s", ids(s.ListRules(ctx, "t1", RuleFilter{})))
		note("list scenes: %s", ids(s.ListRules(ctx, "t1", RuleFilter{Kind: "scene"})))
		note("list by name 'BOILER': %s", ids(s.ListRules(ctx, "t1", RuleFilter{Name: "BOILER"})))
		note("list by name 'cafe' (accent-sensitive): %s", ids(s.ListRules(ctx, "t1", RuleFilter{Name: "cafe"})))
		note("list by name 'café': %s", ids(s.ListRules(ctx, "t1", RuleFilter{Name: "café"})))
		note("list enabled: %s", ids(s.ListRules(ctx, "t1", RuleFilter{Enabled: &yes})))
		note("list for device d2: %s", ids(s.ListRules(ctx, "t1", RuleFilter{DeviceID: "d2"})))
		note("list for device D2 (case-sensitive): %s", ids(s.ListRules(ctx, "t1", RuleFilter{DeviceID: "D2"})))
		note("list for device d9: %s", ids(s.ListRules(ctx, "t1", RuleFilter{DeviceID: "d9"})))
		note("list for template c1: %s", ids(s.ListRules(ctx, "t1", RuleFilter{DeviceConfigID: "c1"})))
		page, err := s.ListRules(ctx, "t1", RuleFilter{Limit: 2})
		total, err2 := s.CountRules(ctx, "t1", RuleFilter{})
		note("page of 2 then the true total: %s %d / %s %d", kind(err), len(page), kind(err2), total)
		note("delete: %s", kind(s.DeleteRule(ctx, "t1", "r2")))
		note("delete again: %s", kind(s.DeleteRule(ctx, "t1", "r2")))
		note("list after the delete: %s", ids(s.ListRules(ctx, "t1", RuleFilter{})))
		return out
	}
	a, b := run(pg), run(my)
	if !reflect.DeepEqual(a, b) {
		for i := range a {
			if i >= len(b) || a[i] != b[i] {
				bb := ""
				if i < len(b) {
					bb = b[i]
				}
				t.Errorf("step %d differs\n  pg: %s\n  my: %s", i, a[i], bb)
			}
		}
	}
	if len(a) != 35 {
		t.Fatalf("the scenario ended early (%d steps):\n%s", len(a), strings.Join(a, "\n"))
	}
	_ = sql.ErrNoRows
}
