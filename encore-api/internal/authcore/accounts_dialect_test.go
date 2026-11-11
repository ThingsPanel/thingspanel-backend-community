package authcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

func mysqlMigrationStatements(t *testing.T, name string) []string {
	t.Helper()
	contents, err := os.ReadFile("../../../sql/mysql/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	for _, part := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n-- @@\n") {
		var kept []string
		for _, line := range strings.Split(part, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				kept = append(kept, line)
			}
		}
		if statement := strings.TrimSpace(strings.Join(kept, "\n")); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

// accountFixtures returns the PostgreSQL oracle (the schema the existing PostgreSQL test builds) and
// the MySQL subject (the real MySQL migrations).
func accountFixtures(t *testing.T) (pg, my *SQLAccounts, seed func(db *sql.DB, d Dialect)) {
	t.Helper()
	pgDSN, myDSN := os.Getenv("TP_AUTH_TEST_DSN"), os.Getenv("TP_AUTH_MYSQL_DSN")
	if pgDSN == "" || myDSN == "" {
		t.Skip("TP_AUTH_TEST_DSN (notification_test) and TP_AUTH_MYSQL_DSN required")
	}
	config, e := pgx.ParseConfig(pgDSN)
	if e != nil || config.Database != "notification_test" {
		t.Fatal("test requires the notification_test fixture")
	}
	admin := stdlib.OpenDB(*config)
	schema := "auth_dx_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(`CREATE SCHEMA ` + schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	config.RuntimeParams["search_path"] = schema
	pgDB := stdlib.OpenDB(*config)
	t.Cleanup(func() { pgDB.Close() })
	if _, e = pgDB.Exec(`CREATE TABLE users(id varchar(36) PRIMARY KEY,email text,name text,tenant_id text,authority text,status text,password text,phone_number text);
 CREATE TABLE app_push_devices(id text,user_id text,active boolean);`); e != nil {
		t.Fatal(e)
	}
	if _, e = pgDB.Exec(`CREATE TABLE sys_function(id varchar(36) PRIMARY KEY, name varchar(50) NOT NULL, enable_flag varchar(20) NOT NULL, description varchar(500), remark varchar(255));
 INSERT INTO sys_function(id,name,enable_flag) VALUES ('function_2','enable_reg','disable'),('function_3','frontend_res','disable');
 CREATE TABLE boards(id varchar(36) PRIMARY KEY, name varchar(255) NOT NULL, config json DEFAULT '{}'::json, tenant_id varchar(36) NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, home_flag varchar(2) NOT NULL, description varchar(500), remark varchar(255), menu_flag varchar(2), vis_type varchar(50));
 CREATE TABLE casbin_rule(id serial PRIMARY KEY, ptype varchar(100), v0 varchar(100), v1 varchar(100), v2 varchar(100), v3 varchar(100), v4 varchar(100), v5 varchar(100));
 CREATE TABLE user_address(id serial PRIMARY KEY, user_id varchar(36) NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE, country varchar(50), province varchar(50), city varchar(50), district varchar(50), street varchar(100), detailed_address varchar(200), postal_code varchar(10), address_label varchar(50), longitude varchar(20), latitude varchar(20), additional_info varchar(500), created_time timestamptz DEFAULT CURRENT_TIMESTAMP, updated_time timestamptz DEFAULT CURRENT_TIMESTAMP);
 ALTER TABLE users ADD COLUMN IF NOT EXISTS created_at timestamptz, ADD COLUMN IF NOT EXISTS updated_at timestamptz, ADD COLUMN IF NOT EXISTS password_last_updated timestamptz, ADD COLUMN IF NOT EXISTS remark varchar(255), ADD COLUMN IF NOT EXISTS additional_info json DEFAULT '{}'::json, ADD COLUMN IF NOT EXISTS organization varchar(200), ADD COLUMN IF NOT EXISTS timezone varchar(50), ADD COLUMN IF NOT EXISTS default_language varchar(10), ADD COLUMN IF NOT EXISTS last_visit_time timestamptz, ADD COLUMN IF NOT EXISTS last_visit_ip varchar(30), ADD COLUMN IF NOT EXISTS last_visit_device varchar(200), ADD COLUMN IF NOT EXISTS password_fail_count int DEFAULT 0, ADD COLUMN IF NOT EXISTS avatar_url varchar(500);`); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"001_identity.sql", "002_self_unlink.sql"} {
		migration, e := os.ReadFile("../../../sql/auth/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pgDB.Exec(string(migration)); e != nil {
			t.Fatal(e)
		}
	}

	myDB, e := sql.Open("mysql", myDSN)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for _, table := range []string{"auth_audit_events", "auth_external_bindings", "auth_provider_settings", "app_push_devices", "user_address", "casbin_rule", "boards", "sys_function", "users"} {
			_, _ = myDB.Exec("DROP TABLE IF EXISTS " + table)
		}
		myDB.Close()
	})
	for _, table := range []string{"auth_audit_events", "auth_external_bindings", "auth_provider_settings", "app_push_devices", "user_address", "casbin_rule", "boards", "sys_function", "devices", "users"} {
		if _, e = myDB.Exec("DROP TABLE IF EXISTS " + table); e != nil {
			t.Fatal(e)
		}
	}
	conn, e := myDB.Conn(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	for _, name := range []string{"001_core_users_devices.sql", "003_auth.sql", "004_app_push_devices.sql", "005_account_support.sql"} {
		for _, statement := range mysqlMigrationStatements(t, name) {
			if _, e = conn.ExecContext(context.Background(), statement); e != nil {
				t.Fatalf("apply %s: %v\n%s", name, e, statement)
			}
		}
	}
	seed = func(db *sql.DB, d Dialect) {
		t.Helper()
		for _, u := range [][]string{{"u1", "a@example.com", "A", "+86 13900000000"}, {"u2", "b@example.com", "B", "+1 13900000001"}} {
			phone := u[3]
			if _, e := db.Exec(`INSERT INTO users(id,email,name,tenant_id,authority,status,password,phone_number) VALUES(`+q(d, 1)+`,`+q(d, 2)+`,`+q(d, 3)+`,'t1','TENANT_USER','N','hash',`+q(d, 4)+`)`, u[0], u[1], u[2], phone); e != nil {
				t.Fatalf("seed user: %v", e)
			}
		}
		if d == DialectPostgres {
			if _, e := db.Exec(`INSERT INTO app_push_devices VALUES('phone','u1',true)`); e != nil {
				t.Fatal(e)
			}
		} else if _, e := db.Exec(`INSERT INTO app_push_devices(id,tenant_id,user_id,app_id,installation_id,cid,platform,session_hash,expires_at) VALUES('00000000-0000-0000-0000-000000000001','t1','u1','app','inst','cid','ios','h',NOW(6))`); e != nil {
			t.Fatal(e)
		}
	}
	return &SQLAccounts{DB: pgDB}, &SQLAccounts{DB: myDB, Dialect: DialectMySQL}, seed
}

