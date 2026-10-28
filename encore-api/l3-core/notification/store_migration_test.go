package notification

import (
	"context"
	"strings"
	"testing"
)

func TestNotificationMigrationFailureRollsBackAndCanReenter(t *testing.T) {
	db := openNotificationTestDB(t)
	ctx := context.Background()
	migration := readNotificationMigration(t)
	broken := migration + `
CREATE TABLE notification_migration_failure_probe (id INTEGER);
CREATE TABLE notification_migration_failure_probe (id INTEGER);
`
	if err := runTestMigrationOnce(ctx, db, "0001", broken); err == nil {
		t.Fatal("expected the deliberately duplicated DDL to fail")
	}
	var created bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('notification_plugin_registrations') IS NOT NULL`).Scan(&created); err != nil {
		t.Fatalf("check DDL rollback: %v", err)
	}
	if created {
		t.Fatal("migration DDL remained after the transaction failed")
	}
	var recorded bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM notification_test_migration_versions WHERE version = '0001')`).Scan(&recorded); err != nil {
		t.Fatalf("check failed migration ledger: %v", err)
	}
	if recorded {
		t.Fatal("failed migration was recorded as applied")
	}

	if err := runTestMigrationOnce(ctx, db, "0001", migration); err != nil {
		t.Fatalf("retry migration after rollback: %v", err)
	}
	if err := runTestMigrationOnce(ctx, db, "0001", migration); err != nil {
		t.Fatalf("repeat migration entry: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('notification_plugin_registrations') IS NOT NULL`).Scan(&created); err != nil {
		t.Fatalf("check applied migration: %v", err)
	}
	if !created {
		t.Fatal("migration did not create notification schema")
	}
	var versions int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_test_migration_versions WHERE version = '0001'`).Scan(&versions); err != nil {
		t.Fatalf("check migration version count: %v", err)
	}
	if versions != 1 {
		t.Fatalf("expected one applied migration record, got %d", versions)
	}
	var immutableTrigger bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_trigger WHERE tgname = 'notification_instance_config_immutable' AND NOT tgisinternal
	)`).Scan(&immutableTrigger); err != nil {
		t.Fatalf("check immutable snapshot trigger: %v", err)
	}
	if !immutableTrigger {
		t.Fatal("immutable instance config trigger is missing")
	}
	if strings.Contains(strings.ToUpper(migration), "DROP SCHEMA") || strings.Contains(strings.ToUpper(migration), "DROP TABLE") {
		t.Fatal("notification migration must not drop schema or existing tables")
	}
}
