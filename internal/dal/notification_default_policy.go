package dal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"project/internal/model"
	"project/pkg/global"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrDefaultPolicyConflict    = errors.New("notification default policy version conflict")
	ErrDefaultPolicyUnavailable = errors.New("notification default policy unavailable")
	ErrDefaultPolicyInvalid     = errors.New("notification default policy target unavailable")
)

type TenantDefaultPolicyRecord struct {
	SourceDeploymentID string    `gorm:"column:source_deployment_id" json:"-"`
	TenantID           string    `gorm:"column:tenant_id" json:"-"`
	NativeGroupID      *string   `gorm:"column:native_group_id" json:"-"`
	AliasGroupID       *string   `gorm:"column:alias_group_id" json:"-"`
	GroupRevision      int64     `gorm:"column:group_revision" json:"-"`
	Version            int64     `gorm:"column:version" json:"version"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"-"`
}

func defaultPolicyLockKey(deploymentID, tenantID string) string {
	return fmt.Sprintf("default-policy:%d:%s%d:%s", len(deploymentID), deploymentID, len(tenantID), tenantID)
}

func lockDefaultPolicyTenant(tx *gorm.DB, deploymentID, tenantID string) error {
	if deploymentID == "" || tenantID == "" {
		return ErrDefaultPolicyUnavailable
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, defaultPolicyLockKey(deploymentID, tenantID)).Error; err != nil {
		return ErrDefaultPolicyUnavailable
	}
	return nil
}

func lockDefaultPolicyAlias(tx *gorm.DB, key SourceRouteKey) error {
	if key.DeploymentID == "" || key.TenantID == "" || key.LegacyGroup == "" {
		return ErrDefaultPolicyUnavailable
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, sourceRouteAdvisoryKey(key)).Error; err != nil {
		return ErrDefaultPolicyUnavailable
	}
	return nil
}

// GetTenantDefaultPolicy reads the tenant pointer and its currently available
// published policies. All reads are scoped by the authenticated tenant ID.
func GetTenantDefaultPolicy(ctx context.Context, deploymentID, tenantID string) (TenantDefaultPolicyRecord, []NativePublishState, error) {
	var selected TenantDefaultPolicyRecord
	available := make([]NativePublishState, 0)
	if global.DB == nil || deploymentID == "" || tenantID == "" {
		return selected, available, ErrDefaultPolicyUnavailable
	}
	db := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	if err := db.Raw(`SELECT source_deployment_id, tenant_id, native_group_id, alias_group_id, group_revision, version, updated_at
		FROM notification_tenant_default_policies WHERE source_deployment_id = ? AND tenant_id = ?`, deploymentID, tenantID).Scan(&selected).Error; err != nil {
		return selected, available, ErrDefaultPolicyUnavailable
	}
	var rows []struct {
		LegacyGroupID string `gorm:"column:legacy_group_id"`
		NativeGroupID string `gorm:"column:native_group_id"`
	}
	if err := db.Raw(`SELECT r.legacy_group_id, r.notification_group_id AS native_group_id
		FROM notification_source_group_routes r
		JOIN notification_groups g ON g.id = r.legacy_group_id AND g.tenant_id = r.tenant_id
		WHERE r.source_deployment_id = ? AND r.tenant_id = ? AND r.engine = 'encore'
		AND g.notification_type = 'ENCORE' AND g.status = 'OPEN'
		ORDER BY r.notification_group_id, r.legacy_group_id`, deploymentID, tenantID).Scan(&rows).Error; err != nil {
		return selected, available, ErrDefaultPolicyUnavailable
	}
	for _, row := range rows {
		state, err := GetNativePublishState(ctx, SourceRouteKey{DeploymentID: deploymentID, TenantID: tenantID, LegacyGroup: row.LegacyGroupID}, row.NativeGroupID)
		if err != nil || !state.Published || !state.Enabled {
			continue
		}
		available = append(available, state)
	}
	return selected, available, nil
}

