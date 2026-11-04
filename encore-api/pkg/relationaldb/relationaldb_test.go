package relationaldb

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
)

func reset(t *testing.T, driver string) {
	t.Helper()
	t.Setenv("TP_RELATIONAL_DRIVER", driver)
	mu.Lock()
	resolved = false
	registry = relational.NewRegistry()
	mu.Unlock()
}

func TestUnknownDriverFailsAndNothingIsOpened(t *testing.T) {
	reset(t, "oracle")
	if _, err := open(context.Background(), "device_metadata", func() (*sql.DB, error) { t.Fatal("pool requested"); return nil, nil }); err == nil {
		t.Fatal("accepted relational.driver=oracle")
	}
}

func TestMySQLModeNeverTouchesTheEncoreDeclaration(t *testing.T) {
	host := os.Getenv("TP_MYSQL_TEST_HOST")
	if host == "" {
		t.Skip("TP_MYSQL_TEST_* not set")
	}
	reset(t, "mysql")
	port, _ := strconv.Atoi(os.Getenv("TP_MYSQL_TEST_PORT"))
	t.Setenv("TP_RELATIONAL_MYSQL_DEVICE_METADATA_HOST", host)
	t.Setenv("TP_RELATIONAL_MYSQL_DEVICE_METADATA_PORT", strconv.Itoa(port))
	t.Setenv("TP_RELATIONAL_MYSQL_DEVICE_METADATA_USERNAME", os.Getenv("TP_MYSQL_TEST_USER"))
	t.Setenv("TP_RELATIONAL_MYSQL_DEVICE_METADATA_PASSWORD", os.Getenv("TP_MYSQL_TEST_PASSWORD"))
	t.Setenv("TP_RELATIONAL_MYSQL_DEVICE_METADATA_DATABASE", os.Getenv("TP_MYSQL_TEST_DB"))
	db, err := open(context.Background(), "device_metadata", func() (*sql.DB, error) { t.Fatal("PostgreSQL pool requested in MySQL mode"); return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	if d := relational.MustDialectOf(db); d != relational.MySQL {
		t.Fatalf("dialect %s", d)
	}
	var one int
	if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil || one != 1 {
		t.Fatalf("read: %v %d", err, one)
	}
	if _, err := open(context.Background(), "device_metadata", nil); err == nil {
		t.Fatal("second Open of the same resource was accepted")
	}
	if got, ok := Get("device_metadata"); !ok || got != db {
		t.Fatal("Get does not return the opened handle")
	}
	if err := Close("device_metadata"); err != nil {
		t.Fatal(err)
	}
	if err := Close("device_metadata"); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, ok := Get("device_metadata"); ok {
		t.Fatal("handle still registered after Close")
	}
}

func TestPostgresModeAdoptsAndDoesNotCloseTheEncorePool(t *testing.T) {
	dsn := os.Getenv("TEST_PG_SCHEMA_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_SCHEMA_DSN not set")
	}
	reset(t, "") // empty = postgres
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db, err := open(context.Background(), "device_metadata", func() (*sql.DB, error) { return pool, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := Close("device_metadata"); err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(); err != nil {
		t.Fatalf("Close closed a pool Encore owns: %v", err)
	}
	var one int
	if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil || one != 1 {
		t.Fatalf("read: %v %d", err, one)
	}
}
