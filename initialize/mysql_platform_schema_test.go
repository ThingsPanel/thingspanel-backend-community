package initialize

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	componentsql "project/sql"
)

func TestLegacyLockAcquisitionResult(t *testing.T) {
	for _, tc := range []struct {
		name      string
		result    sql.NullInt64
		acquired  bool
		uncertain bool
	}{
		{name: "null means not acquired", result: sql.NullInt64{}},
		{name: "zero means busy", result: sql.NullInt64{Int64: 0, Valid: true}},
		{name: "one means acquired", result: sql.NullInt64{Int64: 1, Valid: true}, acquired: true},
		{name: "unexpected result is uncertain", result: sql.NullInt64{Int64: 2, Valid: true}, uncertain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acquired, uncertain := legacyLockAcquisition(tc.result)
			if acquired != tc.acquired || uncertain != tc.uncertain {
				t.Fatalf("legacyLockAcquisition(%+v)=(%v,%v), want (%v,%v)", tc.result, acquired, uncertain, tc.acquired, tc.uncertain)
			}
		})
	}
}

func TestVerifiedLegacyReceiptAcceptsLineEndingVariantsAndRejectsBadChecksum(t *testing.T) {
	lf := []byte("CREATE TABLE sample (id INT);\n")
	crlf := []byte("CREATE TABLE sample (id INT);\r\n")
	migration := relational.Migration{Version: 1, Name: "0001_sample.up.sql", SHA256: componentsql.MigrationChecksum(lf)}
	for _, contents := range [][]byte{lf, crlf} {
		digest := sha256.Sum256(contents)
		receipt, err := verifiedLegacyReceipt("owner", "001_sample.sql", hex.EncodeToString(digest[:]), migration, lf)
		if err != nil || receipt.SHA256 != migration.SHA256 {
			t.Fatalf("valid line-ending variant rejected or not canonicalized: receipt=%+v err=%v", receipt, err)
		}
	}
	if _, err := verifiedLegacyReceipt("owner", "001_sample.sql", strings.Repeat("0", 64), migration, lf); err == nil {
		t.Fatal("invalid legacy checksum was accepted")
	}
}

func TestPlatformMySQLMigrationOwnershipExcludesAutomationServiceSchema(t *testing.T) {
	for _, owner := range mysqlOwnerModules() {
		if owner.module == "mysql-automation" {
			t.Fatal("the platform must not execute the automation service's schema")
		}
	}
	var platformBase string
	for _, name := range componentsql.MySQLPlatformBaseMigrationNames() {
		contents, err := componentsql.MySQLFamilyMigrationContents(os.Getenv("TP_MYSQL_TEST_FLAVOR"), name)
		if err != nil {
			t.Fatal(err)
		}
		platformBase += string(contents)
	}
	for _, table := range []string{"automation_scope_generation", "automation_command_guard"} {
		if !strings.Contains(platformBase, table) {
			t.Fatalf("platform-owned automation guard table %s must remain in platform-base", table)
		}
	}
}
