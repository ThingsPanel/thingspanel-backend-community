//go:build !notification_standalone

package notification

import "encore.dev/storage/sqldb"

// notificationDB is the notification service's only durable database. It is
// deliberately separate from the legacy database and other Encore services.
var notificationDB = sqldb.NewDatabase("notification", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})
