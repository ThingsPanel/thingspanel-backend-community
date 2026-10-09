package dal

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSourceRouteExplicitRollbackPreservesImmutableHistoryAndOutbox(t *testing.T) {
	db := openNotificationSourceFixture(t)
	ctx := context.Background()
	key := SourceRouteKey{DeploymentID: "deploy-explicit-rollback", TenantID: "tenant-rollback", LegacyGroup: "group-rollback"}
	if err := SwitchSourceRoute(ctx, key, 0, 4, "native-rollback", "projection-rollback-001"); err != nil {
		t.Fatalf("create initial Encore route: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	pendingID := uuid.NewString()
	unknownID := uuid.NewString()
	terminalID := uuid.NewString()
	insertOutbox := func(id, eventID, actionID, idempotencyKey, state string, handedOff *time.Time) {
		t.Helper()
		if err := db.Exec(`INSERT INTO notification_source_outbox
			(id, source_deployment_id, tenant_id, source_event_id, source_action_id, legacy_group_id, notification_group_id,
			 group_revision, idempotency_key, request_body, body_sha256, occurred_at, expires_at, state, attempts, next_attempt_at, handed_off_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 4, ?, ?, ?, ?, ?, ?, 0, ?, ?)`, id, key.DeploymentID, key.TenantID, eventID, actionID,
			key.LegacyGroup, "native-rollback", idempotencyKey, []byte(`{"frozen":true}`), strings.Repeat("a", 64), now, now.Add(time.Hour), state, now, handedOff).Error; err != nil {
			t.Fatal("insert frozen outbox fixture")
		}
	}
	insertOutbox(pendingID, "event-pending", "action-pending", "key-pending-001", "pending", nil)
	insertOutbox(unknownID, "event-unknown", "action-unknown", "key-unknown-001", "retry_wait", nil)
	if err := db.Exec(`UPDATE notification_source_outbox SET failure_code = 'relay_retry' WHERE id = ?`, unknownID).Error; err != nil {
		t.Fatal("mark unknown relay outcome")
	}
	handedOff := now.Add(-time.Minute)
	insertOutbox(terminalID, "event-handed", "action-handed", "key-handed-001", "handed_off", &handedOff)

	if err := SwitchSourceRouteToLegacy(ctx, key, 1); err != nil {
		t.Fatalf("explicit CAS rollback failed: %v", err)
	}
	if err := SwitchSourceRouteToLegacy(ctx, key, 1); !errors.Is(err, ErrSourceRouteConflict) {
		t.Fatalf("stale rollback version should conflict, got %v", err)
	}
	if err := SwitchSourceRoute(ctx, key, 2, 5, "different-native-target", "projection-rebind-001"); !errors.Is(err, ErrSourceRouteConflict) {
		t.Fatalf("source tuple rebound to a different native target: %v", err)
	}

	var route struct {
		Engine        string         `gorm:"column:engine"`
		Current       sql.NullString `gorm:"column:notification_group_id"`
		Bound         sql.NullString `gorm:"column:bound_notification_group_id"`
		GroupRevision int64          `gorm:"column:group_revision"`
		RouteVersion  int64          `gorm:"column:route_version"`
	}
	if err := db.Raw(`SELECT engine, notification_group_id, bound_notification_group_id, group_revision, route_version
		FROM notification_source_group_routes WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ?`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&route).Error; err != nil {
		t.Fatal("read rolled back source route")
	}
	if route.Engine != "legacy" || route.Current.Valid || !route.Bound.Valid || route.Bound.String != "native-rollback" || route.GroupRevision != 0 || route.RouteVersion != 2 {
		t.Fatalf("unexpected rollback route state: %+v", route)
	}
	var revisions, encoreOutbox, unknownRetries int64
	if err := db.Raw(`SELECT count(*) FROM notification_source_group_route_revisions WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND notification_group_id = 'native-rollback'`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&revisions).Error; err != nil || revisions != 1 {
		t.Fatalf("projection revision was not retained: count=%d err=%v", revisions, err)
	}
	if err := db.Raw(`SELECT count(*) FROM notification_source_outbox WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND notification_group_id = 'native-rollback' AND group_revision = 4`, key.DeploymentID, key.TenantID, key.LegacyGroup).Scan(&encoreOutbox).Error; err != nil || encoreOutbox != 3 {
		t.Fatalf("old outbox rows lost their immutable Encore target: count=%d err=%v", encoreOutbox, err)
	}
	if err := db.Raw(`SELECT count(*) FROM notification_source_outbox WHERE id = ? AND state = 'retry_wait' AND failure_code = 'relay_retry'`, unknownID).Scan(&unknownRetries).Error; err != nil || unknownRetries != 1 {
		t.Fatalf("unknown relay outcome was changed during rollback: count=%d err=%v", unknownRetries, err)
	}
	var terminalState string
	if err := db.Raw(`SELECT state FROM notification_source_outbox WHERE id = ?`, terminalID).Row().Scan(&terminalState); err != nil || terminalState != "handed_off" {
		t.Fatalf("terminal outbox outcome changed: state=%q err=%v", terminalState, err)
	}
	claimed, err := ClaimSourceOutbox(ctx, 10, time.Minute, 3, now.Add(time.Second))
	if err != nil || len(claimed) != 2 {
		t.Fatalf("old pending and unknown events were not retained for the Encore relay: %+v err=%v", claimed, err)
	}
	claimedIDs := map[string]bool{}
	for _, row := range claimed {
		claimedIDs[row.ID] = true
		if row.NotificationGroupID != "native-rollback" || row.GroupRevision != 4 || string(row.RequestBody) != `{"frozen":true}` {
			t.Fatalf("old event was rebound to legacy during rollback: %+v", row)
		}
	}
	if !claimedIDs[pendingID] || !claimedIDs[unknownID] {
		t.Fatalf("pending/unknown old event disappeared from Encore relay: %+v", claimedIDs)
	}
	newEventRoute, err := WithLockedSourceRoute(ctx, key, func(_ *gorm.DB, snapshot SourceRouteSnapshot) error { return nil })
	if err != nil || newEventRoute.Engine != "legacy" || newEventRoute.RouteVersion != 2 || newEventRoute.GroupRevision != 0 {
		t.Fatalf("new events did not observe the explicit legacy route: %+v err=%v", newEventRoute, err)
	}
}
