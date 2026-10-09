package api

import (
	"os"
	"strings"
)

const (
	notificationMemberReaderTokenEnv       = "NOTIFICATION_MEMBER_READER_TOKEN"
	notificationMemberReaderDeploymentsEnv = "NOTIFICATION_MEMBER_READER_DEPLOYMENTS"
	notificationMemberReaderTenantsEnv     = "NOTIFICATION_MEMBER_READER_TENANT_IDS"
)

// NewNotificationMemberReaderFromEnv reads only the private reader settings.
// Missing or malformed configuration leaves the handler disabled.
func NewNotificationMemberReaderFromEnv() *NotificationMemberReaderApi {
	return NewNotificationMemberReader(NotificationMemberReaderConfig{
		Token:                os.Getenv(notificationMemberReaderTokenEnv),
		AllowedDeploymentIDs: parseNotificationReaderIDs(os.Getenv(notificationMemberReaderDeploymentsEnv)),
		AllowedTenantIDs:     parseNotificationReaderIDs(os.Getenv(notificationMemberReaderTenantsEnv)),
	})
}

func parseNotificationReaderIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if !validReaderScopeID(id) {
			return nil
		}
		if _, duplicate := seen[id]; duplicate {
			return nil
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
