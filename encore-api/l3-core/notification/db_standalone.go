//go:build notification_standalone

package notification

import "database/sql"

// notificationDB is a compile-time stub for the isolated standalone package tests.
// Tests connect through an explicitly approved fixture DB and
// never starts Encore service initialization or opens the legacy service DB.
type notificationDBAdapter struct{}

func (notificationDBAdapter) Stdlib() *sql.DB { return nil }

var notificationDB notificationDBAdapter
