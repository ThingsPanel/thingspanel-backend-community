package dal

import (
	"context"
	"errors"

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
	if global.DB == nil || group == nil || group.ID == "" || group.TenantID == "" {
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
	if global.DB == nil || id == "" || tenantID == "" {
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
		if bridgeEnabled {
			var tables int
			if err := tx.Raw(`SELECT count(*) FROM unnest(ARRAY[
				to_regclass('notification_source_group_routes'),
				to_regclass('notification_source_group_route_revisions'),
				to_regclass('notification_source_outbox')]) AS t(rel) WHERE rel IS NOT NULL`).Scan(&tables).Error; err != nil || tables != 3 {
				return ErrSourceRouteUnavailable
			}
			lockKey := sourceRouteAdvisoryKey(key)
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, lockKey).Error; err != nil {
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
