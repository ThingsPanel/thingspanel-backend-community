package dal

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The default PostgreSQL handle must never be asked for a MySQL flavor: relational.FlavorOf errors for non-MySQL
// dialectors, which once made topic-mapping create/update fail on PostgreSQL. The pool is lazy; no server is needed.
// The MariaDB branch is covered by TestMariaDBTopicMappingUniqueKeyRepository against a real MariaDB.
func TestIsMariaDBHandlePostgresIsFalse(t *testing.T) {
	pool, err := sql.Open("pgx", "postgres://u:p@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := isMariaDBHandle(db)
	if err != nil {
		t.Fatalf("isMariaDBHandle on PostgreSQL returned error: %v", err)
	}
	if got {
		t.Fatal("PostgreSQL handle reported as MariaDB")
	}
}
