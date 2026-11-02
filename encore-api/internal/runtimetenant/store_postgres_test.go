package runtimetenant

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func sourceFixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_RUNTIME_TENANT_DATABASE_DSN")
	if dsn == "" {
		t.Skip("disposable runtime tenant PostgreSQL DSN not configured")
	}
	root, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal("open isolated runtime source root")
	}
	schema := "runtime_source_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = root.Exec("CREATE SCHEMA " + schema); e != nil {
		t.Fatal("create isolated source schema")
	}
	scopedDSN := dsn + " search_path=" + schema
	if u, e := url.Parse(dsn); e == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		scopedDSN = u.String()
	}
	db, e := sql.Open("pgx", scopedDSN)
	if e != nil {
		t.Fatal("open isolated source schema")
	}
	t.Cleanup(func() { db.Close(); root.Exec("DROP SCHEMA " + schema + " CASCADE"); root.Close() })
	if _, e = db.Exec("CREATE TABLE device_runtime_outbox(id uuid primary key,tenant_id uuid NOT NULL)"); e != nil {
		t.Fatal("create isolated legacy outbox")
	}
	migration, e := os.ReadFile(filepath.Join("..", "..", "devicemetadata", "migrations", "0040_runtime_tenant_sources.up.sql"))
	if e != nil {
		t.Fatal("read source binding migration")
	}
	if _, e = db.Exec(string(migration)); e != nil {
		t.Fatal("apply isolated source binding migration")
	}
	return db
}
func sourceVerifier(raw string) fixtureVerifier {
	return fixtureVerifier{map[string]Principal{"admin": {UserID: "fixture-platform-admin", Authority: "SYS_ADMIN"}, "tenant": {UserID: "fixture-tenant-" + raw, RawTenantID: raw, Authority: "TENANT_ADMIN"}}}
}
func TestSourceSchemaPostgresCollisionAndTerminalLifecycle(t *testing.T) {
	db := sourceFixtureDB(t)
	ctx := context.Background()
	scope := fixtureScope()
	native := "1234abcd-0000-0000-0000-000000000000"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	// Both valid fixture raw identities normalize to the same UUID. Only one
	// explicitly reviewed owner may reserve it, even under concurrent provision.
	for _, raw := range []string{"1234abcd", "1234-abcd"} {
		wg.Add(1)
		go func(raw string) {
			defer wg.Done()
			_, e := seedIsolatedSchemaBinding(ctx, db, raw, scope, native)
			results <- e
		}(raw)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("colliding owners accepted=%d", success)
	}
	binding, e := Lookup(ctx, SQLReader{db}, native, scope)
	if e != nil {
		t.Fatal("load winning source")
	}
	otherRaw := "1234abcd"
	if binding.RawTenantID == otherRaw {
		otherRaw = "1234-abcd"
	}
	if _, e = Revoke(ctx, db, sourceVerifier(otherRaw), "admin", "tenant", scope, native, strings.Repeat("d", 64)); e == nil {
		t.Fatal("colliding foreign raw principal revoked another binding")
	}
	var audits int
	if e = db.QueryRow("SELECT count(*) FROM runtime_tenant_source_audit").Scan(&audits); e != nil || audits != 1 {
		t.Fatal("audit not atomic with unique reservation")
	}
	legacyID := uuid.NewString()
	if _, e = db.Exec("INSERT INTO device_runtime_outbox(id,tenant_id) VALUES($1,$2)", legacyID, native); e != nil {
		t.Fatal("seed unproven historical outbox")
	}
	if _, e = db.Exec("UPDATE device_runtime_outbox SET source_binding_id=$1,source_binding_generation=1 WHERE id=$2", binding.ID, legacyID); e == nil {
		t.Fatal("historical proof was automatically adoptable")
	}
	if _, e = db.Exec("INSERT INTO device_runtime_outbox(id,tenant_id,source_binding_id,source_binding_generation) VALUES($1,$2,$3,1)", uuid.NewString(), uuid.NewString(), binding.ID); e == nil {
		t.Fatal("foreign tenant reused binding proof")
	}
	if _, e = Revoke(ctx, db, sourceVerifier(binding.RawTenantID), "admin", "tenant", scope, native, strings.Repeat("b", 64)); e != nil {
		t.Fatal("terminal owner revocation failed")
	}
	if _, e = Lookup(ctx, SQLReader{db}, native, scope); e == nil {
		t.Fatal("revoked mapping remained usable")
	}
	for _, query := range []string{"DELETE FROM runtime_tenant_sources", "UPDATE runtime_tenant_sources SET state='active',revoked_at=NULL", "DELETE FROM runtime_tenant_source_audit"} {
		if _, e = db.Exec(query); e == nil {
			t.Fatal("immutable source or audit was changed")
		}
	}
	if _, e = seedIsolatedSchemaBinding(ctx, db, binding.RawTenantID, scope, native); e == nil {
		t.Fatal("revoked owner was rebound")
	}
}

// This is privileged disposable-schema setup, NOT a registration authority or
// an acceptance test of the paused production Provision operation.
func seedIsolatedSchemaBinding(ctx context.Context, db *sql.DB, raw string, scope Scope, native string) (Binding, error) {
	b := Binding{ID: uuid.NewString(), TenantID: native, RawTenantID: raw, Scope: scope, Generation: 1}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return b, e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `INSERT INTO runtime_tenant_sources(id,tenant_id,source_tenant_id,environment,provider_id,namespace,service_instance_id,community_origin,generation,reviewed_manifest_sha256,platform_actor_id,tenant_actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,'isolated-platform-admin','isolated-tenant-admin')`, b.ID, native, raw, scope.Environment, scope.ProviderID, scope.Namespace, scope.ServiceInstanceID, scope.CommunityOrigin, strings.Repeat("a", 64))
	if e != nil {
		return b, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO runtime_tenant_source_audit(id,binding_id,action,platform_actor_id,tenant_actor_id,reviewed_manifest_sha256) VALUES($1,$2,'provision','isolated-platform-admin','isolated-tenant-admin',$3)`, uuid.NewString(), b.ID, strings.Repeat("a", 64))
	if e != nil {
		return b, e
	}
	return b, tx.Commit()
}
