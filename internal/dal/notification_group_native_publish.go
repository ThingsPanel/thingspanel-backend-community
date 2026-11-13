package dal

import (
	"context"
	"errors"
	"time"

	"project/internal/model"
	"project/pkg/global"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const notificationEncoreGroupType = "ENCORE"

type NativePublishState struct {
	LegacyGroupID          string
	NativeGroupID          string
	Name                   string
	GroupRevision          int64
	EffectiveGroupRevision int64
	RouteVersion           int64
	Engine                 string
	Enabled                bool
	Published              bool
	Status                 string
}

// EnsureNativePublishAlias creates the deterministic legacy selector row in a
// closed state. It never opens a route and never replaces an existing group.
func EnsureNativePublishAlias(ctx context.Context, key SourceRouteKey, aliasID, name string) error {
	if global.DB == nil || key.DeploymentID == "" || key.TenantID == "" || key.LegacyGroup != aliasID || aliasID == "" || name == "" {
		return ErrSourceRouteUnavailable
	}
	db := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, sourceRouteAdvisoryKey(key)).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		var existing model.NotificationGroup
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", aliasID).Take(&existing)
		if result.Error == nil {
			if existing.TenantID != key.TenantID || existing.NotificationType != notificationEncoreGroupType {
				return ErrSourceRouteConflict
			}
			return nil
		}
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrSourceRouteUnavailable
		}
		config := "{}"
		group := model.NotificationGroup{ID: aliasID, Name: name, NotificationType: notificationEncoreGroupType, Status: "CLOSE", NotificationConfig: &config, TenantID: key.TenantID, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		if err := tx.Create(&group).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		return nil
	})
}

// ActivateNativePublishRoute atomically opens the ENCORE alias and switches
// future alarms to the exact native revision. Existing projection-history rows
// are reused when a previously stopped route is resumed.
func ActivateNativePublishRoute(ctx context.Context, key SourceRouteKey, nativeGroupID, aliasName string, groupRevision, expectedRouteVersion int64, projectionKey string) (NativePublishState, bool, error) {
	if global.DB == nil || key.DeploymentID == "" || key.TenantID == "" || key.LegacyGroup == "" || nativeGroupID == "" || aliasName == "" || groupRevision < 1 || expectedRouteVersion < 0 || projectionKey == "" {
		return NativePublishState{}, false, ErrSourceRouteConflict
	}
	var state NativePublishState
	replayed := false
	db := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, sourceRouteAdvisoryKey(key)).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		if err := tx.Exec(`INSERT INTO notification_source_group_routes
			(source_deployment_id, tenant_id, legacy_group_id, engine, group_revision, route_version, bound_notification_group_id)
			VALUES (?, ?, ?, 'legacy', 0, 0, NULL) ON CONFLICT DO NOTHING`, key.DeploymentID, key.TenantID, key.LegacyGroup).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		var route struct {
			Engine        string  `gorm:"column:engine"`
			ActiveID      *string `gorm:"column:notification_group_id"`
			BoundID       *string `gorm:"column:bound_notification_group_id"`
			GroupRevision int64   `gorm:"column:group_revision"`
			RouteVersion  int64   `gorm:"column:route_version"`
		}
		result := tx.Raw(`SELECT engine, notification_group_id, bound_notification_group_id, group_revision, route_version
			FROM notification_source_group_routes
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? FOR UPDATE`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&route)
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrSourceRouteUnavailable
		}
		var alias model.NotificationGroup
		result = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", key.LegacyGroup, key.TenantID).Take(&alias)
		if result.Error != nil || alias.NotificationType != notificationEncoreGroupType {
			return ErrSourceRouteConflict
		}
		if route.Engine == "encore" && route.ActiveID != nil && *route.ActiveID == nativeGroupID && route.BoundID != nil && *route.BoundID == nativeGroupID && route.GroupRevision == groupRevision && alias.Status == "OPEN" && alias.Name == aliasName {
			state = nativePublishStateFromRows(key.LegacyGroup, nativeGroupID, alias, route.Engine, route.GroupRevision, route.RouteVersion, true)
			replayed = true
			return nil
		}
		if route.RouteVersion != expectedRouteVersion || (route.BoundID != nil && *route.BoundID != nativeGroupID) {
			return ErrSourceRouteConflict
		}
		if route.Engine == "encore" {
			if route.ActiveID == nil || *route.ActiveID != nativeGroupID || groupRevision <= route.GroupRevision {
				return ErrSourceRouteConflict
			}
		} else if route.Engine != "legacy" {
			return ErrSourceRouteConflict
		}
		var existingProjection struct {
			NotificationGroupID string `gorm:"column:notification_group_id"`
		}
		projectionResult := tx.Raw(`SELECT notification_group_id FROM notification_source_group_route_revisions
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND group_revision = ?`, key.DeploymentID, key.TenantID, key.LegacyGroup, groupRevision).Scan(&existingProjection)
		if projectionResult.Error != nil {
			return ErrSourceRouteUnavailable
		}
		insertProjection := projectionResult.RowsAffected == 0
		if !insertProjection && existingProjection.NotificationGroupID != nativeGroupID {
			return ErrSourceRouteConflict
		}
		if insertProjection {
			var revisionCount int64
			if err := tx.Raw(`SELECT count(*) FROM notification_source_group_route_revisions WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ?`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&revisionCount).Error; err != nil {
				return ErrSourceRouteUnavailable
			}
			if revisionCount >= 128 {
				return ErrSourceRouteConflict
			}
		}
		now := time.Now().UTC()
		updated := tx.Exec(`UPDATE notification_source_group_routes
			SET engine = 'encore', notification_group_id = ?, bound_notification_group_id = ?, group_revision = ?, route_version = route_version + 1, updated_at = ?
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND route_version = ?`,
			nativeGroupID, nativeGroupID, groupRevision, now, key.DeploymentID, key.TenantID, key.LegacyGroup, expectedRouteVersion)
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrSourceRouteConflict
		}
		if insertProjection {
			if err := tx.Exec(`INSERT INTO notification_source_group_route_revisions
				(source_deployment_id, tenant_id, legacy_group_id, group_revision, notification_group_id, projection_key, projected_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)`, key.DeploymentID, key.TenantID, key.LegacyGroup, groupRevision, nativeGroupID, projectionKey, now).Error; err != nil {
				return ErrSourceRouteConflict
			}
		}
		opened := tx.Model(&model.NotificationGroup{}).Where("id = ? AND tenant_id = ? AND notification_type = ?", key.LegacyGroup, key.TenantID, notificationEncoreGroupType).
			Updates(map[string]any{"name": aliasName, "status": "OPEN", "notification_config": "{}", "updated_at": now})
		if opened.Error != nil || opened.RowsAffected != 1 {
			return ErrSourceRouteConflict
		}
		state = NativePublishState{LegacyGroupID: key.LegacyGroup, NativeGroupID: nativeGroupID, Name: aliasName,
			GroupRevision: groupRevision, EffectiveGroupRevision: groupRevision, RouteVersion: route.RouteVersion + 1,
			Engine: "encore", Enabled: true, Published: true, Status: "published"}
		return nil
	})
	if err != nil {
		return NativePublishState{}, false, err
	}
	return state, replayed, nil
}

