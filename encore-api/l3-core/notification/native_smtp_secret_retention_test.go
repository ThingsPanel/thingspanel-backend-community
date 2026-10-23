package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// TestNativeUISMTPSecretRetainedAfterBlankEdit is an opt-in, read-only check
// against the native browser fixture. It verifies that changing only the
// instance name while leaving the password field blank preserved the secret
// across config versions 1 and 2.
func TestNativeUISMTPSecretRetainedAfterBlankEdit(t *testing.T) {
	if os.Getenv("NOTIFICATION_NATIVE_SMTP_RETENTION") != "1" {
		t.Skip("set NOTIFICATION_NATIVE_SMTP_RETENTION=1 to inspect the native UI fixture")
	}
	const fixtureDSN = "postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_core_native"
	if os.Getenv("NOTIFICATION_TEST_DSN") != fixtureDSN {
		t.Fatal("refusing to inspect a database outside the approved native fixture")
	}

	cfg, err := pgx.ParseConfig(fixtureDSN)
	if err != nil {
		t.Fatal("native fixture DSN is invalid")
	}
	ip := net.ParseIP(cfg.Host)
	if ip == nil || !ip.IsLoopback() || cfg.Port != 25543 || cfg.Database != "notification_core_native" || cfg.User != "notification_test" {
		t.Fatal("native fixture must be the approved loopback database and user")
	}

	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5_000_000_000)
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal("could not begin read-only native fixture transaction")
	}
	defer tx.Rollback()

	var databaseName, databaseUser string
	if err := tx.QueryRowContext(ctx, `SELECT current_database(), current_user`).Scan(&databaseName, &databaseUser); err != nil {
		t.Fatal("could not verify native fixture identity")
	}
	if databaseName != "notification_core_native" || databaseUser != "notification_test" {
		t.Fatal("connected database is not the approved native fixture")
	}

	var instanceID string
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `
		SELECT i.id, i.current_config_version
		FROM notification_instances i
		JOIN notification_instance_configs c
		  ON c.tenant_id=i.tenant_id AND c.instance_id=i.id
		JOIN notification_plugin_registrations p ON p.id=c.plugin_registration_id
		WHERE i.tenant_id='tenant-native'
		  AND p.plugin_id='thingspanel.smtp'
		  AND i.name='UI verified SMTP fixture'
		GROUP BY i.id, i.current_config_version
	`).Scan(&instanceID, &currentVersion); err != nil {
		t.Fatal("could not find the unique UI-verified SMTP fixture")
	}
	var instanceCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(DISTINCT i.id)
		FROM notification_instances i
		JOIN notification_instance_configs c
		  ON c.tenant_id=i.tenant_id AND c.instance_id=i.id
		JOIN notification_plugin_registrations p ON p.id=c.plugin_registration_id
		WHERE i.tenant_id='tenant-native'
		  AND p.plugin_id='thingspanel.smtp'
		  AND i.name='UI verified SMTP fixture'
	`).Scan(&instanceCount); err != nil || instanceCount != 1 || currentVersion != 2 {
		t.Fatal("expected exactly one UI-verified SMTP fixture at config version 2")
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	protector, err := NewAESGCMProtector("fixture-v1", map[string][]byte{"fixture-v1": key})
	if err != nil {
		t.Fatal("could not construct fixture snapshot protector")
	}

	values := make(map[int64]string, 2)
	rows, err := tx.QueryContext(ctx, `
		SELECT config_version, slot, key_ref, ciphertext, aad_version
		FROM notification_instance_secrets
		WHERE tenant_id='tenant-native' AND instance_id=$1
		  AND config_version IN (1,2) AND slot='from_password'
		ORDER BY config_version
	`, instanceID)
	if err != nil {
		t.Fatal("could not read encrypted SMTP fixture snapshots")
	}
	for rows.Next() {
		var version int64
		var slot, keyRef string
		var ciphertext []byte
		var aadVersion int
		if err := rows.Scan(&version, &slot, &keyRef, &ciphertext, &aadVersion); err != nil {
			rows.Close()
			t.Fatal("could not read encrypted SMTP fixture snapshot")
		}
		if slot != "from_password" || keyRef != "fixture-v1" {
			rows.Close()
			t.Fatal("SMTP fixture snapshot does not match the expected protected slot")
		}
		plaintext, err := protector.Open(ctx, SnapshotAAD{
			ScopeKind: "tenant", TenantID: "tenant-native", ResourceID: instanceID,
			Version: version, Purpose: "instance-secret", Slot: slot,
		}, ProtectedSnapshot{KeyRef: keyRef, Ciphertext: ciphertext, AADVersion: aadVersion})
		if err != nil {
			rows.Close()
			t.Fatal("could not authenticate and decrypt SMTP fixture snapshot")
		}
		var value string
		if err := json.Unmarshal(plaintext, &value); err != nil {
			rows.Close()
			t.Fatal("decrypted SMTP fixture snapshot is not a string")
		}
		values[version] = value
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal("could not finish reading encrypted SMTP fixture snapshots")
	}
	if err := rows.Close(); err != nil {
		t.Fatal("could not close encrypted SMTP fixture snapshot cursor")
	}

	const expectedFixturePassword = "FIXTURE_SMTP_SECRET"
	if len(values) != 2 {
		t.Fatal("expected encrypted SMTP passwords in both config versions")
	}
	if values[1] != expectedFixturePassword || values[2] != expectedFixturePassword || values[1] != values[2] {
		t.Fatal("blank UI password edit did not preserve the expected fixture secret")
	}
	t.Log("read-only native SMTP secret retention evidence: versions_match=true expected_fixture_match=true")
}
