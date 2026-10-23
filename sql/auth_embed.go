package sql

import "embed"

//go:embed auth/*.sql
var authMigrationFiles embed.FS

// AuthMigrations is owned by the community migration runner, never Encore sqldb.
func AuthMigrations() ([]NativeBindingMigration, error) {
	entries, err := authMigrationFiles.ReadDir("auth")
	if err != nil {
		return nil, err
	}
	var out []NativeBindingMigration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := "auth/" + entry.Name()
		data, err := authMigrationFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		out = append(out, NativeBindingMigration{FileName: name, Contents: data})
	}
	return out, nil
}