// StopNativePublishRoute switches only future alarms back to the legacy
// selector and closes the deterministic alias in the same transaction.
func StopNativePublishRoute(ctx context.Context, key SourceRouteKey, nativeGroupID string, expectedRouteVersion int64) (NativePublishState, bool, error) {
	if global.DB == nil || key.DeploymentID == "" || key.TenantID == "" || key.LegacyGroup == "" || nativeGroupID == "" || expectedRouteVersion < 0 {
		return NativePublishState{}, false, ErrSourceRouteConflict
	}
	var state NativePublishState
	replayed := false
	db := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, sourceRouteAdvisoryKey(key)).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		var route struct {
			Engine        string  `gorm:"column:engine"`
			ActiveID      *string `gorm:"column:notification_group_id"`
			BoundID       *string `gorm:"column:bound_notification_group_id"`
			GroupRevision int64   `gorm:"column:group_revision"`
			RouteVersion  int64   `gorm:"column:route_version"`
		}
		result := tx.Raw(`SELECT engine, notification_group_id, bound_notification_group_id, group_revision, route_version
			FROM notification_source_group_routes
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? FOR UPDATE`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&route)
		if result.Error != nil {
			return ErrSourceRouteUnavailable
		}
		if result.RowsAffected != 1 || route.BoundID == nil || *route.BoundID != nativeGroupID {
			return ErrSourceRouteConflict
		}
		var alias model.NotificationGroup
		result = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", key.LegacyGroup, key.TenantID).Take(&alias)
		if result.Error != nil || alias.NotificationType != notificationEncoreGroupType {
			return ErrSourceRouteConflict
		}
		if route.Engine == "legacy" && route.ActiveID == nil && route.GroupRevision == 0 && alias.Status == "CLOSE" {
			state, result.Error = nativePublishStateTx(tx, key, nativeGroupID)
			if result.Error != nil {
				return ErrSourceRouteUnavailable
			}
			replayed = true
			return nil
		}
		if route.Engine != "encore" || route.ActiveID == nil || *route.ActiveID != nativeGroupID || route.RouteVersion != expectedRouteVersion || route.GroupRevision < 1 || alias.Status != "OPEN" {
			return ErrSourceRouteConflict
		}
		now := time.Now().UTC()
		updated := tx.Exec(`UPDATE notification_source_group_routes
			SET engine = 'legacy', notification_group_id = NULL, group_revision = 0, route_version = route_version + 1, updated_at = ?
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND route_version = ? AND engine = 'encore'`,
			now, key.DeploymentID, key.TenantID, key.LegacyGroup, expectedRouteVersion)
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrSourceRouteConflict
		}
		closed := tx.Model(&model.NotificationGroup{}).Where("id = ? AND tenant_id = ? AND notification_type = ? AND status = 'OPEN'", key.LegacyGroup, key.TenantID, notificationEncoreGroupType).
			Update("status", "CLOSE")
		if closed.Error != nil || closed.RowsAffected != 1 {
			return ErrSourceRouteConflict
		}
		if err := tx.Model(&model.NotificationGroup{}).Where("id = ? AND tenant_id = ?", key.LegacyGroup, key.TenantID).Update("updated_at", now).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		state = NativePublishState{LegacyGroupID: key.LegacyGroup, NativeGroupID: nativeGroupID, Name: alias.Name,
			GroupRevision: route.GroupRevision, EffectiveGroupRevision: 0, RouteVersion: route.RouteVersion + 1,
			Engine: "legacy", Enabled: false, Published: false, Status: "stopped"}
		return nil
	})
	if err != nil {
		return NativePublishState{}, false, err
	}
	return state, replayed, nil
}

