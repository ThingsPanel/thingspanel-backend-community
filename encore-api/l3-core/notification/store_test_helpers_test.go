package notification

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type testSnapshotProtector struct {
	key []byte
}

func createFixtureInstance(t *testing.T, store *Store, tenantID string) string {
	t.Helper()
	ctx := context.Background()
	manifest := contract.PluginManifestV1{
		APIVersion: "1.0", PluginID: "fixture-plugin", Name: "Fixture plugin", PluginVersion: "1.0.0",
		Channels: []contract.Channel{contract.ChannelEmail}, ContentModes: []string{"text"},
		ConfigSchema: json.RawMessage(`{"type":"object"}`), RecipientSchema: json.RawMessage(`{"type":"object"}`),
		SecretFields: []string{"token"},
	}
	plugin, err := store.RegisterPlugin(ctx, tenantID, "fixture-plugin-reg-001", strings.Repeat("a", 64), contract.PluginRegistration{
		PluginID: manifest.PluginID, PluginVersion: manifest.PluginVersion, Origin: "https://fixture.example.test", AuthSecret: "fixture-auth-value", ManifestDigest: strings.Repeat("b", 64),
	}, manifest)
	if err != nil {
		t.Fatalf("create fixture plugin: %v", err)
	}
	grantFixturePlugin(t, store, tenantID, plugin.ID)
	instance, err := store.CreateInstance(ctx, tenantID, "fixture-instance-001", strings.Repeat("c", 64), contract.InstanceCreate{
		PluginRegistrationID: plugin.ID, Name: "fixture account", Channel: contract.ChannelEmail,
		Config: json.RawMessage(`{"region":"test"}`), ProviderIdentity: map[string]string{"account": "fixture"},
	}, map[string]string{"token": "fixture-instance-secret"})
	if err != nil {
		t.Fatalf("create fixture instance: %v", err)
	}
	enabled := true
	if _, err := store.UpdateInstance(ctx, tenantID, instance.ID, "fixture-instance-enable-01", strings.Repeat("d", 64), contract.InstanceUpdate{ExpectedVersion: 1, Enabled: &enabled}); err != nil {
		t.Fatalf("enable fixture instance: %v", err)
	}
	return instance.ID
}

