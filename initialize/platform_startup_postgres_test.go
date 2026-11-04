package initialize

import (
	"os"
	"testing"

	global "project/pkg/global"
)

// This opt-in check exercises the real platform startup path against a
// dedicated PostgreSQL database preloaded with the community base schema.
func TestRelationalInitPostgresOwnsConfiguredPool(t *testing.T) {
	host, port, database, username := os.Getenv("TP_TEST_PLATFORM_HOST"), os.Getenv("TP_TEST_PLATFORM_PORT"), os.Getenv("TP_TEST_PLATFORM_DATABASE"), os.Getenv("TP_TEST_PLATFORM_USERNAME")
	if host == "" || port == "" || database == "" || username == "" {
		t.Skip("TP_TEST_PLATFORM_HOST/PORT/DATABASE/USERNAME must identify a disposable PostgreSQL platform fixture")
	}
	previousGlobal := global.DB
	if err := CloseRelationalResources(); err != nil {
		t.Fatal(err)
	}
	setViper(t, map[string]interface{}{
		"relational.driver":                     "postgres",
		"relational.postgres.platform.host":     host,
		"relational.postgres.platform.port":     port,
		"relational.postgres.platform.database": database,
		"relational.postgres.platform.username": username,
		"relational.postgres.platform.password": os.Getenv("TP_TEST_PLATFORM_PASSWORD"),
		"relational.postgres.platform.time_zone": "UTC",
		"relational.postgres.platform.log_level": 1,
	})
	t.Cleanup(func() {
		_ = CloseRelationalResources()
		global.DB = previousGlobal
	})
	db, err := RelationalInit()
	if err != nil {
		t.Fatalf("RelationalInit PostgreSQL startup: %v", err)
	}
	if db.Dialector.Name() != "postgres" || global.DB != db {
		t.Fatalf("platform DB not selected/published: dialect=%s global_match=%t", db.Dialector.Name(), global.DB == db)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseRelationalResources(); err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(); err == nil {
		t.Fatal("application shutdown must close the platform pool")
	}
}
