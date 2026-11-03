package initialize

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func openMySQLRunner(t *testing.T) *gorm.DB {
	t.Helper()
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if host == "" || port == "" {
		t.Skip("TP_MYSQL_TEST_* required")
	}
	portNumber, _ := strconv.Atoi(port)
	db, err := MysqlConnect(&DbConfig{Host: host, Port: portNumber, Username: os.Getenv("TP_MYSQL_TEST_USER"), Password: os.Getenv("TP_MYSQL_TEST_PASSWORD"),
		DbName: os.Getenv("TP_MYSQL_TEST_DB"), TimeZone: "UTC", LogLevel: 1, IdleConns: 8, OpenConns: 16})
	if err != nil {
		t.Fatal(err)
	}
	reset := func() {
		for _, statement := range []string{
			"DROP TABLE IF EXISTS devices", "DROP TABLE IF EXISTS users", "DROP TABLE IF EXISTS device_id_reservations",
			"DROP TABLE IF EXISTS " + mysqlLedgerTable, "DROP TABLE IF EXISTS runner_probe",
		} {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	reset()
	t.Cleanup(func() {
		reset()
		if p, e := db.DB(); e == nil {
			_ = p.Close()
		}
	})
	return db
}

func scalar(t *testing.T, db *gorm.DB, query string, args ...interface{}) int {
	t.Helper()
	var n int
	if err := db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMySQLRunnerConcurrentStartupAppliesOnceAndIsRepeatable(t *testing.T) {
	db := openMySQLRunner(t)
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- ApplyMySQLCoreSchema(context.Background(), db) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent start-up: %v", err)
		}
	}
	if n := scalar(t, db, "SELECT COUNT(*) FROM "+mysqlLedgerTable+" WHERE module_name = ?", mysqlCoreModule); n != 2 {
		t.Fatalf("expected one ledger row per file (2), got %d", n)
	}
	// Data written after the first start-up survives a later one.
	if err := db.Exec("INSERT INTO users (id, phone_number, email, password) VALUES ('u', '1', 'a@x.com', 'p')").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyMySQLCoreSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if scalar(t, db, "SELECT COUNT(*) FROM users") != 1 {
		t.Fatal("a repeated start-up must not touch existing data")
	}
	// The fence is live after the runner: a device without a reservation is rejected.
	if err := db.Exec("INSERT INTO devices (id, voucher, tenant_id, device_number) VALUES ('d', 'v', 't', 'n')").Error; err == nil {
		t.Fatal("the identity fence must be active after start-up")
	}
}

func TestMySQLRunnerWaitsForTheOwnerLockAndTimesOut(t *testing.T) {
	db := openMySQLRunner(t)
	pool, _ := db.DB()
	holder, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	var got int
	if err := holder.QueryRowContext(context.Background(), "SELECT GET_LOCK(?, 5)", mysqlLockNamePrefix+mysqlCoreModule).Scan(&got); err != nil || got != 1 {
		t.Fatalf("holder lock: %d %v", got, err)
	}
	started := time.Now()
	err = applyMySQLMigrations(context.Background(), db, mysqlMigration{module: mysqlCoreModule, description: "probe", lockWait: time.Second,
		files:  []mysqlMigrationFile{{name: "a.sql", contents: []byte("x"), statements: []string{"SELECT 1"}}},
		verify: func(context.Context, *sql.Conn) error { return nil }})
	if err == nil || !strings.Contains(err.Error(), "not obtained") || time.Since(started) > 5*time.Second {
		t.Fatalf("a held owner lock must time out with an error, got %v after %s", err, time.Since(started))
	}
	var released int
	_ = holder.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", mysqlLockNamePrefix+mysqlCoreModule).Scan(&released)
	if err := ApplyMySQLCoreSchema(context.Background(), db); err != nil {
		t.Fatalf("after the holder releases, start-up must succeed: %v", err)
	}
}

func TestMySQLRunnerDetectsChecksumDriftAndMissingSchema(t *testing.T) {
	db := openMySQLRunner(t)
	if err := ApplyMySQLCoreSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE "+mysqlLedgerTable+" SET checksum = ? WHERE module_name = ? AND file_name = ?", strings.Repeat("0", 64), mysqlCoreModule, "002_device_identity.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyMySQLCoreSchema(context.Background(), db); err == nil || !strings.Contains(err.Error(), "checksum drift") {
		t.Fatalf("a changed checksum must be refused, got %v", err)
	}
	// Drop the drifted row so the runner re-records the idempotent file, then remove a trigger
	// behind the ledger's back: the ledger says applied, verification must still notice.
	if err := db.Exec("DELETE FROM " + mysqlLedgerTable + " WHERE file_name = '002_device_identity.sql'").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyMySQLCoreSchema(context.Background(), db); err != nil {
		t.Fatalf("re-recording an idempotent file must succeed: %v", err)
	}
	if err := db.Exec("DROP TRIGGER devices_identity_fence_delete").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyMySQLCoreSchema(context.Background(), db); err == nil || !strings.Contains(err.Error(), "devices_identity_fence_delete") {
		t.Fatalf("a missing fence trigger must fail verification even though the ledger is complete, got %v", err)
	}
}

func TestMySQLRunnerInterruptedFileIsNotRecordedAndCanBeRepaired(t *testing.T) {
	db := openMySQLRunner(t)
	verify := func(context.Context, *sql.Conn) error { return nil }
	broken := mysqlMigration{module: "probe", description: "probe", verify: verify, files: []mysqlMigrationFile{{
		name: "001.sql", contents: []byte("broken"),
		statements: []string{"CREATE TABLE IF NOT EXISTS runner_probe (id INT PRIMARY KEY)", "THIS IS NOT SQL"},
	}}}
	if err := applyMySQLMigrations(context.Background(), db, broken); err == nil || !strings.Contains(err.Error(), "statement 2") {
		t.Fatalf("expected the second statement to fail, got %v", err)
	}
	if scalar(t, db, "SELECT COUNT(*) FROM "+mysqlLedgerTable+" WHERE module_name = 'probe'") != 0 {
		t.Fatal("an interrupted file must not be recorded as applied")
	}
	if scalar(t, db, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'runner_probe'") != 1 {
		t.Fatal("the first statement's effect is expected to remain (DDL cannot be rolled back); files must be idempotent")
	}
	fixed := broken
	fixed.files = []mysqlMigrationFile{{name: "001.sql", contents: []byte("fixed"),
		statements: []string{"CREATE TABLE IF NOT EXISTS runner_probe (id INT PRIMARY KEY)", "ALTER TABLE runner_probe ADD COLUMN note VARCHAR(10) NULL"}}}
	if err := applyMySQLMigrations(context.Background(), db, fixed); err != nil {
		t.Fatalf("a corrected, idempotent file must apply: %v", err)
	}
	if scalar(t, db, "SELECT COUNT(*) FROM "+mysqlLedgerTable+" WHERE module_name = 'probe'") != 1 {
		t.Fatal("the corrected file must be recorded exactly once")
	}
	if err := applyMySQLMigrations(context.Background(), db, mysqlMigration{description: "x", verify: verify}); err == nil {
		t.Fatal("a registration without a module must be rejected")
	}
	if err := applyMySQLMigrations(context.Background(), nil, fixed); err == nil {
		t.Fatal("a nil database must be rejected")
	}
}
