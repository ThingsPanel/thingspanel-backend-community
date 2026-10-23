package devicemetadata

import "encore.dev/storage/sqldb"

var db = sqldb.NewDatabase("device_metadata", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})
