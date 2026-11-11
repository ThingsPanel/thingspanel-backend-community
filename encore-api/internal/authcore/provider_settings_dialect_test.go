package authcore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestProviderSettingsBehaveTheSameOnMySQLAsOnPostgres(t *testing.T) {
	pg, my, _ := accountFixtures(t)
	// The PostgreSQL oracle needs the provider settings table too.
	migration, err := os.ReadFile("../../../sql/auth/003_provider_settings.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.DB.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	before := unsupportedStatements.Load()
	ctx := context.Background()
	admin := User{ID: "admin", Authority: "SYS_ADMIN"}
	key := []byte(strings.Repeat("k", 32))
	build := func(c ProviderConfig) (*Provider, error) { return &Provider{Config: c}, nil }

	run := func(a *SQLAccounts, d Dialect) []string {
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		ph := func(n int) string { return q(d, n) }
		for _, u := range [][]string{{"a2", "t1", "", "second@x.com"}, {"a1", "t1", "Alpha", "first@x.com"}, {"b1", "t2", "", "beta@x.com"}, {"c1", "t3", "", ""}, {"member", "t1", "Zed", "member@x.com"}} {
			authority := "TENANT_ADMIN"
			if u[0] == "member" {
				authority = "TENANT_USER"
			}
			if _, err := a.DB.Exec(`INSERT INTO users(id,email,name,tenant_id,authority,status,password,phone_number) VALUES(`+ph(1)+`,`+ph(2)+`,`+ph(3)+`,`+ph(4)+`,`+ph(5)+`,'N','h',`+ph(6)+`)`, u[0], u[3]+"|"+u[0], u[2], u[1], authority, "p-"+u[0]); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		// Make the emails the intended ones (the primary key above only keeps them distinct).
		for _, u := range [][]string{{"a2", "second@x.com"}, {"a1", "first@x.com"}, {"b1", "beta@x.com"}, {"c1", ""}} {
			if _, err := a.DB.Exec(`UPDATE users SET email=`+ph(1)+` WHERE id=`+ph(2), u[1], u[0]); err != nil {
				t.Fatalf("email: %v", err)
			}
		}
		m := &ProviderSettings{DB: a.DB, Dialect: a.Dialect, Key: key, Origin: "https://panel.example", Build: build}
		options, e := m.TenantOptions(ctx)
		var names []string
		for _, o := range options {
			names = append(names, o.ID+"="+o.Name)
		}
		note("TenantOptions: %s %v", kind(e), names)
		good := ProviderConfig{ID: "mini", Label: "WeChat", Kind: "wechat-mini", AppID: "app", AppSecret: "s3cret-value", TenantIDs: []string{"t1"}}
		note("Save new: %s", kind(m.Save(ctx, admin, providerChange{ID: "mini", ExpectedRevision: 0, Config: good})))
		note("Save new again with revision 0: %s", kind(m.Save(ctx, admin, providerChange{ID: "mini", ExpectedRevision: 0, Config: good})))
		list := func(label string) {
			l, e := m.List(ctx)
			var parts []string
			for _, s := range l {
				parts = append(parts, fmt.Sprintf("%s rev=%d validated=%d tested=%d published=%d enabled=%v secret=%v tested_at=%v label=%s", s.Config.ID, s.Revision, s.ValidatedRevision, s.TestedRevision, s.PublishedRevision, s.Enabled, s.SecretConfigured, s.TestedAt != nil, s.Config.Label))
			}
			note("%s: %s %v", label, kind(e), parts)
		}
		list("List after save")
		note("Save with an invalid id: %s", kind(m.Save(ctx, admin, providerChange{ID: "Bad Id", Config: ProviderConfig{ID: "Bad Id", Label: "x", Kind: "wechat-mini"}})))
		_, e = m.Validate(ctx, "mini", 7)
		note("Validate wrong revision: %s", kind(e))
		bad := good
		bad.ID, bad.TenantIDs = "orphan", []string{"nobody"}
		note("Save provider for an unknown tenant: %s", kind(m.Save(ctx, admin, providerChange{ID: "orphan", Config: bad})))
		_, e = m.Validate(ctx, "orphan", 1)
		note("Validate unknown tenant: %s %v", kind(e), e)
		_, e = m.Validate(ctx, "mini", 1)
		note("Validate: %s", kind(e))
		note("SetEnabled before testing: %s", kind(m.SetEnabled(ctx, admin, "mini", 1, true)))
		note("markTested: %s", kind(m.markTested(ctx, admin, "mini", 1)))
		note("SetEnabled: %s", kind(m.SetEnabled(ctx, admin, "mini", 1, true)))
		list("List after enable")
		resolved, e := m.Resolve(ctx, map[string]*Provider{})
		var ids []string
		for id, p := range resolved {
			ids = append(ids, id+":"+p.Config.ReviewID+":"+p.Config.Label)
		}
		note("Resolve: %s %v", kind(e), ids)
		changed := good
		changed.Label, changed.AppSecret = "Renamed", ""
		note("Update keeping the secret: %s", kind(m.Save(ctx, admin, providerChange{ID: "mini", ExpectedRevision: 1, Config: changed})))
		list("List after update")
		resolved, e = m.Resolve(ctx, map[string]*Provider{})
		ids = nil
		for id, p := range resolved {
			ids = append(ids, id+":"+p.Config.ReviewID+":"+p.Config.Label)
		}
		note("Resolve still serves the published draft: %s %v", kind(e), ids)
		moved := changed
		moved.AppID = "other-app"
		note("Change identity without a new secret: %s", kind(m.Save(ctx, admin, providerChange{ID: "mini", ExpectedRevision: 2, Config: moved})))
		note("Disable: %s", kind(m.SetEnabled(ctx, admin, "mini", 3, false)))
		resolved, e = m.Resolve(ctx, map[string]*Provider{})
		note("Resolve after disable: %s count=%d", kind(e), len(resolved))
		rows, e := a.DB.QueryContext(ctx, `SELECT user_id,event,provider FROM auth_audit_events ORDER BY id`)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		var events []string
		for rows.Next() {
			var u, ev, p string
			if err := rows.Scan(&u, &ev, &p); err != nil {
				t.Fatal(err)
			}
			events = append(events, u+"/"+ev+"/"+p)
		}
		note("audit: %s", strings.Join(events, " | "))
		return out
	}
	a, b := run(pg, DialectPostgres), run(my, DialectMySQL)
	if !reflect.DeepEqual(a, b) {
		for i := 0; i < len(a) || i < len(b); i++ {
			var x, y string
			if i < len(a) {
				x = a[i]
			}
			if i < len(b) {
				y = b[i]
			}
			if x != y {
				t.Errorf("step %d differs\n  pg: %s\n  my: %s", i, x, y)
			}
		}
	}
	joined := strings.Join(a, "\n")
	for _, want := range []string{"Save new: ok", "conflict", "Validate: ok", "markTested: ok", "SetEnabled: ok", "secret=true"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the oracle scenario did not reach %q:\n%s", want, joined)
		}
	}
	if got := unsupportedStatements.Load() - before; got != 0 {
		t.Fatalf("%d statements could not be translated for MySQL", got)
	}
}

// Records why the MySQL connection needs clientFoundRows=true: without it an UPDATE that rewrites a
// column to its current value reports zero affected rows.
func TestMySQLRowsAffectedSemanticsNeedClientFoundRows(t *testing.T) {
	_, my, seed := accountFixtures(t)
	seed(my.DB, DialectMySQL)
	base := os.Getenv("TP_AUTH_MYSQL_DSN")
	if !strings.Contains(base, "clientFoundRows=true") {
		t.Skip("TP_AUTH_MYSQL_DSN must set clientFoundRows=true for this test")
	}
	affected := func(db interface {
		Exec(string, ...any) (sql.Result, error)
	}) int64 {
		r, err := db.Exec("UPDATE users SET name = name WHERE id = 'u1'")
		if err != nil {
			t.Fatal(err)
		}
		n, _ := r.RowsAffected()
		return n
	}
	if got := affected(my.DB); got != 1 {
		t.Fatalf("with clientFoundRows=true a no-change UPDATE must report the matched row, got %d", got)
	}
	plain, err := sql.Open("mysql", strings.Replace(base, "&clientFoundRows=true", "", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if got := affected(plain); got != 0 {
		t.Fatalf("without the flag MySQL reports changed rows only, got %d", got)
	}
}
