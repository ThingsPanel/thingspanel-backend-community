package authcore

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPostgresIdentityUniquenessAndRevocation(t *testing.T) {
	dsn := os.Getenv("TP_AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated notification_test PostgreSQL fixture")
	}
	config, e := pgx.ParseConfig(dsn)
	if e != nil || config.Database != "notification_test" {
		t.Fatal("test requires notification_test fixture")
	}
	admin := stdlib.OpenDB(*config)
	defer admin.Close()
	schema := "auth_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(`CREATE SCHEMA ` + schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	config.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*config)
	defer db.Close()
	_, e = db.Exec(`CREATE TABLE users(id varchar(36) PRIMARY KEY,email text,name text,tenant_id text,authority text,status text,password text);
 CREATE TABLE app_push_devices(id text,user_id text,active boolean);
 CREATE TABLE sys_function(name text,enable_flag text);
 INSERT INTO users VALUES('u1','a@example.com','A','t1','TENANT_USER','N','hash'),('u2','b@example.com','B','t1','TENANT_USER','N','hash');
 INSERT INTO app_push_devices VALUES('phone','u1',true);`)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"001_identity.sql", "002_self_unlink.sql"} {
		migration, e := os.ReadFile("../../../sql/auth/" + name)
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			if _, e = db.Exec(string(migration)); e != nil {
				t.Fatal(e)
			}
		}
	}
	if _, e = db.Exec(`ALTER TABLE users ADD COLUMN phone_number text; UPDATE users SET phone_number='+86 13900000000' WHERE id='u1'`); e != nil {
		t.Fatal(e)
	}
	a := &SQLAccounts{DB: db}
	if u, e := a.User(context.Background(), "13900000000", true); e != nil || u.ID != "u1" {
		t.Fatal("phone alias compatibility", e)
	}
	if _, e = db.Exec(`UPDATE users SET phone_number='+1 13900000000' WHERE id='u2'`); e != nil {
		t.Fatal(e)
	}
	if _, e = a.User(context.Background(), "13900000000", true); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("ambiguous phone accepted", e)
	}

	ctx := context.Background()
	u1, e := a.User(ctx, "u1", false)
	if e != nil {
		t.Fatal(e)
	}
	u2, e := a.User(ctx, "u2", false)
	if e != nil {
		t.Fatal(e)
	}
	id := Identity{Issuer: "https://issuer", Subject: "same", Provider: "enterprise", ClientID: "web"}
	var wg sync.WaitGroup
	var wins atomic.Int32
	for _, u := range []User{u1, u2} {
		wg.Add(1)
		go func(u User) {
			defer wg.Done()
			e := a.Bind(ctx, u, id)
			if e == nil {
				wins.Add(1)
			} else if !errors.Is(e, ErrConflict) {
				t.Error(e)
			}
		}(u)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("identity ownership was not unique")
	}
	owner, e := a.FindIdentity(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	list, e := a.Bindings(ctx, owner)
	if e != nil || len(list) != 1 {
		t.Fatal("binding missing", e)
	}
	if e = a.Disable(ctx, User{ID: "other-admin", TenantID: "other", Authority: "TENANT_ADMIN"}, list[0].ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("cross tenant disable allowed")
	}
	actor := User{ID: "admin", TenantID: "t1", Authority: "TENANT_ADMIN"}
	if e = a.Disable(ctx, actor, list[0].ID); e != nil {
		t.Fatal(e)
	}
	current, e := a.User(ctx, owner.ID, false)
	if e != nil || current.Version != 1 {
		t.Fatal("version not advanced", e)
	}
	if _, e = a.FindIdentity(ctx, id); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("disabled identity remains usable")
	}
	if e = a.Bind(ctx, current, id); !errors.Is(e, ErrConflict) {
		t.Fatal("disabled identity silently rebound")
	}
	if e = a.Disable(ctx, actor, list[0].ID); e != nil {
		t.Fatal(e)
	}
	current, e = a.User(ctx, owner.ID, false)
	if e != nil || current.Version != 1 {
		t.Fatal("idempotent disable advanced version")
	}
	var rows int
	if e = db.QueryRow(`SELECT count(*) FROM auth_audit_events WHERE event='identity_disabled'`).Scan(&rows); e != nil || rows != 1 {
		t.Fatal("missing atomic audit")
	}
	if e = a.AllowRebind(ctx, User{ID: "other", Authority: "TENANT_ADMIN", TenantID: "other"}, list[0].ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("cross tenant rebind approval")
	}
	if e = a.AllowRebind(ctx, actor, list[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e = a.FindIdentity(ctx, id); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("approval enabled login without proof")
	}
	stranger := u1
	if stranger.ID == current.ID {
		stranger = u2
	}
	if e = a.Bind(ctx, stranger, id); !errors.Is(e, ErrConflict) {
		t.Fatal("approval transferred identity")
	}
	if e = a.Bind(ctx, current, id); e != nil {
		t.Fatal("approved owner could not rebind", e)
	}
	if _, e = a.FindIdentity(ctx, id); e != nil {
		t.Fatal(e)
	}
	if e = a.Disable(ctx, actor, list[0].ID); e != nil {
		t.Fatal(e)
	}
	current, e = a.User(ctx, owner.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Bind(ctx, current, id); !errors.Is(e, ErrConflict) {
		t.Fatal("reused rebind approval")
	}

	// An administrator block cannot be changed into a self-service release.
	if e = a.Unlink(ctx, current, list[0].ID); !errors.Is(e, ErrConflict) {
		t.Fatal("admin block self-unlinked", e)
	}
	if e = a.Bind(ctx, current, id); !errors.Is(e, ErrConflict) {
		t.Fatal("admin block bypassed", e)
	}
	if e = a.AllowRebind(ctx, actor, list[0].ID); e != nil {
		t.Fatal(e)
	}
	if e = a.Bind(ctx, current, id); e != nil {
		t.Fatal(e)
	}
	if e = a.Unlink(ctx, stranger, list[0].ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("other user unlinked identity", e)
	}
	oldVersion := current.Version
	if e = a.Unlink(ctx, current, list[0].ID); e != nil {
		t.Fatal(e)
	}
	current, e = a.User(ctx, current.ID, false)
	if e != nil || current.Version != oldVersion+1 {
		t.Fatal("unlink did not revoke sessions", e)
	}
	list, e = a.Bindings(ctx, current)
	if e != nil || list[0].Enabled || !list[0].SelfUnlinked {
		t.Fatal("incorrect voluntary release state", e)
	}
	if e = a.Bind(ctx, stranger, id); !errors.Is(e, ErrConflict) {
		t.Fatal("unlink transferred ownership", e)
	}
	if e = a.Bind(ctx, current, id); e != nil {
		t.Fatal("voluntary rebind failed", e)
	}
	if e = a.Unlink(ctx, current, list[0].ID); e != nil {
		t.Fatal(e)
	}
	if e = a.Disable(ctx, actor, list[0].ID); e != nil {
		t.Fatal(e)
	}
	current, _ = a.User(ctx, current.ID, false)
	if e = a.Bind(ctx, current, id); !errors.Is(e, ErrConflict) {
		t.Fatal("admin block after voluntary release bypassed", e)
	}

}