// GetNativePublishState returns tenant-scoped current state and immutable last
// published revision for the deterministic alias.
func GetNativePublishState(ctx context.Context, key SourceRouteKey, nativeGroupID string) (NativePublishState, error) {
	if global.DB == nil || key.DeploymentID == "" || key.TenantID == "" || key.LegacyGroup == "" || nativeGroupID == "" {
		return NativePublishState{}, ErrSourceRouteUnavailable
	}
	var state NativePublishState
	db := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		state, err = nativePublishStateTx(tx, key, nativeGroupID)
		return err
	})
	if err != nil {
		return NativePublishState{}, err
	}
	return state, nil
}

func nativePublishStateTx(tx *gorm.DB, key SourceRouteKey, nativeGroupID string) (NativePublishState, error) {
	state := NativePublishState{LegacyGroupID: key.LegacyGroup, NativeGroupID: nativeGroupID, Engine: "legacy", Status: "unpublished"}
	var alias model.NotificationGroup
	result := tx.Where("id = ? AND tenant_id = ?", key.LegacyGroup, key.TenantID).Take(&alias)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return NativePublishState{}, ErrSourceRouteUnavailable
	}
	if result.Error == nil {
		if alias.NotificationType != notificationEncoreGroupType {
			return NativePublishState{}, ErrSourceRouteConflict
		}
		state.Name = alias.Name
		state.Enabled = alias.Status == "OPEN"
	}
	var route struct {
		Engine        string  `gorm:"column:engine"`
		ActiveID      *string `gorm:"column:notification_group_id"`
		BoundID       *string `gorm:"column:bound_notification_group_id"`
		GroupRevision int64   `gorm:"column:group_revision"`
		RouteVersion  int64   `gorm:"column:route_version"`
	}
	result = tx.Raw(`SELECT engine, notification_group_id, bound_notification_group_id, group_revision, route_version
		FROM notification_source_group_routes WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ?`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&route)
	if result.Error != nil {
		return NativePublishState{}, ErrSourceRouteUnavailable
	}
	if result.RowsAffected == 0 {
		if state.Enabled {
			return NativePublishState{}, ErrSourceRouteConflict
		}
		return state, nil
	}
	if route.Engine != "legacy" && route.Engine != "encore" {
		return NativePublishState{}, ErrSourceRouteUnavailable
	}
	state.RouteVersion = route.RouteVersion
	if route.BoundID != nil && *route.BoundID != nativeGroupID {
		return NativePublishState{}, ErrSourceRouteConflict
	}
	if route.Engine == "encore" {
		if route.ActiveID == nil || *route.ActiveID != nativeGroupID || route.BoundID == nil || route.GroupRevision < 1 || !state.Enabled {
			return NativePublishState{}, ErrSourceRouteConflict
		}
		state.GroupRevision = route.GroupRevision
		state.EffectiveGroupRevision = route.GroupRevision
		state.Engine = "encore"
		state.Published = true
		state.Status = "published"
		return state, nil
	}
	if route.ActiveID != nil || route.GroupRevision != 0 {
		return NativePublishState{}, ErrSourceRouteConflict
	}
	if route.BoundID != nil {
		state.Status = "stopped"
		var latest struct {
			GroupRevision int64 `gorm:"column:group_revision"`
		}
		result := tx.Raw(`SELECT group_revision FROM notification_source_group_route_revisions
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND notification_group_id = ?
			ORDER BY group_revision DESC LIMIT 1`, key.DeploymentID, key.TenantID, key.LegacyGroup, nativeGroupID).Scan(&latest)
		if result.Error != nil || result.RowsAffected != 1 {
			return NativePublishState{}, ErrSourceRouteUnavailable
		}
		state.GroupRevision = latest.GroupRevision
	}
	return state, nil
}

func nativePublishStateFromRows(aliasID, nativeID string, alias model.NotificationGroup, engine string, revision, routeVersion int64, enabled bool) NativePublishState {
	state := NativePublishState{LegacyGroupID: aliasID, NativeGroupID: nativeID, Name: alias.Name, GroupRevision: revision,
		RouteVersion: routeVersion, Engine: engine, Enabled: enabled, Published: engine == "encore" && enabled}
	if state.Published {
		state.Status = "published"
		state.EffectiveGroupRevision = revision
	} else {
		state.Engine = "legacy"
		state.Status = "stopped"
	}
	return state
}
