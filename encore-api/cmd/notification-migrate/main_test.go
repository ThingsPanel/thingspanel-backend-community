package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestLoadMigrationsHasContiguousVersionedChecksums(t *testing.T) {
	migrations, err := loadMigrations(os.DirFS("../.."), "l3-core/notification/migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 3 || migrations[0].version != "0001" || migrations[1].version != "0002" || migrations[2].version != "0003" {
		t.Fatalf("unexpected migration set: %+v", migrations)
	}
	for _, m := range migrations {
		if len(m.checksum) != 64 {
			t.Fatalf("migration %s has invalid checksum %q", m.version, m.checksum)
		}
	}
}

func TestLoadMigrationsRejectsVersionGap(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"0001_first.up.sql", "0003_third.up.sql"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := loadMigrations(os.DirFS(dir), ".")
	if err == nil || !strings.Contains(err.Error(), "gap") {
		t.Fatalf("expected version gap rejection, got %v", err)
	}
}

func TestValidatePassfileRequiresRegular0600File(t *testing.T) {
	dir := t.TempDir()
	passfile := filepath.Join(dir, "pgpass")
	if err := os.WriteFile(passfile, []byte("fixture-only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validatePassfile(passfile); err != nil {
		t.Fatalf("valid passfile rejected: %v", err)
	}
	if err := os.Chmod(passfile, 0644); err != nil {
		t.Fatal(err)
	}
	if err := validatePassfile(passfile); err == nil {
		t.Fatal("world-readable passfile was accepted")
	}
	if err := os.Chmod(passfile, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "pgpass-link")
	if err := os.Symlink(passfile, link); err != nil {
		t.Fatal(err)
	}
	if err := validatePassfile(link); err == nil {
		t.Fatal("passfile symlink was accepted")
	}
	if err := validatePassfile(""); err == nil {
		t.Fatal("missing passfile was accepted")
	}
}

func TestNotificationMigrationCLIStatusUsesPGPassfileAndDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	conn := openMigrationFixture(t)
	appSchema, ledgerSchema := createOwnedSchemas(t, ctx, conn)
	passfile := filepath.Join(t.TempDir(), "pgpass")
	if err := os.WriteFile(passfile, []byte("127.0.0.1:25543:notification_test:notification_test:fixture-only-password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PGHOST", "127.0.0.1")
	t.Setenv("PGPORT", "25543")
	t.Setenv("PGUSER", "notification_test")
	t.Setenv("PGDATABASE", "notification_test")
	t.Setenv("PGPASSFILE", passfile)
	t.Setenv("PGPASSWORD", "")
	t.Setenv("NOTIFICATION_MIGRATE_EXPECTED_DB", "notification_test")
	t.Setenv("NOTIFICATION_MIGRATE_EXPECTED_SCHEMA", appSchema)
	t.Setenv("NOTIFICATION_MIGRATE_LEDGER_SCHEMA", ledgerSchema)
	t.Setenv("NOTIFICATION_MIGRATE_MODE", "status")
	migrationsDir, err := filepath.Abs(filepath.Join("..", "..", "l3-core", "notification", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFICATION_MIGRATIONS_DIR", migrationsDir)
	var output bytes.Buffer
	if err := runFromEnvironmentOutput(ctx, &output); err != nil {
		t.Fatalf("status-mode CLI failed: %v", err)
	}
	for _, want := range []string{"target_db=notification_test", "mode=status", "pending version=0001", "pending version=0002", "pending version=0003"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("status output missing %q: %s", want, output.String())
		}
	}
	var ledgerExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL`, ledgerSchema).Scan(&ledgerExists); err != nil {
		t.Fatal(err)
	}
	var appRelations int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1`, appSchema).Scan(&appRelations); err != nil {
		t.Fatal(err)
	}
	if ledgerExists || appRelations != 0 {
		t.Fatalf("status mode wrote metadata: ledger=%v application_relations=%d", ledgerExists, appRelations)
	}
}

func TestNotificationMigrationUpgradePreservesRowsAndRejectsDrift(t *testing.T) {
	ctx := context.Background()
	conn := openMigrationFixture(t)
	appSchema, ledgerSchema := createOwnedSchemas(t, ctx, conn)
	migrations, err := loadMigrations(os.DirFS("../.."), "l3-core/notification/migrations")
	if err != nil {
		t.Fatal(err)
	}
	opts := migrationOptions{expectedDB: "notification_test", applicationDB: "notification_test", appSchema: appSchema, ledgerSchema: ledgerSchema, apply: true}
	if err := applyMigrations(ctx, conn, opts, migrations[:1], ioDiscard{}); err != nil {
		t.Fatalf("apply migration 0001: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO notification_requests(tenant_id,id,source_type,source_id,source_deployment_id,source_event_id,action_id,intent_digest,intake_status,blocked_reason,expires_at)
		VALUES('migration-tenant','request-old','source','event-old','migration-deployment','event-old','action-old',$1,'blocked','legacy_blocked',NOW()+INTERVAL '1 day')`, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("seed pre-upgrade request: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO notification_idempotency(tenant_id,operation,idempotency_key,request_digest,resource_type,resource_id,resource_version)
		VALUES('migration-tenant','source','migration-event-key',$1,'notification','request-old',1)`, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("seed pre-upgrade idempotency row: %v", err)
	}
	if _, err := conn.Exec(ctx, `BEGIN`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO notification_plugin_registrations(id,plugin_id,plugin_version,origin,manifest_digest,manifest,auth_key_ref,auth_ciphertext,auth_aad_version)
		VALUES('migration-plugin','plugin.mail','1.0.0','https://plugin.invalid',repeat('a',64),'{}'::jsonb,'key-ref',decode('01','hex'),1)`); err != nil {
		_, _ = conn.Exec(ctx, `ROLLBACK`)
		t.Fatalf("seed pre-upgrade registration: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO notification_plugin_registration_states(registration_id,version,enabled,health) VALUES('migration-plugin',1,true,'unknown')`); err != nil {
		_, _ = conn.Exec(ctx, `ROLLBACK`)
		t.Fatalf("seed pre-upgrade registration state: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO notification_plugin_grants(tenant_id,registration_id,enabled,created_at) VALUES('migration-tenant','migration-plugin',false,'2026-01-02T03:04:05Z')`); err != nil {
		_, _ = conn.Exec(ctx, `ROLLBACK`)
		t.Fatalf("seed pre-upgrade grant: %v", err)
	}
	if _, err := conn.Exec(ctx, `COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(ctx, conn, opts, migrations, ioDiscard{}); err != nil {
		t.Fatalf("apply migrations 0002 and 0003: %v", err)
	}
	if err := applyMigrations(ctx, conn, opts, migrations, ioDiscard{}); err != nil {
		t.Fatalf("repeat migration run: %v", err)
	}
	var sourceType, intake, reason, digest string
	if err := conn.QueryRow(ctx, `SELECT source_type,intake_status,blocked_reason,intent_digest FROM notification_requests WHERE tenant_id='migration-tenant' AND id='request-old'`).Scan(&sourceType, &intake, &reason, &digest); err != nil {
		t.Fatal(err)
	}
	if sourceType != "alarm" || intake != "blocked" || reason != "legacy_blocked" || digest != strings.Repeat("a", 64) {
		t.Fatalf("request changed unexpectedly after migration: source=%q intake=%q reason=%q digest=%q", sourceType, intake, reason, digest)
	}
	var idemCount, grantVersion, grantStateCount int
	var enabled bool
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM notification_idempotency WHERE tenant_id='migration-tenant' AND idempotency_key='migration-event-key' AND resource_id='request-old'`).Scan(&idemCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `SELECT version,enabled FROM notification_plugin_grants WHERE tenant_id='migration-tenant' AND registration_id='migration-plugin'`).Scan(&grantVersion, &enabled); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM notification_plugin_grant_states WHERE tenant_id='migration-tenant' AND registration_id='migration-plugin' AND version=1 AND enabled=false AND created_at='2026-01-02T03:04:05Z'::timestamptz`).Scan(&grantStateCount); err != nil {
		t.Fatal(err)
	}
	if idemCount != 1 || grantVersion != 1 || enabled || grantStateCount != 1 {
		t.Fatalf("upgrade lost stable facts: idem=%d grantVersion=%d enabled=%v grantState=%d", idemCount, grantVersion, enabled, grantStateCount)
	}
	altered := append([]migration(nil), migrations...)
	altered[0].body += "\n-- checksum drift fixture\n"
	altered[0].checksum = strings.Repeat("f", 64)
	if err := applyMigrations(ctx, conn, opts, altered, ioDiscard{}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum drift rejection, got %v", err)
	}
}

func TestNotificationMigrationDDLAndLedgerRollbackTogether(t *testing.T) {
	ctx := context.Background()
	conn := openMigrationFixture(t)
	appSchema, ledgerSchema := createOwnedSchemas(t, ctx, conn)
	broken := migration{version: "0001", filename: "0001_broken.up.sql", body: "CREATE TABLE notification_rollback_probe(id INTEGER); SELECT 1/0;", checksum: strings.Repeat("0", 64)}
	opts := migrationOptions{expectedDB: "notification_test", applicationDB: "notification_test", appSchema: appSchema, ledgerSchema: ledgerSchema, apply: true}
	err := applyMigrations(ctx, conn, opts, []migration{broken}, ioDiscard{})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected transactional migration failure, got %v", err)
	}
	var relationExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, appSchema+".notification_rollback_probe").Scan(&relationExists); err != nil {
		t.Fatal(err)
	}
	if relationExists {
		t.Fatal("migration DDL survived the failed transaction")
	}
	var ledgerCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM `+quoteIdent(ledgerSchema)+`.`+quoteIdent(ledgerTable)).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 0 {
		t.Fatalf("failed migration created ledger row: %d", ledgerCount)
	}
}

func TestNotificationMigrationRefusesNonEmptyUntrackedSchema(t *testing.T) {
	ctx := context.Background()
	conn := openMigrationFixture(t)
	appSchema, ledgerSchema := createOwnedSchemas(t, ctx, conn)
	if _, err := conn.Exec(ctx, "CREATE TABLE "+quoteIdent(appSchema)+".notification_untracked_probe(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations(os.DirFS("../.."), "l3-core/notification/migrations")
	if err != nil {
		t.Fatal(err)
	}
	opts := migrationOptions{expectedDB: "notification_test", applicationDB: "notification_test", appSchema: appSchema, ledgerSchema: ledgerSchema, apply: true}
	if err := applyMigrations(ctx, conn, opts, migrations, ioDiscard{}); err == nil || !strings.Contains(err.Error(), "untracked schema") {
		t.Fatalf("expected non-empty untracked schema refusal, got %v", err)
	}
	var ledgerSchemaExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL`, ledgerSchema).Scan(&ledgerSchemaExists); err != nil {
		t.Fatal(err)
	}
	if ledgerSchemaExists {
		t.Fatal("tool created a ledger schema while refusing to adopt existing objects")
	}
}

func TestNotificationMigrationLeavesUnrelatedObjectsUntouched(t *testing.T) {
	ctx := context.Background()
	conn := openMigrationFixture(t)
	appSchema, ledgerSchema := createOwnedSchemas(t, ctx, conn)
	if _, err := conn.Exec(ctx, "CREATE TABLE "+quoteIdent(appSchema)+`.unrelated_probe(id INTEGER PRIMARY KEY, marker TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO "+quoteIdent(appSchema)+`.unrelated_probe(id,marker) VALUES (7,'preserve-me')`); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations(os.DirFS("../.."), "l3-core/notification/migrations")
	if err != nil {
		t.Fatal(err)
	}
	opts := migrationOptions{expectedDB: "notification_test", applicationDB: "notification_test", appSchema: appSchema, ledgerSchema: ledgerSchema}
	var status bytes.Buffer
	if err := applyMigrations(ctx, conn, opts, migrations, &status); err != nil {
		t.Fatalf("status in schema with unrelated table failed: %v", err)
	}
	var relationExists bool
	var ledgerExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, appSchema+".unrelated_probe").Scan(&relationExists); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL`, ledgerSchema).Scan(&ledgerExists); err != nil {
		t.Fatal(err)
	}
	if !relationExists || ledgerExists {
		t.Fatalf("status changed unrelated schema: unrelated_probe_exists=%v ledger_exists=%v", relationExists, ledgerExists)
	}
	opts.apply = true
	if err := applyMigrations(ctx, conn, opts, migrations, ioDiscard{}); err != nil {
		t.Fatalf("apply in schema with unrelated table failed: %v", err)
	}
	var marker string
	if err := conn.QueryRow(ctx, "SELECT marker FROM "+quoteIdent(appSchema)+`.unrelated_probe WHERE id=7`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != "preserve-me" {
		t.Fatalf("unrelated row changed: %q", marker)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func openMigrationFixture(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("NOTIFICATION_MIGRATE_TEST_DSN")
	if dsn == "" {
		t.Skip("set NOTIFICATION_MIGRATE_TEST_DSN to run isolated PostgreSQL migration tests")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid isolated migration fixture DSN")
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 25543 || cfg.Database != "notification_test" || cfg.User != "notification_test" || cfg.Password != "fixture-only-password" {
		t.Fatal("migration integration tests require the designated loopback:25543 notification_test fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal("could not connect to the designated isolated PostgreSQL fixture")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func createOwnedSchemas(t *testing.T, ctx context.Context, conn *pgx.Conn) (appSchema, ledgerSchema string) {
	t.Helper()
	var bytes [6]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(bytes[:])
	appSchema = "notification_migrate_" + suffix
	ledgerSchema = "notification_ledger_" + suffix
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoteIdent(appSchema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quoteIdent(appSchema)+" CASCADE")
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quoteIdent(ledgerSchema)+" CASCADE")
	})
	return appSchema, ledgerSchema
}
