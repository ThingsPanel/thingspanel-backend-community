package dal

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"project/internal/model"
	"project/pkg/global"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var ErrSourceRouteUnavailable = errors.New("notification source route unavailable")
var ErrSourceRouteConflict = errors.New("notification source route conflict")

type SourceRouteKey struct {
	DeploymentID string
	TenantID     string
	LegacyGroup  string
}

type SourceRouteSnapshot struct {
	Engine              string
	NotificationGroupID string
	GroupRevision       int64
	RouteVersion        int64
}

func sourceRouteAdvisoryKey(key SourceRouteKey) string {
	parts := []string{key.DeploymentID, key.TenantID, key.LegacyGroup}
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(strconv.Itoa(len(part)))
		b.WriteByte(':')
		b.WriteString(part)
	}
	return b.String()
}

type SourceOutboxRecord struct {
	ID                  string     `gorm:"column:id;type:uuid;primaryKey"`
	SourceDeploymentID  string     `gorm:"column:source_deployment_id"`
	TenantID            string     `gorm:"column:tenant_id"`
	SourceEventID       string     `gorm:"column:source_event_id"`
	SourceActionID      string     `gorm:"column:source_action_id"`
	LegacyGroupID       string     `gorm:"column:legacy_group_id"`
	NotificationGroupID string     `gorm:"column:notification_group_id"`
	GroupRevision       int64      `gorm:"column:group_revision"`
	IdempotencyKey      string     `gorm:"column:idempotency_key"`
	RequestBody         []byte     `gorm:"column:request_body"`
	BodySHA256          string     `gorm:"column:body_sha256"`
	OccurredAt          time.Time  `gorm:"column:occurred_at"`
	ExpiresAt           time.Time  `gorm:"column:expires_at"`
	State               string     `gorm:"column:state"`
	Attempts            int        `gorm:"column:attempts"`
	NextAttemptAt       time.Time  `gorm:"column:next_attempt_at"`
	LeaseToken          *string    `gorm:"column:lease_token;type:uuid"`
	LeaseUntil          *time.Time `gorm:"column:lease_until"`
	HandedOffAt         *time.Time `gorm:"column:handed_off_at"`
	FailureCode         *string    `gorm:"column:failure_code"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (SourceOutboxRecord) TableName() string { return "notification_source_outbox" }

func SaveAlarmInfoQuietly(ctx context.Context, alarm *model.AlarmInfo) error {
	if global.DB == nil || alarm == nil {
		return errors.New("alarm persistence unavailable")
	}
	return global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Create(alarm).Error
}

func SaveAlarmHistoryQuietly(ctx context.Context, alarm *model.AlarmHistory) error {
	if global.DB == nil || alarm == nil {
		return errors.New("alarm persistence unavailable")
	}
	return global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Create(alarm).Error
}

// WithLockedSourceRoute serializes events and route switches for the same
// tuple, including the first route write when no row exists yet.
func WithLockedSourceRoute(ctx context.Context, key SourceRouteKey, fn func(*gorm.DB, SourceRouteSnapshot) error) (SourceRouteSnapshot, error) {
	var snapshot SourceRouteSnapshot
	if key.DeploymentID == "" || key.TenantID == "" || key.LegacyGroup == "" || fn == nil || global.DB == nil {
		return snapshot, ErrSourceRouteUnavailable
	}
	var readErr error
	err := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
		lockKey := sourceRouteAdvisoryKey(key)
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, lockKey).Error; err != nil {
			readErr = ErrSourceRouteUnavailable
			return readErr
		}
		var row struct {
			Engine              string  `gorm:"column:engine"`
			NotificationGroupID *string `gorm:"column:notification_group_id"`
			GroupRevision       int64   `gorm:"column:group_revision"`
			RouteVersion        int64   `gorm:"column:route_version"`
		}
		result := tx.Raw(`SELECT engine, notification_group_id, group_revision, route_version
			FROM notification_source_group_routes
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ?
			FOR UPDATE`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&row)
		if result.Error != nil {
			readErr = ErrSourceRouteUnavailable
			return readErr
		}
		snapshot = SourceRouteSnapshot{Engine: "legacy", RouteVersion: 0}
		if result.RowsAffected > 0 {
			snapshot.Engine = row.Engine
			snapshot.GroupRevision = row.GroupRevision
			snapshot.RouteVersion = row.RouteVersion
			if row.NotificationGroupID != nil {
				snapshot.NotificationGroupID = *row.NotificationGroupID
			}
			if snapshot.Engine != "legacy" && snapshot.Engine != "encore" {
				readErr = ErrSourceRouteUnavailable
				return readErr
			}
		}
		return fn(tx, snapshot)
	})
	if readErr != nil {
		return SourceRouteSnapshot{}, ErrSourceRouteUnavailable
	}
	if err != nil {
		return SourceRouteSnapshot{}, err
	}
	return snapshot, nil
}

func SaveAlarmInfoWithSource(ctx context.Context, alarm *model.AlarmInfo, key SourceRouteKey, buildOutbox func(SourceRouteSnapshot) (*SourceOutboxRecord, error)) (SourceRouteSnapshot, error) {
	return WithLockedSourceRoute(ctx, key, func(tx *gorm.DB, route SourceRouteSnapshot) error {
		if err := tx.Create(alarm).Error; err != nil {
			return err
		}
		if route.Engine == "encore" {
			outbox, err := buildOutbox(route)
			if err != nil {
				return err
			}
			if outbox == nil || outbox.NotificationGroupID != route.NotificationGroupID || outbox.GroupRevision != route.GroupRevision {
				return ErrSourceRouteConflict
			}
			if err := tx.Create(outbox).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func SaveAlarmHistoryWithSource(ctx context.Context, alarm *model.AlarmHistory, key SourceRouteKey, buildOutbox func(SourceRouteSnapshot) (*SourceOutboxRecord, error)) (SourceRouteSnapshot, error) {
	return WithLockedSourceRoute(ctx, key, func(tx *gorm.DB, route SourceRouteSnapshot) error {
		if err := tx.Create(alarm).Error; err != nil {
			return err
		}
		if route.Engine == "encore" {
			outbox, err := buildOutbox(route)
			if err != nil {
				return err
			}
			if outbox == nil || outbox.NotificationGroupID != route.NotificationGroupID || outbox.GroupRevision != route.GroupRevision {
				return ErrSourceRouteConflict
			}
			if err := tx.Create(outbox).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// SwitchSourceRoute persists an already server-confirmed projection under the
// same tuple lock used by event creation. A previous version is immutable.
func SwitchSourceRoute(ctx context.Context, key SourceRouteKey, expectedVersion, groupRevision int64, notificationGroupID, projectionKey string) error {
	return switchSourceRoute(ctx, key, expectedVersion, groupRevision, notificationGroupID, projectionKey, nil)
}

// SwitchSourceRouteIfLegacyGroupUnchanged performs the route CAS only if the
// exact legacy group read before the remote compatibility check is still
// current. The tuple advisory lock serializes it with both alarm creation and
// all bridge-enabled legacy group writes.
func SwitchSourceRouteIfLegacyGroupUnchanged(ctx context.Context, key SourceRouteKey, expectedVersion, groupRevision int64, notificationGroupID, projectionKey string, expectedGroup *model.NotificationGroup) error {
	if expectedGroup == nil || expectedGroup.ID != key.LegacyGroup || expectedGroup.TenantID != key.TenantID {
		return ErrSourceRouteConflict
	}
	return switchSourceRoute(ctx, key, expectedVersion, groupRevision, notificationGroupID, projectionKey, expectedGroup)
}

func switchSourceRoute(ctx context.Context, key SourceRouteKey, expectedVersion, groupRevision int64, notificationGroupID, projectionKey string, expectedGroup *model.NotificationGroup) error {
	if key.DeploymentID == "" || key.TenantID == "" || key.LegacyGroup == "" || notificationGroupID == "" || projectionKey == "" || groupRevision < 1 {
		return ErrSourceRouteConflict
	}
	if global.DB == nil {
		return ErrSourceRouteUnavailable
	}
	return global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
		lockKey := sourceRouteAdvisoryKey(key)
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, lockKey).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		if err := tx.Exec(`INSERT INTO notification_source_group_routes
			(source_deployment_id, tenant_id, legacy_group_id, engine, group_revision, route_version, bound_notification_group_id)
			VALUES (?, ?, ?, 'legacy', 0, 0, NULL) ON CONFLICT DO NOTHING`, key.DeploymentID, key.TenantID, key.LegacyGroup).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		var current struct {
			Engine              string  `gorm:"column:engine"`
			NotificationGroupID *string `gorm:"column:notification_group_id"`
			BoundGroupID        *string `gorm:"column:bound_notification_group_id"`
			GroupRevision       int64   `gorm:"column:group_revision"`
			RouteVersion        int64   `gorm:"column:route_version"`
		}
		if err := tx.Raw(`SELECT engine, notification_group_id, bound_notification_group_id, group_revision, route_version
			FROM notification_source_group_routes
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? FOR UPDATE`,
			key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&current).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		if expectedGroup != nil {
			var current model.NotificationGroup
			result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND tenant_id = ?", key.LegacyGroup, key.TenantID).
				Take(&current)
			if result.Error != nil || result.RowsAffected != 1 || !sameNotificationGroupSnapshot(&current, expectedGroup) {
				return ErrSourceRouteConflict
			}
		}
		if current.RouteVersion != expectedVersion || (current.BoundGroupID != nil && *current.BoundGroupID != notificationGroupID) || groupRevision <= current.GroupRevision {
			return ErrSourceRouteConflict
		}
		var revisionCount int64
		if err := tx.Raw(`SELECT count(*) FROM notification_source_group_route_revisions
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ?`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&revisionCount).Error; err != nil {
			return ErrSourceRouteUnavailable
		}
		if revisionCount >= 128 {
			return ErrSourceRouteConflict
		}
		now := time.Now().UTC()
		result := tx.Exec(`UPDATE notification_source_group_routes
			SET engine = 'encore', notification_group_id = ?, bound_notification_group_id = ?, group_revision = ?, route_version = route_version + 1, updated_at = ?
			WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND route_version = ?`,
			notificationGroupID, notificationGroupID, groupRevision, now, key.DeploymentID, key.TenantID, key.LegacyGroup, expectedVersion)
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrSourceRouteConflict
		}
		result = tx.Exec(`INSERT INTO notification_source_group_route_revisions
			(source_deployment_id, tenant_id, legacy_group_id, group_revision, notification_group_id, projection_key, projected_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, key.DeploymentID, key.TenantID, key.LegacyGroup, groupRevision, notificationGroupID, projectionKey, now)
		if result.Error != nil {
			return ErrSourceRouteConflict
		}
		return nil
	})
}

