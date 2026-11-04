package initialize

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	global "project/pkg/global"
)

func TestRelationalInitMySQLImportsOldReceiptsAndDoesNotReplayAppliedDDL(t *testing.T) {
	host, port, user, database := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT"), os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_DB")
	if host == "" || port == "" || user == "" || database == "" {
		t.Skip("requires an exclusive disposable MySQL test database")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	previousGlobal := global.DB
	if err := CloseRelationalResources(); err != nil {
		t.Fatal(err)
	}
	setViper(t, map[string]interface{}{
		"relational.driver":                     "mysql",
		"relational.mysql.platform.host":        host,
		"relational.mysql.platform.port":        portNumber,
		"relational.mysql.platform.database":    database,
		"relational.mysql.platform.username":    user,
		"relational.mysql.platform.password":    os.Getenv("TP_MYSQL_TEST_PASSWORD"),
		"relational.mysql.platform.time_zone":   "UTC",
		"relational.mysql.platform.log_level":   1,
		"relational.postgres.platform.host":     "127.0.0.1",
		"relational.postgres.platform.port":     1,
		"relational.postgres.platform.database": "unreachable",
		"relational.postgres.platform.username": "unused",
		"relational.postgres.platform.password": "unused",
	})
	t.Cleanup(func() {
		_ = CloseRelationalResources()
		global.DB = previousGlobal
	})

	db, err := RelationalInit()
	if err != nil {
		t.Fatalf("MySQL startup with PostgreSQL unreachable: %v", err)
	}
	if db.Dialector.Name() != "mysql" {
		t.Fatalf("dialector=%s, want mysql", db.Dialector.Name())
	}
	var modules int
	if err := db.Raw("SELECT COUNT(DISTINCT module_name) FROM " + mysqlLedgerTable).Scan(&modules).Error; err != nil || modules != 8 {
		t.Fatalf("legacy module receipts=%d err=%v", modules, err)
	}
	// Recreate the old-runner state: its receipts remain, but the relational
	// ledger is absent. Removing one schema object makes any accidental replay
	// observable because it would repair that object before verification.
	if err := db.Exec("DROP TABLE " + relational.LedgerTable).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TRIGGER devices_identity_fence_delete").Error; err != nil {
		t.Fatal(err)
	}
	if err := CloseRelationalResources(); err != nil {
		t.Fatal(err)
	}
	global.DB = nil
	if _, err := RelationalInit(); err == nil || !strings.Contains(err.Error(), "devices_identity_fence_delete") {
		t.Fatalf("old receipts must be imported and prevent DDL replay; missing trigger should be reported, got %v", err)
	}
	if global.DB != nil {
		t.Fatal("failed startup must not leave a live global database handle")
	}
}
