package dal

import (
	"context"
	"errors"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"project/internal/model"
	"project/pkg/global"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrNotificationGroupNotFound = errors.New("notification group not found")
	ErrNotificationGroupReadOnly = errors.New("notification group is read only")
)

// UpdateNotificationGroupForTenant serializes legacy edits with route changes.
// When the bridge is enabled, missing route tables or lookup errors fail closed.
func UpdateNotificationGroupForTenant(ctx context.Context, group *model.NotificationGroup, key SourceRouteKey, bridgeEnabled bool) error {
	if global.DB == nil || group == nil || group.ID == "" || group.TenantID == "" || key.TenantID != group.TenantID || key.LegacyGroup != group.ID {
		return ErrSourceRouteUnavailable
	}
	return withLegacyGroupWriteLock(ctx, key, bridgeEnabled, func(tx *gorm.DB) error {
		result := tx.Model(&model.NotificationGroup{}).
			Where("id = ? AND tenant_id = ?", group.ID, group.TenantID).
			Updates(group)
		if result.Error != nil {
			return ErrSourceRouteUnavailable
		}
		if result.RowsAffected == 0 {
			return ErrNotificationGroupNotFound
		}
		return nil
	})
}

func DeleteNotificationGroupForTenant(ctx context.Context, id, tenantID string, key SourceRouteKey, bridgeEnabled bool) error {
	if global.DB == nil || id == "" || tenantID == "" || key.TenantID != tenantID || key.LegacyGroup != id {
		return ErrSourceRouteUnavailable
	}
	return withLegacyGroupWriteLock(ctx, key, bridgeEnabled, func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&model.NotificationGroup{})
		if result.Error != nil {
			return ErrSourceRouteUnavailable
		}
		if result.RowsAffected == 0 {
			return ErrNotificationGroupNotFound
		}
		return nil
	})
}

func withLegacyGroupWriteLock(ctx context.Context, key SourceRouteKey, bridgeEnabled bool, write func(*gorm.DB) error) error {
	if key.TenantID == "" || key.LegacyGroup == "" || write == nil {
		return ErrSourceRouteUnavailable
	}
	if bridgeEnabled && key.DeploymentID == "" {
		return ErrSourceRouteUnavailable
	}
	db := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	return db.Transaction(func(tx *gorm.DB) error {
		var groupType string
		group := tx.Raw(`SELECT notification_type FROM notification_groups WHERE id = ? AND tenant_id = ? FOR UPDATE`, key.LegacyGroup, key.TenantID).Scan(&groupType)
		if group.Error != nil {
			return ErrSourceRouteUnavailable
		}
		if group.RowsAffected > 0 && groupType == notificationEncoreGroupType {
			return ErrNotificationGroupReadOnly
		}
		if bridgeEnabled {
			tables, err := countSourceBridgeTables(tx)
			if err != nil || tables != 3 {
				return ErrSourceRouteUnavailable
			}
			lockKey := sourceRouteAdvisoryKey(key)
			if err := relational.LockTx(ctx, tx, lockKey); err != nil {
				return ErrSourceRouteUnavailable
			}
			var route struct {
				Engine string `gorm:"column:engine"`
			}
			result := tx.Raw(`SELECT engine FROM notification_source_group_routes
				WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? FOR UPDATE`,
				key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&route)
			if result.Error != nil {
				return ErrSourceRouteUnavailable
			}
			if result.RowsAffected > 0 && route.Engine != "legacy" {
				if route.Engine == "encore" {
					return ErrNotificationGroupReadOnly
				}
				return ErrSourceRouteUnavailable
			}
		}
		if err := write(tx); err != nil {
			return err
		}
		return nil
	})
}

// GetNotificationGroupByTenantID masks cross-tenant identifiers as missing.
func GetNotificationGroupByTenantID(id, tenantID string) (*model.NotificationGroup, error) {
	if global.DB == nil || id == "" || tenantID == "" {
		return nil, ErrNotificationGroupNotFound
	}
	var group model.NotificationGroup
	err := global.DB.Session(&gorm.Session{Logger: logger.Discard}).
		Where("id = ? AND tenant_id = ?", id, tenantID).First(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotificationGroupNotFound
	}
	if err != nil {
		return nil, ErrSourceRouteUnavailable
	}
	return &group, nil
}

// sourceBridgeTables are the tables the notification source bridge needs.
var sourceBridgeTables = []string{"notification_source_group_routes", "notification_source_group_route_revisions", "notification_source_outbox"}

// countSourceBridgeTables counts how many of the bridge tables exist in the current schema. PostgreSQL asks
// to_regclass (which also honours the search path); MySQL asks information_schema.
func countSourceBridgeTables(db *gorm.DB) (int, error) {
	var n int
	if db.Dialector.Name() == "mysql" {
		err := db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name IN (?, ?, ?)`,
			sourceBridgeTables[0], sourceBridgeTables[1], sourceBridgeTables[2]).Scan(&n).Error
		return n, err
	}
	err := db.Raw(`SELECT count(*) FROM unnest(ARRAY[
		to_regclass('notification_source_group_routes'),
		to_regclass('notification_source_group_route_revisions'),
		to_regclass('notification_source_outbox')]) AS t(rel) WHERE rel IS NOT NULL`).Scan(&n).Error
	return n, err
}
