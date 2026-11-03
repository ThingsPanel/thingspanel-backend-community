package sql

import (
	"embed"
	"strings"
)

//go:embed mysql/001_core_users_devices.sql mysql/002_device_identity.sql mysql/003_auth.sql mysql/004_app_push_devices.sql mysql/005_account_support.sql mysql/006_device_status_history.sql mysql/007_service_access.sql mysql/008_history_intent.sql mysql/009_platformconfig_dictionary.sql
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

// MySQLMigrationNames lists the MySQL core migration files in order (users, devices, identity fence).
func MySQLMigrationNames() []string {
	return []string{"001_core_users_devices.sql", "002_device_identity.sql"}
}

// MySQLPlatformConfigMigrationNames lists the platform dictionary migration files; they stand alone.
func MySQLPlatformConfigMigrationNames() []string {
	return []string{"009_platformconfig_dictionary.sql"}
}

// MySQLHistoryIntentMigrationNames lists the telemetry ingest migration files; they stand alone.
func MySQLHistoryIntentMigrationNames() []string {
	return []string{"008_history_intent.sql"}
}

// MySQLServiceAccessMigrationNames lists the service access migration files; they stand alone.
func MySQLServiceAccessMigrationNames() []string {
	return []string{"007_service_access.sql"}
}

// MySQLDeviceStatusMigrationNames lists the device status migration files; they require the core schema.
func MySQLDeviceStatusMigrationNames() []string {
	return []string{"006_device_status_history.sql"}
}

// MySQLAuthMigrationNames lists the authentication migration files; they require the core schema.
func MySQLAuthMigrationNames() []string {
	return []string{"003_auth.sql", "004_app_push_devices.sql", "005_account_support.sql"}
}
