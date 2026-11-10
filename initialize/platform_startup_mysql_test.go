package initialize

import (
	"errors"
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
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })
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
		"relational.mysql.flavor":               os.Getenv("TP_MYSQL_TEST_FLAVOR"),
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
	previousCasbinInit := mysqlCasbinInit
	t.Cleanup(func() { mysqlCasbinInit = previousCasbinInit })

	db, err := RelationalInit()
	if err != nil {
		t.Fatalf("MySQL startup with PostgreSQL unreachable: %v", err)
	}
	if db.Dialector.Name() != "mysql" {
		t.Fatalf("dialector=%s, want mysql", db.Dialector.Name())
	}
	var modules int
	wantModules := len(mysqlOwnerModules()) + 2 // the core and auth owners are registered separately
	if err := db.Raw("SELECT COUNT(DISTINCT module_name) FROM " + mysqlLedgerTable).Scan(&modules).Error; err != nil || modules != wantModules {
		t.Fatalf("legacy module receipts=%d want=%d err=%v", modules, wantModules, err)
	}
	if err := CloseRelationalResources(); err != nil {
		t.Fatal(err)
	}
	var failedPoolPing func() error
	mysqlCasbinInit = func() error {
		if pool, poolErr := global.DB.DB(); poolErr == nil {
			failedPoolPing = pool.Ping
		}
		return errors.New("injected MySQL auth adapter failure")
	}
	if _, err := RelationalInit(); err == nil || !strings.Contains(err.Error(), "injected MySQL auth adapter failure") {
		t.Fatalf("MySQL authorization initialization error must abort startup, got %v", err)
	}
	mysqlCasbinInit = previousCasbinInit
	if global.DB != nil {
		t.Fatal("failed MySQL authorization initialization left global.DB published")
	}
	if _, ok := platformRegistry.Get("platform"); ok {
		t.Fatal("failed MySQL authorization initialization left the pool in the registry")
	}
	if failedPoolPing == nil || failedPoolPing() == nil {
		t.Fatal("failed MySQL authorization initialization did not close its pool")
	}
	db, err = RelationalInit()
	if err != nil {
		t.Fatalf("MySQL startup after failed auth initialization: %v", err)
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