// q renders a positional placeholder for the seeding statements above.
func q(d Dialect, n int) string {
	if d == DialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func kind(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrUnavailable):
		return "unavailable"
	}
	return "other:" + err.Error()
}

func TestAccountsBehaveTheSameOnMySQLAsOnPostgres(t *testing.T) {
	pg, my, seed := accountFixtures(t)
	seed(pg.DB, DialectPostgres)
	seed(my.DB, DialectMySQL)
	before := unsupportedStatements.Load()
	ctx := context.Background()
	admin := User{ID: "admin", Authority: "SYS_ADMIN"}
	identity := Identity{Issuer: "https://issuer", Subject: "same", Provider: "enterprise", ClientID: "web"}

	run := func(a *SQLAccounts) []string {
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		user := func(key string, login bool) User {
			u, e := a.User(ctx, key, login)
			note("User(%s,login=%v): %s id=%s", key, login, kind(e), u.ID)
			return u
		}
		u1 := user("u1", false)
		user("nobody", false)
		user("a@example.com", true)
		user("13900000000", true) // phone alias of u1 only
		user("13900000001", true) // phone alias of u2 only
		user("1390000", true)     // too short to be an alias
		user("+86 13900000000", true)
		stale := u1
		stale.Version = 99
		_, e := a.FindIdentity(ctx, identity)
		note("FindIdentity before bind: %s", kind(e))
		note("Bind stale version: %s", kind(a.Bind(ctx, stale, identity)))
		note("Bind invalid identity: %s", kind(a.Bind(ctx, u1, Identity{Issuer: "https://issuer"})))
		note("Bind u1: %s", kind(a.Bind(ctx, u1, identity)))
		note("Bind u1 again (idempotent): %s", kind(a.Bind(ctx, u1, identity)))
		u2 := user("u2", false)
		note("Bind u2 to u1's identity: %s", kind(a.Bind(ctx, u2, identity)))
		note("Bind u1 with another provider on the same subject: %s", kind(a.Bind(ctx, u1, Identity{Issuer: identity.Issuer, Subject: identity.Subject, Provider: "other", ClientID: "web"})))
		found, e := a.FindIdentity(ctx, identity)
		note("FindIdentity after bind: %s id=%s", kind(e), found.ID)
		list, e := a.Bindings(ctx, u1)
		note("Bindings: %s count=%d enabled=%v self_unlinked=%v", kind(e), len(list), len(list) == 1 && list[0].Enabled, len(list) == 1 && list[0].SelfUnlinked)
		if len(list) != 1 {
			return out
		}
		id := list[0].ID
		note("Disable by a tenant user: %s", kind(a.Disable(ctx, u2, id)))
		note("Disable by admin: %s", kind(a.Disable(ctx, admin, id)))
		_, e = a.FindIdentity(ctx, identity)
		note("FindIdentity after disable: %s", kind(e))
		after := user("u1", false)
		note("auth_version advanced by %d", after.Version-u1.Version)
		note("Bind while disabled and not allowed: %s", kind(a.Bind(ctx, after, identity)))
		note("AllowRebind by a tenant user: %s", kind(a.AllowRebind(ctx, u2, id)))
		note("AllowRebind by admin: %s", kind(a.AllowRebind(ctx, admin, id)))
		note("AllowRebind on an enabled binding is impossible later: bind again %s", kind(a.Bind(ctx, after, identity)))
		note("AllowRebind on an enabled binding: %s", kind(a.AllowRebind(ctx, admin, id)))
		cur := user("u1", false)
		note("Unlink by the owner: %s", kind(a.Unlink(ctx, cur, id)))
		note("Unlink again: %s", kind(a.Unlink(ctx, user("u1", false), id)))
		list, _ = a.Bindings(ctx, u1)
		note("after unlink enabled=%v self_unlinked=%v", list[0].Enabled, list[0].SelfUnlinked)
		note("Disable by admin after a voluntary unlink: %s", kind(a.Disable(ctx, admin, id)))
		note("Bind after a voluntary unlink: %s", kind(a.Bind(ctx, user("u1", false), identity)))
		var events []string
		rows, e := a.DB.QueryContext(ctx, `SELECT actor_id,user_id,event,provider FROM auth_audit_events ORDER BY id`)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		for rows.Next() {
			var actor, uid, event, provider string
			if e := rows.Scan(&actor, &uid, &event, &provider); e != nil {
				t.Fatal(e)
			}
			events = append(events, strings.Join([]string{actor, uid, event, provider}, "/"))
		}
		note("audit trail: %s", strings.Join(events, " | "))
		var active int
		if e := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_push_devices WHERE user_id='u1' AND active`).Scan(&active); e != nil {
			t.Fatal(e)
		}
		note("active push devices for u1: %d", active)
		return out
	}
	a, b := run(pg), run(my)
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
	if len(a) < 30 {
		t.Fatalf("scenario ended early (%d steps):\n%s", len(a), strings.Join(a, "\n"))
	}
	if got := unsupportedStatements.Load() - before; got != 0 {
		t.Fatalf("%d statements could not be translated for MySQL", got)
	}
	if !strings.Contains(strings.Join(a, "\n"), "Bind u1: ok") || !strings.Contains(strings.Join(a, "\n"), "conflict") {
		t.Fatalf("the oracle did not exercise success and conflict:\n%s", strings.Join(a, "\n"))
	}
}

func TestMySQLRejectsOverlongIdentityAsInvalidNotUnavailable(t *testing.T) {
	_, my, seed := accountFixtures(t)
	seed(my.DB, DialectMySQL)
	u, e := my.User(context.Background(), "u1", false)
	if e != nil {
		t.Fatal(e)
	}
	long := Identity{Issuer: "https://issuer", Subject: strings.Repeat("s", 256), Provider: "p", ClientID: "c"}
	if got := kind(my.Bind(context.Background(), u, long)); got != "unauthorized" {
		t.Fatalf("a 256-character subject must be an invalid identity, got %s", got)
	}
	ok := Identity{Issuer: "https://issuer", Subject: strings.Repeat("é", 255), Provider: "p", ClientID: "c"}
	if got := kind(my.Bind(context.Background(), u, ok)); got != "ok" {
		t.Fatalf("255 characters (multi-byte) must be accepted, got %s", got)
	}
}

func TestDialectRewriteIsFailClosed(t *testing.T) {
	for _, query := range []string{`SELECT a::text FROM t`, `SELECT jsonb_agg(x) FROM t`, `INSERT INTO t VALUES($1) ON CONFLICT(id) DO UPDATE SET a=1`,
		`DELETE FROM t RETURNING id`, `SELECT pg_advisory_xact_lock(1)`, `SELECT DISTINCT ON (a) a FROM t`, `SELECT $2 FROM t`} {
		if _, _, ok := DialectMySQL.rewrite(query, []any{1}); ok {
			t.Errorf("a PostgreSQL-only statement must not be translated: %s", query)
		}
	}
	q, args, ok := DialectMySQL.rewrite(`SELECT x FROM t WHERE a=$2 AND b=$1 AND c=$2 AND d='$9 it''s' AND e<now()+interval '10 minutes'`, []any{"one", "two"})
	const want = `SELECT x FROM t WHERE a=? AND b=? AND c=? AND d='$9 it''s' AND e<NOW(6)+INTERVAL 10 MINUTE`
	if !ok || q != want {
		t.Fatalf("unexpected rewrite: ok=%v\n got %q\nwant %q", ok, q, want)
	}
	if len(args) != 3 || args[0] != "two" || args[1] != "one" || args[2] != "two" {
		t.Fatalf("repeated and reordered placeholders must expand in order of appearance: %v", args)
	}
}

func TestSuperAdminInitializationIsExclusiveOnMySQLAsOnPostgres(t *testing.T) {
	pg, my, _ := accountFixtures(t)
	if _, err := pg.DB.Exec(`SELECT 1`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run := func(a *SQLAccounts) []string {
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		has, e := a.HasAdmin(ctx)
		note("HasAdmin before: %s %v", kind(e), has)
		_, e = a.Initialize(ctx, "not-an-email", "Passw0rd!x")
		note("invalid email: %s", kind(e))
		_, e = a.Initialize(ctx, "root@example.com", "short")
		note("weak password: %s", kind(e))
		_, e = a.Initialize(ctx, "root@example.com", "ALLUPPER123")
		note("no lower-case letter: %s", kind(e))
		type result struct {
			err  error
			user User
		}
		results := make(chan result, 6)
		for i := 0; i < cap(results); i++ {
			go func(i int) {
				u, e := a.Initialize(ctx, fmt.Sprintf("root%d@example.com", i), "Passw0rd!x")
				results <- result{e, u}
			}(i)
		}
		kinds := map[string]int{}
		var winner User
		for i := 0; i < cap(results); i++ {
			r := <-results
			kinds[kind(r.err)]++
			if r.err == nil {
				winner = r.user
			}
		}
		note("concurrent initialisation outcomes: ok=%d conflict=%d other=%d", kinds["ok"], kinds["conflict"], len(kinds)-boolInt(kinds["ok"] > 0)-boolInt(kinds["conflict"] > 0))
		var admins int
		if e := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE authority='SYS_ADMIN'`).Scan(&admins); e != nil {
			t.Fatal(e)
		}
		note("SYS_ADMIN rows: %d", admins)
		has, e = a.HasAdmin(ctx)
		note("HasAdmin after: %s %v", kind(e), has)
		_, e = a.Initialize(ctx, "again@example.com", "Passw0rd!x")
		note("initialise again: %s", kind(e))
		var authority, status, phone, tenant, hash string
		var created, changed bool
		if e := a.dx().QueryRowContext(ctx, `SELECT authority,status,phone_number,COALESCE(tenant_id,''),password,created_at IS NOT NULL,password_last_updated IS NOT NULL FROM users WHERE id=$1`, winner.ID).Scan(&authority, &status, &phone, &tenant, &hash, &created, &changed); e != nil {
			t.Fatal(e)
		}
		note("stored admin: %s/%s phone=%q tenant=%q created=%v pwdtime=%v bcrypt=%v", authority, status, phone, tenant, created, changed, bcryptMatches(hash, "Passw0rd!x"))
		return out
	}
	a, b := run(pg), run(my)
	if !reflect.DeepEqual(a, b) {
		for i := range a {
			if i >= len(b) || a[i] != b[i] {
				t.Errorf("step %d differs\n  pg: %s\n  my: %v", i, a[i], b)
			}
		}
	}
	joined := strings.Join(a, "\n")
	if !strings.Contains(joined, "ok=1 conflict=5 other=0") || !strings.Contains(joined, "SYS_ADMIN rows: 1") {
		t.Fatalf("exactly one concurrent initialisation must win:\n%s", joined)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestSignupFlagAndProfileDetailMatchOnMySQLAndPostgres(t *testing.T) {
	pg, my, _ := accountFixtures(t)
	ctx := context.Background()
	run := func(a *SQLAccounts, d Dialect) []string {
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		on, e := a.Flag(ctx, "enable_reg")
		note("Flag enable_reg: %s %v", kind(e), on)
		unknown, e := a.Flag(ctx, "no_such_flag")
		note("Flag unknown: %s %v", kind(e), unknown)
		if _, e := a.DB.Exec(`UPDATE sys_function SET enable_flag='enable' WHERE name='enable_reg'`); e != nil {
			t.Fatal(e)
		}
		on, e = a.Flag(ctx, "enable_reg")
		note("Flag enable_reg after enabling: %s %v", kind(e), on)

		_, e = a.signup(ctx, "tenant@example.com", "Passw0rd!x", "+86 13800000000")
		note("signup: %s", kind(e))
		_, e = a.signup(ctx, "tenant@example.com", "Passw0rd!x", "+86 13800000001")
		note("signup same email: %s", kind(e))
		_, e = a.signup(ctx, "other@example.com", "Passw0rd!x", "+86 13800000000")
		note("signup same phone: %s", kind(e))
		type result struct {
			err  error
			user User
		}
		results := make(chan result, 6)
		for i := 0; i < cap(results); i++ {
			go func() {
				u, e := a.signup(ctx, "race@example.com", "Passw0rd!x", "+86 13900009999")
				results <- result{e, u}
			}()
		}
		kinds := map[string]int{}
		var winner User
		for i := 0; i < cap(results); i++ {
			r := <-results
			kinds[kind(r.err)]++
			if r.err == nil {
				winner = r.user
			}
		}
		note("concurrent signup of one email: ok=%d conflict=%d", kinds["ok"], kinds["conflict"])
		var users, boards int
		if e := a.dx().QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email='race@example.com'`).Scan(&users); e != nil {
			t.Fatal(e)
		}
		if e := a.dx().QueryRowContext(ctx, `SELECT COUNT(*) FROM boards WHERE tenant_id=$1 AND home_flag='Y'`, winner.TenantID).Scan(&boards); e != nil {
			t.Fatal(e)
		}
		note("rows for the raced email: users=%d home boards=%d", users, boards)
		var remark, authority, name string
		if e := a.dx().QueryRowContext(ctx, `SELECT COALESCE(remark,''),authority,name FROM users WHERE id=$1`, winner.ID).Scan(&remark, &authority, &name); e != nil {
			t.Fatal(e)
		}
		note("winner: authority=%s name=%s remark has the expiry layout=%v", authority, name, len(remark) >= 19 && remark[4] == '-' && remark[10] == ' ')
		var cfg string
		var cfgBytes []byte
		if e := a.dx().QueryRowContext(ctx, `SELECT config FROM boards WHERE tenant_id=$1`, winner.TenantID).Scan(&cfgBytes); e != nil {
			t.Fatal(e)
		}
		var board, want any
		if json.Unmarshal(cfgBytes, &board) != nil || json.Unmarshal([]byte(defaultBoard), &want) != nil {
			t.Fatal("board config is not JSON")
		}
		cfg = fmt.Sprint(reflect.DeepEqual(board, want))
		note("board config equals the default board: %s", cfg)

		// Profile detail with roles and an address.
		if _, e := a.dx().ExecContext(ctx, `UPDATE users SET organization=$1,timezone=$2,default_language=$3,last_visit_ip=$4,password_fail_count=$5,additional_info=$6 WHERE id=$7`, "Acme", "UTC", "en", "10.0.0.1", 2, `{"theme":"dark","n":[1,2]}`, winner.ID); e != nil {
			t.Fatal(e)
		}
		for _, role := range []string{"role-b", "role-a"} {
			if _, e := a.dx().ExecContext(ctx, `INSERT INTO casbin_rule(ptype,v0,v1) VALUES('g',$1,$2)`, winner.ID, role); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := a.dx().ExecContext(ctx, `INSERT INTO user_address(user_id,country,city,postal_code) VALUES($1,'CN','Shanghai','200000')`, winner.ID); e != nil {
			t.Fatal(e)
		}
		raw, e := a.Detail(ctx, winner.ID)
		note("Detail: %s", kind(e))
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			t.Fatalf("detail is not JSON: %s", raw)
		}
		address, _ := doc["address"].(map[string]any)
		note("detail keys=%d roles=%v organization=%v info=%v failures=%v address.city=%v address.country=%v address has user_id=%v", len(doc), doc["user_roles"], doc["organization"], doc["additional_info"], doc["password_fail_count"], address["city"], address["country"], address["user_id"])
		for _, key := range []string{"created_at", "updated_at", "password_last_updated"} {
			stamp, _ := doc[key].(string)
			_, parseErr := time.Parse(time.RFC3339Nano, stamp)
			note("detail %s is ISO 8601: %v", key, parseErr == nil)
		}
		for _, key := range []string{"last_visit_time", "avatar_url", "last_visit_device"} {
			note("detail %s: %v", key, doc[key])
		}
		_, e = a.Detail(ctx, "no-such-user")
		note("Detail unknown user: %s", kind(e))
		return out
	}
	a, b := run(pg, DialectPostgres), run(my, DialectMySQL)
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
	joined := strings.Join(a, "\n")
	if !strings.Contains(joined, "ok=1 conflict=5") || !strings.Contains(joined, "users=1 home boards=1") || !strings.Contains(joined, "roles=[role-b role-a]") {
		t.Fatalf("the oracle scenario did not behave as expected:\n%s", joined)
	}
}

func TestOpenAccountsDBSelectsAndValidatesTheDriver(t *testing.T) {
	cases := []struct {
		name    string
		config  Config
		dialect Dialect
		ok      bool
	}{
		{"default is PostgreSQL", Config{DatabaseURL: "postgres://u:p@localhost/db"}, DialectPostgres, true},
		{"explicit PostgreSQL", Config{DatabaseDriver: "postgres", DatabaseURL: "postgres://u:p@localhost/db"}, DialectPostgres, true},
		{"MySQL with both options", Config{DatabaseDriver: "mysql", DatabaseURL: "u:p@tcp(localhost:3306)/db?parseTime=true&clientFoundRows=true"}, DialectMySQL, true},
		{"MySQL without clientFoundRows", Config{DatabaseDriver: "mysql", DatabaseURL: "u:p@tcp(localhost:3306)/db?parseTime=true"}, DialectMySQL, false},
		{"MySQL without parseTime", Config{DatabaseDriver: "mysql", DatabaseURL: "u:p@tcp(localhost:3306)/db?clientFoundRows=true"}, DialectMySQL, false},
		{"MySQL with a PostgreSQL URL", Config{DatabaseDriver: "mysql", DatabaseURL: "postgres://u:p@localhost/db"}, DialectMySQL, false},
		{"unknown driver", Config{DatabaseDriver: "sqlite", DatabaseURL: "x"}, DialectPostgres, false},
		{"case matters", Config{DatabaseDriver: "MySQL", DatabaseURL: "u:p@tcp(localhost)/db?parseTime=true&clientFoundRows=true"}, DialectPostgres, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, dialect, err := openAccountsDB(c.config)
			if db != nil {
				defer db.Close()
			}
			if (err == nil) != c.ok || (c.ok && dialect != c.dialect) {
				t.Fatalf("ok=%v dialect=%v err=%v", c.ok, dialect, err)
			}
			if err != nil && !errors.Is(err, ErrUnavailable) {
				t.Fatalf("a refused configuration must be reported as unavailable: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "u:p") {
				t.Fatalf("the error leaks the credentials: %v", err)
			}
		})
	}
}

func TestSessionsFollowTheAccountDatabaseOnMySQLAsOnPostgres(t *testing.T) {
	pg, my, _ := accountFixtures(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	run := func(a *SQLAccounts, d Dialect) []string {
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		if _, e := a.DB.Exec(`INSERT INTO users(id,email,name,tenant_id,authority,status,password,phone_number) VALUES(`+q(d, 1)+`,`+q(d, 2)+`,'Sam','t1','TENANT_USER','N',`+q(d, 3)+`,'+1 555')`, "sess-user", "sess@example.com", string(hash)); e != nil {
			t.Fatal(e)
		}
		r := miniredis.RunT(t)
		s := &Service{Accounts: a, Redis: redis.NewClient(&redis.Options{Addr: r.Addr()}), Key: []byte(strings.Repeat("s", 32)), TTL: time.Hour, MaxFailures: 3, LockDuration: time.Minute}
		defer s.Redis.Close()
		ctx := context.Background()
		_, e := s.VerifyPassword(ctx, "sess@example.com", "wrong-password", "")
		note("wrong password: %s", kind(e))
		u, e := s.VerifyPassword(ctx, "sess@example.com", "correct-password", "")
		note("right password: %s id=%s authority=%s", kind(e), u.ID, u.Authority)
		first, e := s.Issue(ctx, u)
		note("issue: %s", kind(e))
		c, e := s.Check(ctx, first.Token)
		note("check: %s id=%s version=%d", kind(e), c.ID, c.Version)
		if _, e := a.dx().ExecContext(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, "sess-user"); e != nil {
			t.Fatal(e)
		}
		_, e = s.Check(ctx, first.Token)
		note("check after the credential version moved: %s", kind(e))
		u, _ = s.VerifyPassword(ctx, "sess@example.com", "correct-password", "")
		second, e := s.Issue(ctx, u)
		note("re-issue: %s", kind(e))
		c, e = s.Check(ctx, second.Token)
		note("check the new session: %s version=%d", kind(e), c.Version)
		if _, e := a.dx().ExecContext(ctx, `UPDATE users SET status='F' WHERE id=$1`, "sess-user"); e != nil {
			t.Fatal(e)
		}
		_, e = s.Check(ctx, second.Token)
		note("check a frozen account: %s", kind(e))
		_, e = s.VerifyPassword(ctx, "sess@example.com", "correct-password", "")
		note("password login of a frozen account: %s", kind(e))
		if _, e := a.dx().ExecContext(ctx, `UPDATE users SET status='N' WHERE id=$1`, "sess-user"); e != nil {
			t.Fatal(e)
		}
		note("logout: %s", kind(s.Logout(ctx, second.Token)))
		_, e = s.Check(ctx, second.Token)
		note("check after logout: %s", kind(e))
		return out
	}
	a, b := run(pg, DialectPostgres), run(my, DialectMySQL)
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
	joined := strings.Join(a, "\n")
	if !strings.Contains(joined, "right password: ok") || !strings.Contains(joined, "check: ok") || !strings.Contains(joined, "check after logout: unauthorized") {
		t.Fatalf("the oracle scenario did not behave as expected:\n%s", joined)
	}
}
