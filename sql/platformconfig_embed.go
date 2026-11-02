package sql

import "embed"

//go:embed platformconfig/001_adopt_dictionary.sql
var platformConfigMigrationFiles embed.FS

func PlatformConfigMigrations() ([]NativeBindingMigration, error) {
	const name = "platformconfig/001_adopt_dictionary.sql"
	contents, err := platformConfigMigrationFiles.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return []NativeBindingMigration{{FileName: name, Contents: contents}}, nil
}
