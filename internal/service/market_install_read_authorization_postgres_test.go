package service

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/pkg/global"
)

func TestMarketInstallationReadAuthorizationPostgres(t *testing.T) {
	dsn := os.Getenv("TP_A08_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("TP_A08_STORE_TEST_DSN is required for actual PostgreSQL authorization verification")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid isolated PostgreSQL configuration")
	}
	owner := stdlib.OpenDB(*config)
	defer owner.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var database, version string
	if err := owner.QueryRowContext(ctx, `SELECT current_database(),current_setting('server_version')`).Scan(&database, &version); err != nil {
		t.Fatal("isolated PostgreSQL unavailable")
	}
	if !strings.HasPrefix(database, "tp_takeover_") && !strings.HasPrefix(database, "tp_a08_") {
		t.Fatal("refusing non-fixture PostgreSQL database")
	}
	schema := "tp_a08_permission_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := owner.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := owner.ExecContext(cleanup, `DROP SCHEMA "`+schema+`" CASCADE`); err != nil {
			t.Error(err)
		}
	}()
	selected, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid isolated PostgreSQL configuration")
	}
	if selected.RuntimeParams == nil {
		selected.RuntimeParams = map[string]string{}
	}
	selected.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*selected)
	defer pool.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated PostgreSQL GORM connection")
	}
	db, enforcer := installLookupPermissionFixture(t, db)
	// A PostgreSQL read-only transaction proves the successful permission check
	// does not issue/update grants, users, operations, or any other SQL object.
	readOnly := db.Begin(&sql.TxOptions{ReadOnly: true})
	if readOnly.Error != nil {
		t.Fatal(readOnly.Error)
	}
	global.DB = readOnly
	decision, err := AuthorizeCurrentMarketInstallationManagement(ctx, model.MarketInstallationReadIdentity{ProtocolVersion: 1, TenantID: "original-tenant", SubjectID: "original-subject"})
	rollbackErr := readOnly.Rollback().Error
	global.DB = db
	if err != nil || rollbackErr != nil || decision == nil || !decision.Authorized {
		t.Fatalf("read-only permission check: %v rollback=%v", err, rollbackErr)
	}
	exerciseLookupPermissionRevocation(t, db, enforcer)
	t.Logf("actual PostgreSQL %s; database=%s; dedicated permission schema=%s", version, database, schema)
}
