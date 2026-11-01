package sql

import "embed"

//go:embed 30.sql
var marketInstallMigrationFiles embed.FS

// MarketInstallMigrations extends the existing installation schema owned by
// the community database. It is not part of Encore's sqldb migration stream.
func MarketInstallMigrations() ([]NativeBindingMigration, error) {
	contents, err := marketInstallMigrationFiles.ReadFile("30.sql")
	if err != nil {
		return nil, err
	}
	return []NativeBindingMigration{{FileName: "30.sql", Contents: contents}}, nil
}
