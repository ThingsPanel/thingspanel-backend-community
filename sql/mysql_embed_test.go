package sql

import (
	"bytes"
	"testing"
)

func TestMariaDBMigrationOverridesDoNotMutateMySQLFiles(t *testing.T) {
	for _, name := range []string{"001_core_users_devices.sql", "002_device_identity.sql", "004_app_push_devices.sql", "007_service_access.sql", "011_automation_partial_unique.sql", "012_platform_base.sql"} {
		mysql, err := MySQLFamilyMigrationContents("mysql", name)
		if err != nil {
			t.Fatal(err)
		}
		maria, err := MySQLFamilyMigrationContents("mariadb", name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(mysql, maria) {
			t.Errorf("%s: expected an explicit MariaDB override", name)
		}
	}
	sharedMySQL, err := MySQLFamilyMigrationContents("mysql", "003_auth.sql")
	if err != nil {
		t.Fatal(err)
	}
	sharedMaria, err := MySQLFamilyMigrationContents("mariadb", "003_auth.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sharedMySQL, sharedMaria) {
		t.Fatal("shared migration unexpectedly differs between MySQL-family flavors")
	}
}
