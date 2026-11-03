package devicemetadata

import (
	"fmt"
	"os"
	"strings"

	"encore.app/pkg/dbx"
	"encore.app/pkg/sqldialect"
	"encore.dev/storage/sqldb"
)

// encoreDB is the PostgreSQL database Encore provisions. It must be declared at package level for Encore's tooling.
var encoreDB = sqldb.NewDatabase("device_metadata", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})

// db is what every store uses. It is Encore's database unless UseDatabase installs another (the MySQL profile).
var db dbx.DB = dbx.Encore(encoreDB)

// UseDatabase replaces the database the service reads and writes. Call it once at start-up, before serving.
func UseDatabase(d dbx.DB) { db = d }

// configureDatabaseFromEnv selects the MySQL profile when TP_DEVICE_METADATA_DB_DRIVER=mysql: the service then uses the
// MySQL database named by TP_DEVICE_METADATA_MYSQL_DSN (a go-sql-driver DSN with parseTime=true&clientFoundRows=true)
// through the SQL translator, and never touches PostgreSQL. Any other value keeps Encore's PostgreSQL database.
func configureDatabaseFromEnv() error {
	switch driver := strings.TrimSpace(os.Getenv("TP_DEVICE_METADATA_DB_DRIVER")); driver {
	case "", "postgres":
		return nil
	case "mysql":
		dsn := strings.TrimSpace(os.Getenv("TP_DEVICE_METADATA_MYSQL_DSN"))
		if dsn == "" {
			return fmt.Errorf("TP_DEVICE_METADATA_DB_DRIVER=mysql requires TP_DEVICE_METADATA_MYSQL_DSN")
		}
		pool, err := sqldialect.OpenMySQL(dsn)
		if err != nil {
			return err
		}
		if err := pool.Ping(); err != nil {
			_ = pool.Close()
			return fmt.Errorf("device metadata MySQL is unreachable: %w", err)
		}
		UseDatabase(dbx.SQL(pool, "mysql"))
		return nil
	default:
		return fmt.Errorf("unsupported TP_DEVICE_METADATA_DB_DRIVER %q", driver)
	}
}
