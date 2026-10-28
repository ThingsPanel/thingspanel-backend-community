package northbound

import "encore.dev/storage/sqldb"

// The northbound ledger is its own database. It is deliberately not the
// device_metadata database and not the Community main database: the Community
// runtime owns source receipts, this service owns configuration and delivery
// accounting.
var db = sqldb.NewDatabase("northbound", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})
