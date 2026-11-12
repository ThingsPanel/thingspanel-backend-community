package initialize

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	global "project/pkg/global"
)

// TestPlatformSchemaStartupOnPostgres runs the backend's real schema startup
// (CheckVersion + every component initializer and verifier) against the
// database named by TP_R4S_TEST_DSN. It is used twice by the R4S drill: once on
// a fresh database (apply) and once on the same database after pg_dump -Fc /
// pg_restore (verify only: every ledger entry is present, so only verifiers run).
// Skipped unless the DSN points at a loopback database whose name starts with r4s_.
func TestPlatformSchemaStartupOnPostgres(t *testing.T) {
	dsn := os.Getenv("TP_R4S_TEST_DSN")
	if dsn == "" {
		t.Skip("TP_R4S_TEST_DSN is required (loopback PostgreSQL database named r4s_*)")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid DSN")
	}
	if ip := net.ParseIP(cfg.Host); ip == nil || !ip.IsLoopback() || !strings.HasPrefix(cfg.Database, "r4s_") {
		t.Fatal("refusing non-loopback or non-r4s_ database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := os.Chdir(".."); err != nil { // CheckVersion reads sql/N.sql relative to the repo root
		t.Fatal(err)
	}
	t.Setenv("TP_PLATFORMCONFIG_OWNER", "encore")
	prev := global.DB
	t.Cleanup(func() { global.DB = prev })
	if err := initializePlatformSchema(context.Background(), db, relational.Postgres); err != nil {
		t.Fatalf("platform schema startup failed: %v", err)
	}
}
