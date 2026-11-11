package offlineupgrade

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Run against an empty disposable database only. The fixture uses the actual
// applied native migrations, including tenant constraints and audit schema.
func TestGroupsPostgresAtomicityReplayAndDrift(t *testing.T) {
	dsn := os.Getenv("UPGRADE_TEST_DSN")
	if dsn == "" {
		t.Skip("UPGRADE_TEST_DSN not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	var database string
	if err = db.Raw("SELECT current_database()").Scan(&database).Error; err != nil {
		t.Fatal(err)
	}
	if database != "tp_upgrade_rehearsal_20000101000001" {
		t.Fatal("disposable fixture database required")
	}
	var existing int64
	db.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema='public'").Scan(&existing)
	if existing != 0 {
		t.Fatal("fixture must be empty")
	}
	files, err := filepath.Glob("../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.Exec(string(sql)).Error; err != nil {
			t.Fatalf("migration %s: %v", filepath.Base(path), err)
		}
	}
	if err = db.Exec("CREATE TABLE schema_migrations(version bigint PRIMARY KEY,dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(40,false)").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("ALTER TABLE device_groups ADD CONSTRAINT fixture_failure CHECK (name <> 'fail')").Error; err != nil {
		t.Fatal(err)
	}
	in := fixtureInput()
	in.Groups = []json.RawMessage{fixtureGroup("a", "tenant", "0", "a"), fixtureGroup("b", "tenant", "a", "fail")}
	if _, err = ApplyGroupsPG(context.Background(), db, in); err == nil {
		t.Fatal("fixture constraint must abort")
	}
	for _, table := range []string{"device_groups", "metadata_audit_events"} {
		var count int64
		if err = db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("partial write survived rollback: %s", table)
		}
	}
	in.Groups[1] = fixtureGroup("b", "tenant", "a", "b")
	r, err := ApplyGroupsPG(context.Background(), db, in)
	if err != nil {
		t.Fatal(err)
	}
	if r.ConvertedGroups != 2 || r.MigrationComplete {
		t.Fatal("wrong accounting or premature cutover acceptance")
	}
	retry, err := ApplyGroupsPG(context.Background(), db, in)
	if err != nil {
		t.Fatal(err)
	}
	if retry.RowsSHA256 != r.RowsSHA256 || retry.State != "verified-existing-rehearsal-groups" {
		t.Fatal("retry did not verify exact rows")
	}
	var auditCount int64
	db.Table("metadata_audit_events").Count(&auditCount)
	if auditCount != 2 {
		t.Fatal("retry duplicated audit events")
	}
	// Same ID and count must not mask a changed source or changed target.
	in.Groups[1] = fixtureGroup("b", "tenant", "a", "changed")
	if _, err = ApplyGroupsPG(context.Background(), db, in); err == nil {
		t.Fatal("source drift accepted")
	}
	in.Groups[1] = fixtureGroup("b", "tenant", "a", "b")
	if err = db.Table("device_groups").Where("name = ?", "b").Update("description", "changed on target").Error; err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyGroupsPG(context.Background(), db, in); err == nil {
		t.Fatal("target drift accepted")
	}
	in.TargetDatabase = "tp_upgrade_rehearsal_20000101000002"
	if _, err = ApplyGroupsPG(context.Background(), db, in); err == nil {
		t.Fatal("connection to wrong database accepted")
	}
	verifyDeviceIdentityRehearsal(t, db)
	verifyCredentialRehearsal(t, db)
	verifyRelationshipRehearsal(t, db)
}

func verifyDeviceIdentityRehearsal(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("ALTER TABLE devices ADD CONSTRAINT fixture_device_failure CHECK (name <> 'fail-device')").Error; err != nil {
		t.Fatal(err)
	}
	in := fixtureDevices()
	in.Devices = []json.RawMessage{fixtureDevice("one", "tenant", "one", ""), fixtureDevice("two", "tenant", "fail-device", "")}
	if _, err := ApplyDeviceIdentitiesPG(context.Background(), db, in); err == nil {
		t.Fatal("device fixture constraint must fail")
	}
	var count int64
	db.Table("devices").Count(&count)
	if count != 0 {
		t.Fatal("partial device identities survived rollback")
	}
	db.Table("metadata_audit_events").Where("resource_kind = ?", "device").Count(&count)
	if count != 0 {
		t.Fatal("partial device audit survived rollback")
	}
	in.Devices[1] = fixtureDevice("two", "tenant", "two", "")
	receipt, err := ApplyDeviceIdentitiesPG(context.Background(), db, in)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ConvertedIdentities != 2 || receipt.PendingAccessConversions != 2 || receipt.MigrationComplete {
		t.Fatal("identity staging incorrectly declared full migration")
	}
	retry, err := ApplyDeviceIdentitiesPG(context.Background(), db, in)
	if err != nil {
		t.Fatal(err)
	}
	if retry.RowsSHA256 != receipt.RowsSHA256 || retry.State != "verified-existing-rehearsal-identities" {
		t.Fatal("identity retry did not verify stored content")
	}
	db.Table("device_access_bindings").Count(&count)
	if count != 0 {
		t.Fatal("identity stage invented access configuration")
	}
	in.Devices[1] = fixtureDevice("two", "tenant", "changed", "")
	if _, err := ApplyDeviceIdentitiesPG(context.Background(), db, in); err == nil {
		t.Fatal("identity source drift accepted")
	}
	in.Devices[1] = fixtureDevice("two", "tenant", "two", "")
	if err = db.Table("devices").Where("name = ?", "two").Update("administrative_state", "enabled").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyDeviceIdentitiesPG(context.Background(), db, in); err == nil {
		t.Fatal("identity target drift accepted")
	}
}
