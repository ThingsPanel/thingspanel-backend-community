//go:build notification_cli

package notification

import "database/sql"

// notificationDB is a compile-time stub for the standalone operator binary.
// The command connects through the explicitly selected physical DB itself and
// never starts Encore service initialization or opens the legacy service DB.
type notificationDBAdapter struct{}

func (notificationDBAdapter) Stdlib() *sql.DB { return nil }

var notificationDB notificationDBAdapter