// SetTenantDefaultPolicy atomically changes the pointer using a tenant lock
// that also protects first-row creation. It takes locks in the same order as
// source alarm resolution (tenant policy, then native alias route).
func SetTenantDefaultPolicy(ctx context.Context, deploymentID, tenantID, nativeGroupID, aliasGroupID string, expectedVersion int64) (TenantDefaultPolicyRecord, error) {
	var out TenantDefaultPolicyRecord
	if global.DB == nil || deploymentID == "" || tenantID == "" || expectedVersion < 0 {
		return out, ErrDefaultPolicyInvalid
	}
	if (nativeGroupID == "") != (aliasGroupID == "") {
		return out, ErrDefaultPolicyInvalid
	}
	db := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockDefaultPolicyTenant(tx, deploymentID, tenantID); err != nil {
			return err
		}
		var current TenantDefaultPolicyRecord
		result := tx.Raw(`SELECT source_deployment_id, tenant_id, native_group_id, alias_group_id, group_revision, version, updated_at
			FROM notification_tenant_default_policies WHERE source_deployment_id = ? AND tenant_id = ? FOR UPDATE`, deploymentID, tenantID).Scan(&current)
		if result.Error != nil {
			return ErrDefaultPolicyUnavailable
		}
		if result.RowsAffected == 0 {
			current = TenantDefaultPolicyRecord{SourceDeploymentID: deploymentID, TenantID: tenantID}
		}
		if current.Version != expectedVersion {
			return ErrDefaultPolicyConflict
		}
		nextVersion := current.Version + 1
		revision := int64(0)
		var nativeValue, aliasValue any
		if nativeGroupID != "" {
			key := SourceRouteKey{DeploymentID: deploymentID, TenantID: tenantID, LegacyGroup: aliasGroupID}
			if err := lockDefaultPolicyAlias(tx, key); err != nil {
				return err
			}
			var route struct {
				Engine   string  `gorm:"column:engine"`
				ActiveID *string `gorm:"column:notification_group_id"`
				Revision int64   `gorm:"column:group_revision"`
			}
			routeResult := tx.Raw(`SELECT engine, notification_group_id, group_revision FROM notification_source_group_routes
				WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? FOR UPDATE`, deploymentID, tenantID, aliasGroupID).Scan(&route)
			if routeResult.Error != nil || routeResult.RowsAffected != 1 || route.Engine != "encore" || route.ActiveID == nil || *route.ActiveID != nativeGroupID || route.Revision < 1 {
				return ErrDefaultPolicyInvalid
			}
			var alias struct {
				Status string `gorm:"column:status"`
				Kind   string `gorm:"column:notification_type"`
			}
			aliasResult := tx.Raw(`SELECT status, notification_type FROM notification_groups WHERE id = ? AND tenant_id = ?`, aliasGroupID, tenantID).Scan(&alias)
			if aliasResult.Error != nil || aliasResult.RowsAffected != 1 || alias.Status != "OPEN" || alias.Kind != "ENCORE" {
				return ErrDefaultPolicyInvalid
			}
			revision = route.Revision
			nativeValue, aliasValue = nativeGroupID, aliasGroupID
		}
		now := time.Now().UTC()
		if err := tx.Exec(`INSERT INTO notification_tenant_default_policies
			(source_deployment_id, tenant_id, native_group_id, alias_group_id, group_revision, version, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (source_deployment_id, tenant_id) DO UPDATE SET
			native_group_id = EXCLUDED.native_group_id, alias_group_id = EXCLUDED.alias_group_id,
			group_revision = EXCLUDED.group_revision, version = EXCLUDED.version, updated_at = EXCLUDED.updated_at`,
			deploymentID, tenantID, nativeValue, aliasValue, revision, nextVersion, now).Error; err != nil {
			return ErrDefaultPolicyUnavailable
		}
		out = TenantDefaultPolicyRecord{SourceDeploymentID: deploymentID, TenantID: tenantID, Version: nextVersion, GroupRevision: revision, UpdatedAt: now}
		if nativeGroupID != "" {
			out.NativeGroupID = &nativeGroupID
			out.AliasGroupID = &aliasGroupID
		}
		return nil
	})
	if err != nil {
		return TenantDefaultPolicyRecord{}, err
	}
	return out, nil
}

