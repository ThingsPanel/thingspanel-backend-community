package sql

import "embed"

//go:embed 30.sql 32.sql
var marketInstallMigrationFiles embed.FS

// MarketInstallMigrations extends the existing installation schema owned by
// the community database. It is not part of Encore's sqldb migration stream.
func MarketInstallMigrations() ([]NativeBindingMigration, error) {
	var migrations []NativeBindingMigration
	for _, name := range []string{"30.sql", "32.sql"} {
		contents, err := marketInstallMigrationFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, NativeBindingMigration{FileName: name, Contents: contents})
	}
	return migrations, nil
}
