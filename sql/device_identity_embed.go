package sql

import "embed"

//go:embed 31.sql
var deviceIdentityMigrationFiles embed.FS

func DeviceIdentityMigrations() ([]NativeBindingMigration, error) {
	contents, err := deviceIdentityMigrationFiles.ReadFile("31.sql")
	if err != nil {
		return nil, err
	}
	return []NativeBindingMigration{{FileName: "31.sql", Contents: contents}}, nil
}
