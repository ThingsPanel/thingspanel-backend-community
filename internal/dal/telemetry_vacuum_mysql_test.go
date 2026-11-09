package dal

import (
	"database/sql"
	"testing"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestVacuumTelemetryDataSkipsMySQL(t *testing.T) {
	pool, err := sql.Open("mysql", "user@tcp(127.0.0.1:1)/unused")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := vacuumTelemetryData(db); err != nil {
		t.Fatalf("MySQL must skip PostgreSQL VACUUM FULL: %v", err)
	}
}
