package dal

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"project/pkg/global"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrNotificationMemberNotFound        = errors.New("notification member not found")
	ErrNotificationMemberReadUnavailable = errors.New("notification member read unavailable")
)

type NotificationMemberContact struct {
	UserID       string `json:"userId"`
	TenantID     string `json:"tenantId"`
	ContactField string `json:"contactField"`
	Address      string `json:"address"`
}

type NotificationTenantPresence struct {
	TenantID string `json:"tenantId"`
	Exists   bool   `json:"exists"`
}

// ResolveNotificationMemberContact returns one explicitly requested contact
// for a currently active member of the exact tenant. It never loads a full user.
func ResolveNotificationMemberContact(ctx context.Context, userID, tenantID, contactField string) (NotificationMemberContact, error) {
	var contact NotificationMemberContact
	if global.DB == nil || userID == "" || tenantID == "" {
		return contact, ErrNotificationMemberReadUnavailable
	}
	var query string
	switch contactField {
	case "email":
		query = `SELECT id, tenant_id, btrim(email) AS address FROM users
			WHERE id = ? AND tenant_id = ? AND status = 'N' AND btrim(email) <> '' LIMIT 1`
	case "phone":
		query = `SELECT id, tenant_id, btrim(phone_number) AS address FROM users
			WHERE id = ? AND tenant_id = ? AND status = 'N' AND btrim(phone_number) <> '' LIMIT 1`
	default:
		return contact, ErrNotificationMemberNotFound
	}
	var row struct {
		UserID   string `gorm:"column:id"`
		TenantID string `gorm:"column:tenant_id"`
		Address  string `gorm:"column:address"`
	}
	err := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Raw(query, userID, tenantID).Row().Scan(&row.UserID, &row.TenantID, &row.Address)
	if errors.Is(err, sql.ErrNoRows) {
		return contact, ErrNotificationMemberNotFound
	}
	if err != nil {
		return contact, ErrNotificationMemberReadUnavailable
	}
	if row.UserID != userID || row.TenantID != tenantID || strings.TrimSpace(row.Address) == "" {
		return contact, ErrNotificationMemberNotFound
	}
	return NotificationMemberContact{UserID: row.UserID, TenantID: row.TenantID, ContactField: contactField, Address: row.Address}, nil
}

// ResolveNotificationTenant reports an active tenant only when its tenant
// administrator record is currently active. This legacy schema has no tenant
// entity table; tenant identity is represented by TENANT_ADMIN users.
func ResolveNotificationTenant(ctx context.Context, tenantID string) (NotificationTenantPresence, error) {
	var presence NotificationTenantPresence
	if global.DB == nil || tenantID == "" {
		return presence, ErrNotificationMemberReadUnavailable
	}
	var found string
	err := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Raw(`
		SELECT tenant_id FROM users
		WHERE tenant_id = ? AND authority = 'TENANT_ADMIN' AND status = 'N'
		LIMIT 1`, tenantID).Row().Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return presence, ErrNotificationMemberNotFound
	}
	if err != nil {
		return presence, ErrNotificationMemberReadUnavailable
	}
	if found != tenantID {
		return presence, ErrNotificationMemberNotFound
	}
	return NotificationTenantPresence{TenantID: found, Exists: true}, nil
}