func resolveDefaultPolicyTx(tx *gorm.DB, deploymentID, tenantID string) (TenantDefaultPolicyRecord, bool, error) {
	var selected TenantDefaultPolicyRecord
	result := tx.Raw(`SELECT source_deployment_id, tenant_id, native_group_id, alias_group_id, group_revision, version, updated_at
		FROM notification_tenant_default_policies WHERE source_deployment_id = ? AND tenant_id = ? FOR UPDATE`, deploymentID, tenantID).Scan(&selected)
	if result.Error != nil {
		return selected, false, ErrDefaultPolicyUnavailable
	}
	if result.RowsAffected == 0 || selected.NativeGroupID == nil || selected.AliasGroupID == nil {
		return selected, false, nil
	}
	return selected, true, nil
}

func defaultRouteSnapshotTx(tx *gorm.DB, deploymentID, tenantID string, selected TenantDefaultPolicyRecord) (SourceRouteSnapshot, bool, error) {
	if selected.NativeGroupID == nil || selected.AliasGroupID == nil || *selected.NativeGroupID == "" || *selected.AliasGroupID == "" || selected.GroupRevision < 1 {
		return SourceRouteSnapshot{}, false, ErrDefaultPolicyUnavailable
	}
	key := SourceRouteKey{DeploymentID: deploymentID, TenantID: tenantID, LegacyGroup: *selected.AliasGroupID}
	if err := lockDefaultPolicyAlias(tx, key); err != nil {
		return SourceRouteSnapshot{}, false, err
	}
	var route struct {
		Engine   string  `gorm:"column:engine"`
		ActiveID *string `gorm:"column:notification_group_id"`
		BoundID  *string `gorm:"column:bound_notification_group_id"`
		Revision int64   `gorm:"column:group_revision"`
		Version  int64   `gorm:"column:route_version"`
	}
	routeResult := tx.Raw(`SELECT engine, notification_group_id, bound_notification_group_id, group_revision, route_version
		FROM notification_source_group_routes WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? FOR UPDATE`, deploymentID, tenantID, *selected.AliasGroupID).Scan(&route)
	if routeResult.Error != nil {
		return SourceRouteSnapshot{}, false, ErrDefaultPolicyUnavailable
	}
	if routeResult.RowsAffected == 1 && route.Engine == "encore" && route.ActiveID != nil && *route.ActiveID == *selected.NativeGroupID && route.BoundID != nil && *route.BoundID == *selected.NativeGroupID && route.Revision > 0 {
		var alias struct {
			Status string `gorm:"column:status"`
			Kind   string `gorm:"column:notification_type"`
		}
		aliasResult := tx.Raw(`SELECT status, notification_type FROM notification_groups WHERE id = ? AND tenant_id = ? FOR UPDATE`, *selected.AliasGroupID, tenantID).Scan(&alias)
		if aliasResult.Error != nil {
			return SourceRouteSnapshot{}, false, ErrDefaultPolicyUnavailable
		}
		if aliasResult.RowsAffected == 1 && alias.Status == "OPEN" && alias.Kind == "ENCORE" {
			return SourceRouteSnapshot{Engine: "encore", NotificationGroupID: *selected.NativeGroupID, GroupRevision: route.Revision, RouteVersion: route.Version}, true, nil
		}
	}
	// Preserve a durable, non-sendable diagnostic for the selected policy even
	// after it is stopped. The selected revision is stored when the pointer is
	// changed and is raised to the latest known projection revision if present.
	revision := selected.GroupRevision
	if routeResult.RowsAffected == 1 && route.Revision > revision {
		revision = route.Revision
	}
	var latest struct {
		GroupRevision int64 `gorm:"column:group_revision"`
	}
	latestResult := tx.Raw(`SELECT group_revision FROM notification_source_group_route_revisions
		WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND notification_group_id = ?
		ORDER BY group_revision DESC LIMIT 1`, deploymentID, tenantID, *selected.AliasGroupID, *selected.NativeGroupID).Scan(&latest)
	if latestResult.Error != nil {
		return SourceRouteSnapshot{}, false, ErrDefaultPolicyUnavailable
	}
	if latestResult.RowsAffected == 1 && latest.GroupRevision > revision {
		revision = latest.GroupRevision
	}
	if revision < 1 {
		return SourceRouteSnapshot{}, false, ErrDefaultPolicyUnavailable
	}
	version := int64(0)
	if routeResult.RowsAffected == 1 {
		version = route.Version
	}
	return SourceRouteSnapshot{Engine: "blocked", NotificationGroupID: *selected.NativeGroupID, GroupRevision: revision, RouteVersion: version}, false, nil
}