func grantFixturePlugin(t *testing.T, store *Store, tenantID, registrationID string) {
	t.Helper()
	key := "fixture-grant-" + registrationID
	digest := strings.Repeat("f", 64)
	_, _, err := store.SetPluginGrant(context.Background(), tenantID, registrationID, key, digest, contract.PluginGrantUpdate{
		PluginRegistrationID: registrationID, TenantID: tenantID, Enabled: true, ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("create explicit fixture plugin grant: %v", err)
	}
}

func fixtureSubmission(instanceID string, recipient string) contract.NotificationSubmit {
	expires := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	text := "fixture notification body"
	return contract.NotificationSubmit{
		Source: contract.SourceRef{Type: "test", ID: "fixture-source"}, ExpiresAt: expires,
		Deliveries: []contract.SubmitDelivery{{InstanceID: instanceID, Recipient: contract.Recipient{Kind: "email", Address: recipient}, Content: contract.Content{Kind: "text", Text: &text}}},
	}
}

func newTestSnapshotProtector() *testSnapshotProtector {
	sum := sha256.Sum256([]byte("notification fixture key, not a real credential"))
	return &testSnapshotProtector{key: sum[:]}
}

func (p *testSnapshotProtector) Seal(_ context.Context, aad SnapshotAAD, plaintext []byte) (ProtectedSnapshot, error) {
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return ProtectedSnapshot{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ProtectedSnapshot{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ProtectedSnapshot{}, err
	}
	aadBytes, err := json.Marshal(aad)
	if err != nil {
		return ProtectedSnapshot{}, err
	}
	ciphertext := append(nonce, gcm.Seal(nil, nonce, plaintext, aadBytes)...)
	return ProtectedSnapshot{KeyRef: "test-key-v1", Ciphertext: ciphertext, AADVersion: 1}, nil
}

func (p *testSnapshotProtector) Open(_ context.Context, aad SnapshotAAD, protected ProtectedSnapshot) ([]byte, error) {
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(protected.Ciphertext) < gcm.NonceSize() {
		return nil, errors.New("invalid fixture ciphertext")
	}
	aadBytes, err := json.Marshal(aad)
	if err != nil {
		return nil, err
	}
	nonce := protected.Ciphertext[:gcm.NonceSize()]
	return gcm.Open(nil, nonce, protected.Ciphertext[gcm.NonceSize():], aadBytes)
}

// openNotificationTestDB only accepts the fixture explicitly provisioned for
// this task and confines every test to a schema it created itself.
func openNotificationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_DSN is required for isolated PostgreSQL tests")
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("NOTIFICATION_TEST_DSN is invalid")
	}
	ip := net.ParseIP(cfg.Host)
	if ip == nil || !ip.IsLoopback() || cfg.Port != 25543 || cfg.Database != "notification_test" || cfg.User != "notification_test" {
		t.Fatal("NOTIFICATION_TEST_DSN must target the loopback:25543 notification_test fixture as notification_test")
	}

	baseDB := stdlib.OpenDB(*cfg)
	if err := baseDB.PingContext(context.Background()); err != nil {
		baseDB.Close()
		t.Fatal("notification PostgreSQL fixture is unreachable")
	}
	schema := "notification_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := baseDB.ExecContext(context.Background(), "CREATE SCHEMA "+quotedSchema); err != nil {
		baseDB.Close()
		t.Fatal("could not create isolated notification test schema")
	}

	isolatedCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		_, _ = baseDB.ExecContext(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
		baseDB.Close()
		t.Fatal("NOTIFICATION_TEST_DSN is invalid")
	}
	isolatedCfg.RuntimeParams["search_path"] = schema
	isolatedCfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	testDB := stdlib.OpenDB(*isolatedCfg)
	if err := testDB.PingContext(context.Background()); err != nil {
		testDB.Close()
		_, _ = baseDB.ExecContext(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
		baseDB.Close()
		t.Fatal("isolated notification test schema is unreachable")
	}
	t.Cleanup(func() {
		_ = testDB.Close()
		_, _ = baseDB.ExecContext(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
		_ = baseDB.Close()
	})
	return testDB
}

func readNotificationMigration(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("migrations/0001_notification.up.sql")
	if err != nil {
		t.Fatalf("read notification migration: %v", err)
	}
	return string(data)
}

func readNotificationGrantMigration(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("migrations/0002_notification_plugin_grants.up.sql")
	if err != nil {
		t.Fatalf("read notification grant migration: %v", err)
	}
	return string(data)
}

func readNotificationAlarmSourceMigration(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("migrations/0003_notification_alarm_source_type.up.sql")
	if err != nil {
		t.Fatalf("read notification alarm source migration: %v", err)
	}
	return string(data)
}

func newMigratedTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db := openNotificationTestDB(t)
	if err := runTestMigrationOnce(context.Background(), db, "0001", readNotificationMigration(t)); err != nil {
		t.Fatalf("apply notification migration: %v", err)
	}
	if err := runTestMigrationOnce(context.Background(), db, "0002", readNotificationGrantMigration(t)); err != nil {
		t.Fatalf("apply notification plugin grant migration: %v", err)
	}
	if err := runTestMigrationOnce(context.Background(), db, "0003", readNotificationAlarmSourceMigration(t)); err != nil {
		t.Fatalf("apply notification alarm source migration: %v", err)
	}
	store, err := NewStore(StoreOptions{DB: db, Protector: newTestSnapshotProtector()})
	if err != nil {
		t.Fatalf("construct notification store: %v", err)
	}
	return store, db
}

func runTestMigrationOnce(ctx context.Context, db *sql.DB, version, migration string) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS notification_test_migration_versions (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("create test migration ledger: %w", err)
	}
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM notification_test_migration_versions WHERE version = $1
	)`, version).Scan(&exists); err != nil {
		return fmt.Errorf("read test migration ledger: %w", err)
	}
	if exists {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_test_migration_versions(version) VALUES ($1)`, version); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
