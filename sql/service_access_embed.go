package sql

import "embed"

//go:embed service_access/001_create_receipts.sql
var serviceAccessMigrationFiles embed.FS

func ServiceAccessMigrations() ([]NativeBindingMigration, error) {
	const name = "service_access/001_create_receipts.sql"
	contents, err := serviceAccessMigrationFiles.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return []NativeBindingMigration{{FileName: name, Contents: contents}}, nil
}