func saveAlarmWithDefault(ctx context.Context, deploymentID, tenantID string, alarm any, buildOutbox func(SourceRouteSnapshot, string) (*SourceOutboxRecord, error)) (SourceRouteSnapshot, string, error) {
	var route SourceRouteSnapshot
	if global.DB == nil || deploymentID == "" || tenantID == "" || alarm == nil || buildOutbox == nil {
		return route, "", ErrDefaultPolicyUnavailable
	}
	var aliasID string
	var readErr error
	err := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := lockDefaultPolicyTenant(tx, deploymentID, tenantID); err != nil {
			readErr = err
			return err
		}
		selected, found, err := resolveDefaultPolicyTx(tx, deploymentID, tenantID)
		if err != nil {
			readErr = err
			return err
		}
		if !found {
			if err := tx.Create(alarm).Error; err != nil {
				return err
			}
			route = SourceRouteSnapshot{Engine: "none"}
			return nil
		}
		aliasID = *selected.AliasGroupID
		route, _, err = defaultRouteSnapshotTx(tx, deploymentID, tenantID, selected)
		if err != nil {
			readErr = err
			return err
		}
		if route.Engine == "blocked" {
			setAlarmRemark(alarm, "default_policy_unavailable")
		}
		if err := tx.Create(alarm).Error; err != nil {
			return err
		}
		outbox, err := buildOutbox(route, aliasID)
		if err != nil {
			return err
		}
		if outbox == nil || outbox.NotificationGroupID != route.NotificationGroupID || outbox.GroupRevision != route.GroupRevision || outbox.LegacyGroupID != aliasID {
			return ErrSourceRouteConflict
		}
		if route.Engine == "blocked" {
			outbox.State = "blocked"
			failure := "default_policy_unavailable"
			outbox.FailureCode = &failure
		}
		if err := tx.Create(outbox).Error; err != nil {
			return err
		}
		return nil
	})
	if readErr != nil {
		return SourceRouteSnapshot{}, "", ErrDefaultPolicyUnavailable
	}
	if err != nil {
		return SourceRouteSnapshot{}, "", err
	}
	return route, aliasID, nil
}

func setAlarmRemark(alarm any, remark string) {
	switch value := alarm.(type) {
	case *model.AlarmInfo:
		value.Remark = &remark
	case *model.AlarmHistory:
		value.Remark = &remark
	}
}

func SaveAlarmInfoWithDefaultSource(ctx context.Context, alarm *model.AlarmInfo, deploymentID, tenantID string, buildOutbox func(SourceRouteSnapshot, string) (*SourceOutboxRecord, error)) (SourceRouteSnapshot, string, error) {
	return saveAlarmWithDefault(ctx, deploymentID, tenantID, alarm, buildOutbox)
}

func SaveAlarmHistoryWithDefaultSource(ctx context.Context, alarm *model.AlarmHistory, deploymentID, tenantID string, buildOutbox func(SourceRouteSnapshot, string) (*SourceOutboxRecord, error)) (SourceRouteSnapshot, string, error) {
	return saveAlarmWithDefault(ctx, deploymentID, tenantID, alarm, buildOutbox)
}

func ValidateDefaultPolicyMigration() error {
	if global.DB == nil {
		return ErrDefaultPolicyUnavailable
	}
	var exists bool
	if err := global.DB.Session(&gorm.Session{Logger: logger.Discard}).Raw(`SELECT to_regclass('notification_tenant_default_policies') IS NOT NULL`).Scan(&exists).Error; err != nil || !exists {
		return ErrDefaultPolicyUnavailable
	}
	return nil
}

