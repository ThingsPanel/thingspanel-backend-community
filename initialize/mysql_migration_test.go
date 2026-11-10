package initialize

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	componentsql "project/sql"
)

func openMySQLRunner(t *testing.T) *gorm.DB {
	t.Helper()
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if host == "" || port == "" || os.Getenv("TP_MYSQL_TEST_DB") == "" {
		t.Skip("TP_MYSQL_TEST_HOST/PORT/DB must identify an isolated, disposable MySQL database")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	db := connectMySQLTestDatabase(t, &DbConfig{Host: host, Port: portNumber, Username: os.Getenv("TP_MYSQL_TEST_USER"), Password: os.Getenv("TP_MYSQL_TEST_PASSWORD"),
		DbName: os.Getenv("TP_MYSQL_TEST_DB"), TimeZone: "UTC", LogLevel: 1, IdleConns: 8, OpenConns: 16})
	for _, statement := range []string{
		"DROP TABLE IF EXISTS devices", "DROP TABLE IF EXISTS users", "DROP TABLE IF EXISTS device_id_reservations",
		"DROP TABLE IF EXISTS runner_probe", "DROP TABLE IF EXISTS " + mysqlLedgerTable, "DROP TABLE IF EXISTS " + relational.LedgerTable,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
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

func TestMySQLRunnerConcurrentStartupImportsAndAppliesOnce(t *testing.T) {
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
		t.Fatalf("expected two compatibility receipts, got %d", n)
	}
	if n := scalar(t, db, "SELECT COUNT(*) FROM "+relational.LedgerTable+" WHERE resource = ?", mysqlCoreModule); n != 2 {
		t.Fatalf("expected two relational receipts, got %d", n)
	}
	if err := db.Exec("INSERT INTO users (id, phone_number, email, password) VALUES ('u', '1', 'a@x.com', 'p')").Error; err != nil {
		t.Fatal(err)
	}
	// Simulate a database created by the previous runner: the old receipts and
	// schema survive, while the new relational ledger does not exist yet.
	if err := db.Exec("DROP TABLE " + relational.LedgerTable).Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyMySQLCoreSchema(context.Background(), db); err != nil {
		t.Fatalf("startup must import old receipts before checking pending SQL: %v", err)
	}
	if scalar(t, db, "SELECT COUNT(*) FROM users") != 1 {
		t.Fatal("startup replay changed existing user data")
	}
	if err := db.Exec("INSERT INTO devices (id, voucher, tenant_id, device_number) VALUES ('d', 'v', 't', 'n')").Error; err == nil {
		t.Fatal("the identity fence must remain live after startup")
	}
}

func TestMySQLRunnerRejectsLegacyChecksumDriftAndMissingSchema(t *testing.T) {
	db := openMySQLRunner(t)
	if err := ApplyMySQLCoreSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE "+mysqlLedgerTable+" SET checksum = ? WHERE module_name = ? AND file_name = ?", strings.Repeat("0", 64), mysqlCoreModule, "002_device_identity.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyMySQLCoreSchema(context.Background(), db); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("legacy checksum drift must fail closed, got %v", err)
	}
	contents, err := componentsql.MySQLFamilyMigrationContents(os.Getenv("TP_MYSQL_TEST_FLAVOR"), "002_device_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE "+mysqlLedgerTable+" SET checksum = ? WHERE module_name = ? AND file_name = ?", componentsql.MigrationChecksum(contents), mysqlCoreModule, "002_device_identity.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TRIGGER devices_identity_fence_delete").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyMySQLCoreSchema(context.Background(), db); err == nil || !strings.Contains(err.Error(), "devices_identity_fence_delete") {
		t.Fatalf("missing schema object must fail verification, got %v", err)
	}
}

func TestMySQLRunnerSharesLegacyGlobalLock(t *testing.T) {
	db := openMySQLRunner(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	holder, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	var got int
	if err := holder.QueryRowContext(context.Background(), "SELECT GET_LOCK(?, 5)", mysqlGlobalLockName).Scan(&got); err != nil || got != 1 {
		t.Fatalf("holder lock: %d %v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := ApplyMySQLPlatformSchema(ctx, db); err == nil {
		t.Fatal("platform migration must wait for an old-version executor holding the shared global lock")
	}
}

func TestMysqlConnectCountsMatchedRowsLikePostgres(t *testing.T) {
	db := openMySQLRunner(t)
	if err := db.Exec("CREATE TABLE IF NOT EXISTS runner_probe (id INT PRIMARY KEY, note VARCHAR(10))").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO runner_probe VALUES (1, 'x')").Error; err != nil {
		t.Fatal(err)
	}
	result := db.Exec("UPDATE runner_probe SET note = 'x' WHERE id = 1")
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("matched row count: affected=%d err=%v", result.RowsAffected, result.Error)
	}
}
