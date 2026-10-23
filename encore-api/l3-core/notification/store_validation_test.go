package notification

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidationResolversAreTenantScopedAndDecryptOnlyRequestedVersion(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-validation")
	instance, err := store.GetInstance(ctx, "tenant-validation", instanceID)
	if err != nil {
		t.Fatalf("get current instance: %v", err)
	}
	config, err := store.ResolveInstanceConfig(ctx, "tenant-validation", instanceID, instance.ConfigVersion)
	if err != nil {
		t.Fatalf("resolve immutable instance config: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil || decoded["region"] != "test" || decoded["token"] != "fixture-instance-secret" {
		t.Fatalf("resolved validation config=%s err=%v", config, err)
	}
	if _, err := store.ResolveInstanceConfig(ctx, "tenant-other", instanceID, instance.ConfigVersion); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("cross-tenant config resolver=%v", err)
	}
	runtime, err := store.ResolveRegistrationRuntime(ctx, "tenant-validation", instance.PluginRegistrationID)
	if err != nil || runtime.AuthSecret != "fixture-auth-value" || runtime.Origin != "https://fixture.example.test" {
		t.Fatalf("resolve granted plugin runtime=(%+v,%v)", runtime, err)
	}
	if _, err := store.ResolveRegistrationRuntime(ctx, "tenant-other", instance.PluginRegistrationID); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("ungranted plugin runtime resolver=%v", err)
	}
}

func TestExpireQueuedArchivesOnlyPastDeadlineAndNeverClaimsExpired(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-expiration")
	accepted, err := store.AcceptSubmission(ctx, "tenant-expiration", "expiry-submit-001", strings.Repeat("e", 64), fixtureSubmission(instanceID, "expiry@example.test"))
	if err != nil || len(accepted.DeliveryIDs) != 1 {
		t.Fatalf("accept expiry fixture=(%+v,%v)", accepted, err)
	}
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `UPDATE notification_deliveries SET expires_at=$2 WHERE tenant_id=$1 AND id=$3`, "tenant-expiration", now.Add(-time.Second), accepted.DeliveryIDs[0]); err != nil {
		t.Fatal("expire queued fixture")
	}
	count, err := store.ExpireQueued(ctx, now, 10)
	if err != nil || count != 1 {
		t.Fatalf("expire queued=(%d,%v)", count, err)
	}
	claims, err := store.ClaimDeliveries(ctx, 10, time.Minute)
	if err != nil || len(claims) != 0 {
		t.Fatalf("claim after expiry=(%d,%v)", len(claims), err)
	}
	delivery, err := store.GetDelivery(ctx, "tenant-expiration", accepted.DeliveryIDs[0])
	if err != nil || delivery.DispatchStatus != "failed" || delivery.DeliveryStatus != "failed" {
		t.Fatalf("expired delivery status=(%+v,%v)", delivery, err)
	}
	var code string
	if err := db.QueryRowContext(ctx, `SELECT error_code FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, "tenant-expiration", accepted.DeliveryIDs[0]).Scan(&code); err != nil || code != "expired" {
		t.Fatalf("expired outcome code=(%q,%v)", code, err)
	}
}
