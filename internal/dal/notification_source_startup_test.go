package dal

import (
	"context"
	"testing"
)

func TestValidateSourceBridgeStartupAllowsOnlyAbsentOrCompleteSchemaWhenDisabled(t *testing.T) {
	t.Run("old install has no source schema", func(t *testing.T) {
		db := openNotificationSourceFixture(t)
		if err := db.Exec(`DROP TABLE notification_source_outbox, notification_source_group_route_revisions, notification_source_group_routes`).Error; err != nil {
			t.Fatal("remove source schema from isolated fixture")
		}
		if err := ValidateSourceBridgeStartup(false, ""); err != nil {
			t.Fatalf("unconfigured old installation should start: %v", err)
		}
	})
	t.Run("partial source schema fails closed", func(t *testing.T) {
		db := openNotificationSourceFixture(t)
		if err := db.Exec(`DROP TABLE notification_source_outbox`).Error; err != nil {
			t.Fatal("make isolated source schema partial")
		}
		if err := ValidateSourceBridgeStartup(false, ""); err == nil {
			t.Fatal("disabled bridge accepted a partial source schema")
		}
	})
	t.Run("enabled requires deployment identity", func(t *testing.T) {
		openNotificationSourceFixture(t)
		if err := ValidateSourceBridgeStartup(true, ""); err == nil {
			t.Fatal("enabled bridge accepted missing deployment identity")
		}
	})
	t.Run("enabled requires the full source schema", func(t *testing.T) {
		db := openNotificationSourceFixture(t)
		if err := db.Exec(`DROP TABLE notification_source_outbox, notification_source_group_route_revisions, notification_source_group_routes`).Error; err != nil {
			t.Fatal("remove source schema from isolated fixture")
		}
		if err := ValidateSourceBridgeStartup(true, "deployment-required"); err == nil {
			t.Fatal("enabled bridge accepted a missing source schema")
		}
	})
	t.Run("disabled bridge cannot own a route already switched to Encore", func(t *testing.T) {
		ctx := context.Background()
		openNotificationSourceFixture(t)
		key := SourceRouteKey{DeploymentID: "deploy-disabled-route", TenantID: "tenant-disabled-route", LegacyGroup: "legacy-disabled-route"}
		if err := SwitchSourceRoute(ctx, key, 0, 1, "native-disabled-route", "projection-disabled-001"); err != nil {
			t.Fatal("create Encore-owned route in isolated fixture")
		}
		if err := ValidateSourceBridgeStartup(false, ""); err == nil {
			t.Fatal("disabled bridge accepted an existing Encore-owned route")
		}
	})
}
