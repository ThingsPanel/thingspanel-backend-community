package dal

import (
	"context"
	"errors"
	"testing"
	"time"

	"project/internal/model"
	"project/pkg/global"

	"gorm.io/gorm"
)

func createNotificationGroupFixture(t *testing.T) {
	t.Helper()
	err := global.DB.Exec(`CREATE TABLE notification_groups (
		id text PRIMARY KEY, name text NOT NULL, notification_type text NOT NULL,
		status text NOT NULL, notification_config text, description text, tenant_id text NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, remark text)`).Error
	if err != nil {
		t.Fatal("could not create notification group fixture")
	}
	err = global.DB.Exec(`INSERT INTO notification_groups
		(id, name, notification_type, status, tenant_id, created_at, updated_at)
		VALUES ('group-a', 'legacy name', 'EMAIL', 'OPEN', 'tenant-a', now(), now())`).Error
	if err != nil {
		t.Fatal("could not insert notification group fixture")
	}
}

func TestLegacyGroupWritesAreTenantScopedAndReadOnlyAfterEncoreSwitch(t *testing.T) {
	db := openNotificationSourceFixture(t)
	createNotificationGroupFixture(t)
	ctx := context.Background()
	key := SourceRouteKey{DeploymentID: "deploy-group-guard", TenantID: "tenant-a", LegacyGroup: "group-a"}

	if _, err := GetNotificationGroupByTenantID("group-a", "tenant-b"); !errors.Is(err, ErrNotificationGroupNotFound) {
		t.Fatalf("cross-tenant read should look missing, got %v", err)
	}
	group, err := GetNotificationGroupByTenantID("group-a", "tenant-a")
	if err != nil {
		t.Fatalf("tenant read failed: %v", err)
	}
	group.Name = "edited before migration"
	if err := UpdateNotificationGroupForTenant(ctx, group, key, true); err != nil {
		t.Fatalf("legacy update before route switch failed: %v", err)
	}
	if err := DeleteNotificationGroupForTenant(ctx, "group-a", "tenant-b", SourceRouteKey{DeploymentID: key.DeploymentID, TenantID: "tenant-b", LegacyGroup: "group-a"}, true); !errors.Is(err, ErrNotificationGroupNotFound) {
		t.Fatalf("cross-tenant delete should look missing, got %v", err)
	}

	if err := SwitchSourceRoute(ctx, key, 0, 1, "encore-group-a", "projection-key-guard-001"); err != nil {
		t.Fatalf("route switch failed: %v", err)
	}
	group.Name = "must not persist"
	if err := UpdateNotificationGroupForTenant(ctx, group, key, true); !errors.Is(err, ErrNotificationGroupReadOnly) {
		t.Fatalf("legacy update after route switch should be read-only, got %v", err)
	}
	if err := DeleteNotificationGroupForTenant(ctx, "group-a", "tenant-a", key, true); !errors.Is(err, ErrNotificationGroupReadOnly) {
		t.Fatalf("legacy delete after route switch should be read-only, got %v", err)
	}
	var persisted string
	if err := db.Raw(`SELECT name FROM notification_groups WHERE id = 'group-a'`).Row().Scan(&persisted); err != nil || persisted != "edited before migration" {
		t.Fatal("failed or read-only edit changed the legacy row")
	}
}

func TestLegacyGroupEditSharesTupleAdvisoryLockWithRouteSwitch(t *testing.T) {
	openNotificationSourceFixture(t)
	createNotificationGroupFixture(t)
	ctx := context.Background()
	key := SourceRouteKey{DeploymentID: "deploy-group-race", TenantID: "tenant-a", LegacyGroup: "group-a"}
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_, _ = WithLockedSourceRoute(ctx, key, func(_ *gorm.DB, _ SourceRouteSnapshot) error {
			close(locked)
			<-release
			return nil
		})
		close(done)
	}()
	select {
	case <-locked:
	case <-time.After(3 * time.Second):
		t.Fatal("could not acquire source tuple lock")
	}
	group, err := GetNotificationGroupByTenantID("group-a", "tenant-a")
	if err != nil {
		t.Fatal("could not read fixture group")
	}
	group.Name = "serialized edit"
	editDone := make(chan error, 1)
	go func() { editDone <- UpdateNotificationGroupForTenant(ctx, group, key, true) }()
	select {
	case err := <-editDone:
		t.Fatalf("legacy group update bypassed tuple lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-done
	select {
	case err := <-editDone:
		if err != nil {
			t.Fatalf("serialized legacy update failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("legacy group update did not resume after route lock released")
	}
}

func TestEnabledLegacyGroupWriteFailsClosedWhenRouteSchemaMissing(t *testing.T) {
	openNotificationSourceFixture(t)
	group := &model.NotificationGroup{ID: "group-a", TenantID: "tenant-a", Name: "blocked"}
	err := UpdateNotificationGroupForTenant(context.Background(), group, SourceRouteKey{DeploymentID: "deploy", TenantID: "tenant-a", LegacyGroup: "group-a"}, true)
	if !errors.Is(err, ErrSourceRouteUnavailable) {
		t.Fatalf("enabled bridge must fail closed without route tables, got %v", err)
	}
}
