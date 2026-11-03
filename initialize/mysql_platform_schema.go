package initialize

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"

	"gorm.io/gorm"

	componentsql "project/sql"
)

// mysqlOwnerModule is one owner's ledger module and its migration files. The names are the ledger module names
// other code reads back (platformconfig checks "platformconfig-dictionary"), so they must not change.
type mysqlOwnerModule struct {
	module, description string
	files               []string
}

func mysqlOwnerModules() []mysqlOwnerModule {
	return []mysqlOwnerModule{
		{"mysql-device-status", "MySQL device status history", componentsql.MySQLDeviceStatusMigrationNames()},
		{"mysql-service-access", "MySQL service access", componentsql.MySQLServiceAccessMigrationNames()},
		{"mysql-history-intent", "MySQL telemetry history intent", componentsql.MySQLHistoryIntentMigrationNames()},
		{"platformconfig-dictionary", "MySQL platform dictionary", componentsql.MySQLPlatformConfigMigrationNames()},
		{"mysql-automation", "MySQL automation", componentsql.MySQLAutomationMigrationNames()},
		{"mysql-platform-base", "MySQL platform base", componentsql.MySQLPlatformBaseMigrationNames()},
	}
}

var createdTable = regexp.MustCompile("(?i)^\\s*CREATE TABLE IF NOT EXISTS\\s+`?(\\w+)`?")

// ApplyMySQLPlatformSchema brings an empty or older MySQL database to the current platform schema: the core and
// auth owners first, then every other owner in order. Each owner has its own ledger module and named lock, a file
// is recorded only after all its statements ran, and every owner's tables are verified to exist afterwards.
func ApplyMySQLPlatformSchema(ctx context.Context, db *gorm.DB) error {
	if err := ApplyMySQLCoreSchema(ctx, db); err != nil {
		return err
	}
	if err := ApplyMySQLAuthSchema(ctx, db); err != nil {
		return err
	}
	for _, owner := range mysqlOwnerModules() {
		var files []mysqlMigrationFile
		var tables []string
		for _, name := range owner.files {
			contents, err := componentsql.MySQLMigrationContents(name)
			if err != nil {
				return err
			}
			statements, err := componentsql.MySQLMigrationStatements(name)
			if err != nil {
				return err
			}
			for _, statement := range statements {
				if m := createdTable.FindStringSubmatch(statement); m != nil {
					tables = append(tables, m[1])
				}
			}
			files = append(files, mysqlMigrationFile{name: name, contents: contents, statements: statements})
		}
		expected := tables
		description := owner.description
		err := applyMySQLMigrations(ctx, db, mysqlMigration{module: owner.module, description: description, files: files,
			verify: func(ctx context.Context, conn *sql.Conn) error {
				return verifyMySQLTables(ctx, conn, description, expected)
			}})
		if err != nil {
			return err
		}
	}
	return nil
}

func verifyMySQLTables(ctx context.Context, conn *sql.Conn, description string, tables []string) error {
	for _, table := range tables {
		var n int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", table).Scan(&n); err != nil || n != 1 {
			return fmt.Errorf("%s schema is incomplete: table %s is missing", description, table)
		}
	}
	return nil
}
