package sql

import (
	"embed"
	"strings"
)

//go:embed mysql/001_core_users_devices.sql mysql/002_device_identity.sql
var mysqlMigrationFiles embed.FS

// MySQLMigrationContents returns the raw bytes of one MySQL migration file (the ledger checksum input).
func MySQLMigrationContents(name string) ([]byte, error) {
	return mysqlMigrationFiles.ReadFile("mysql/" + name)
}

// MySQLMigrationStatements returns the statements of one MySQL migration file. Statements are
// separated by lines that contain only "-- @@"; the leading comment block is dropped.
func MySQLMigrationStatements(name string) ([]string, error) {
	contents, err := mysqlMigrationFiles.ReadFile("mysql/" + name)
	if err != nil {
		return nil, err
	}
	var statements []string
	for _, part := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n-- @@\n") {
		var kept []string
		for _, line := range strings.Split(part, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			kept = append(kept, line)
		}
		if statement := strings.TrimSpace(strings.Join(kept, "\n")); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements, nil
}

// MySQLMigrationNames lists the MySQL migration files in order.
func MySQLMigrationNames() []string {
	return []string{"001_core_users_devices.sql", "002_device_identity.sql"}
}