func sameNotificationGroupSnapshot(current, expected *model.NotificationGroup) bool {
	if current == nil || expected == nil {
		return false
	}
	return current.ID == expected.ID && current.TenantID == expected.TenantID && current.Name == expected.Name &&
		current.NotificationType == expected.NotificationType && current.Status == expected.Status &&
		sameOptionalString(current.NotificationConfig, expected.NotificationConfig) &&
		sameOptionalString(current.Description, expected.Description) && sameOptionalString(current.Remark, expected.Remark) &&
		current.CreatedAt.Equal(expected.CreatedAt) && current.UpdatedAt.Equal(expected.UpdatedAt)
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func ClaimSourceOutbox(ctx context.Context, limit int, lease time.Duration, maxAttempts int, now time.Time) ([]SourceOutboxRecord, error) {
	if global.DB == nil || limit < 1 || limit > 100 || lease <= 0 || maxAttempts < 1 {
		return nil, errors.New("source relay unavailable")
	}
	var claimed []SourceOutboxRecord
	err := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE notification_source_outbox
			SET state = 'failed', lease_token = NULL, lease_until = NULL,
				failure_code = 'retry_exhausted', updated_at = ?
			WHERE state = 'leased' AND lease_until < ? AND attempts >= ?`, now, now, maxAttempts).Error; err != nil {
			return err
		}
		return tx.Raw(`WITH picked AS (
			SELECT id FROM notification_source_outbox
			WHERE ((state IN ('pending', 'retry_wait') AND next_attempt_at <= ?)
				OR (state = 'leased' AND lease_until < ?)) AND attempts < ?
			ORDER BY next_attempt_at, created_at
			FOR UPDATE SKIP LOCKED LIMIT ?
		)
		UPDATE notification_source_outbox AS o
		SET state = 'leased', attempts = o.attempts + 1, lease_token = gen_random_uuid(), lease_until = ?, updated_at = ?
		FROM picked WHERE o.id = picked.id
		RETURNING o.*`, now, now, maxAttempts, limit, now.Add(lease), now).Scan(&claimed).Error
	})
	if err != nil {
		return nil, errors.New("source relay claim failed")
	}
	return claimed, nil
}

func ValidateSourceBridgeStartup(enabled bool, deploymentID string) error {
	if global.DB == nil || (enabled && deploymentID == "") {
		return errors.New("source bridge startup unavailable")
	}
	var tableCount int
	if err := global.DB.Session(&gorm.Session{Logger: logger.Discard}).Raw(`SELECT count(*) FROM unnest(ARRAY[
		to_regclass('notification_source_group_routes'),
		to_regclass('notification_source_group_route_revisions'),
		to_regclass('notification_source_outbox')]) AS t(rel) WHERE rel IS NOT NULL`).Scan(&tableCount).Error; err != nil {
		return errors.New("source bridge startup unavailable")
	}
	if tableCount == 0 && !enabled {
		return nil
	}
	if tableCount != 3 {
		return errors.New("source bridge schema unavailable")
	}
	var encoreRoutes int64
	if err := global.DB.Session(&gorm.Session{Logger: logger.Discard}).Raw(`SELECT count(*) FROM notification_source_group_routes WHERE engine = 'encore'`).Scan(&encoreRoutes).Error; err != nil {
		return errors.New("source bridge startup unavailable")
	}
	if encoreRoutes > 0 && (!enabled || deploymentID == "") {
		return errors.New("existing source routes require relay configuration")
	}
	if enabled {
		var mismatchedRoutes int64
		err := global.DB.Session(&gorm.Session{Logger: logger.Discard}).Raw(`SELECT count(*) FROM notification_source_group_routes
			WHERE source_deployment_id <> ?`, deploymentID).Scan(&mismatchedRoutes).Error
		if err != nil || mismatchedRoutes != 0 {
			return errors.New("source bridge deployment identity mismatch")
		}
	}
	return nil
}

type SourceAttemptResult struct {
	StatusCode int
	Accepted   bool
	RetryAfter time.Duration
	SafeCode   string
}

func FinishSourceOutboxAttempt(ctx context.Context, record SourceOutboxRecord, result SourceAttemptResult, now time.Time, maxAttempts int) error {
	if global.DB == nil || record.ID == "" || record.LeaseToken == nil {
		return errors.New("source relay update failed")
	}
	state := "retry_wait"
	var next time.Time
	var handedAt *time.Time
	var failure *string
	if result.Accepted && result.StatusCode == 202 {
		state = "handed_off"
		handedAt = &now
	} else if result.StatusCode == 401 || result.StatusCode == 403 || result.StatusCode == 422 || result.StatusCode == 400 {
		state = "blocked"
		failure = safeSourceFailure(result.SafeCode, "target_rejected")
	} else if record.Attempts >= maxAttempts {
		state = "failed"
		failure = safeSourceFailure(result.SafeCode, "retry_exhausted")
	} else {
		backoff := result.RetryAfter
		if backoff < time.Second {
			shift := record.Attempts - 1
			if shift < 0 {
				shift = 0
			}
			if shift > 8 {
				shift = 8
			}
			backoff = time.Second * time.Duration(1<<shift)
		}
		if backoff > 15*time.Minute {
			backoff = 15 * time.Minute
		}
		next = now.Add(backoff)
		failure = safeSourceFailure(result.SafeCode, "relay_retry")
	}
	update := global.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Model(&SourceOutboxRecord{}).
		Where("id = ? AND state = 'leased' AND lease_token = ?", record.ID, *record.LeaseToken).
		Updates(map[string]interface{}{"state": state, "next_attempt_at": next, "lease_token": nil, "lease_until": nil, "handed_off_at": handedAt, "failure_code": failure, "updated_at": now})
	if update.Error != nil || update.RowsAffected != 1 {
		return errors.New("source relay update failed")
	}
	return nil
}

func safeSourceFailure(got, fallback string) *string {
	switch got {
	case "target_rejected", "authentication_failed", "source_scope_mismatch", "invalid_source_event", "relay_retry", "retry_exhausted", "unexpected_response":
		return &got
	default:
		return &fallback
	}
}
