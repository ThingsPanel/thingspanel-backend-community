package app

import "project/internal/api"

// NewNotificationMemberReaderFromEnv builds the private source-member and
// source-tenant read handler. Missing or malformed settings leave it disabled;
// the routes then return 404. Router wiring must place both routes before
// JWT/API-key and operation-log middleware.
func NewNotificationMemberReaderFromEnv() *api.NotificationMemberReaderApi {
	return api.NewNotificationMemberReaderFromEnv()
}
