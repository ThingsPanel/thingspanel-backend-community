package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"os"
	"path/filepath"
	"project/internal/dal"
	"project/internal/model"
	"sync"
	"testing"
)

func TestNativeRuntimeRegistrationTenantOwnershipAndRetry(t *testing.T) {
	db := automationPG(t)
	if err := db.Exec("CREATE TABLE users (tenant_id varchar(36)); INSERT INTO users VALUES ('63a4b529'),('other')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&dal.DeviceIDReservation{}); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "018_native_runtime_registrations.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Exec(string(migration)).Error; err != nil {
			t.Fatal(err)
		}
	}
	lifecycle, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "019_native_runtime_credential_lifecycle.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Exec(string(lifecycle)).Error; err != nil {
			t.Fatal(err)
		}
	}
	tenant := nativeTenantIdentity("63a4b529")
	ctx := context.Background()
	in := NativeRuntimeRegistration{EventID: uuid.NewString(), Change: "activation", DeviceID: uuid.NewString(), CredentialID: uuid.NewString(), DeviceNumber: "native-test", Name: "native", Voucher: `{"username":"fresh-secret"}`}
	for i := 0; i < 2; i++ {
		if err := RegisterNativeRuntime(ctx, db, tenant, "dep", in); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- RegisterNativeRuntime(ctx, db, tenant, "dep", in) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var device model.Device
	if err := db.First(&device, "id=?", in.DeviceID).Error; err != nil {
		t.Fatal(err)
	}
	if device.TenantID != "63a4b529" || device.DeviceConfigID != nil || device.Voucher != in.Voucher {
		t.Fatal("projection must preserve source tenant and must not invent template")
	}
	for _, attempt := range []struct{ tenant, dep string }{{nativeTenantIdentity("other"), "dep"}, {tenant, "other-dep"}} {
		if err := RegisterNativeRuntime(ctx, db, attempt.tenant, attempt.dep, in); !errors.Is(err, ErrNativeRegistrationConflict) {
			t.Fatalf("ownership changed: %v", err)
		}
	}
	first := in
	rotated := in
	rotated.EventID = uuid.NewString()
	rotated.Change = "rotation"
	rotated.ReplacementCredentialID = uuid.NewString()
	rotated.Voucher = `{"username":"rotated-secret"}`
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", rotated); err != nil {
		t.Fatal("rotation", err)
	}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", rotated); err != nil {
		t.Fatal("rotation retry", err)
	}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", first); err != nil {
		t.Fatal("old event acknowledgement", err)
	}
	var after model.Device
	db.First(&after, "id=?", in.DeviceID)
	if after.Voucher != rotated.Voucher {
		t.Fatal("old activation replay rewrote rotated voucher")
	}
	stale := rotated
	stale.EventID = uuid.NewString()
	stale.ReplacementCredentialID = uuid.NewString()
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", stale); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatal("stale rotation accepted", err)
	}
	revoke := rotated
	revoke.EventID = uuid.NewString()
	revoke.Change = "revocation"
	revoke.CredentialID = rotated.ReplacementCredentialID
	revoke.ReplacementCredentialID = ""
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", revoke); err != nil {
		t.Fatal("revoke", err)
	}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", revoke); err != nil {
		t.Fatal("revoke retry", err)
	}
	reused := first
	reused.EventID = uuid.NewString()
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", reused); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatal("retired credential reactivated", err)
	}
	newActivation := first
	newActivation.EventID = uuid.NewString()
	newActivation.CredentialID = uuid.NewString()
	newActivation.Voucher = `{"username":"new-after-revoke"}`
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", newActivation); err != nil {
		t.Fatal("fresh activation after revoke", err)
	}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", revoke); err != nil {
		t.Fatal("old revocation ack", err)
	}
	after = model.Device{}
	db.First(&after, "id=?", in.DeviceID)
	if after.Voucher != newActivation.Voucher {
		t.Fatal("old revoke replay disabled fresh credential")
	}
	conflicting := newActivation
	conflicting.Voucher = `{"username":"same-event-different-payload"}`
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", conflicting); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatal("event payload mutation accepted", err)
	}
	legacy := model.Device{ID: uuid.NewString(), TenantID: "63a4b529", DeviceNumber: "user-device", Voucher: `{"username":"existing"}`}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	in.DeviceID = legacy.ID
	in.DeviceNumber = legacy.DeviceNumber
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", in); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("existing user device overwritten: %v", err)
	}
	var preserved model.Device
	db.First(&preserved, "id=?", legacy.ID)
	if preserved.Voucher != legacy.Voucher {
		t.Fatal("existing voucher overwritten")
	}
}
func TestNativeTenantIdentityPreservesUUID(t *testing.T) {
	id := uuid.NewString()
	if nativeTenantIdentity(id) != id || nativeTenantIdentity("63a4b529") == nativeTenantIdentity("other") {
		t.Fatal("tenant mapping collision")
	}
}

func TestNativeRuntimeCacheInvalidationRequiresRedisAcknowledgement(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("isolated Redis not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	ctx := context.Background()
	id := "qa-native-cache-" + uuid.NewString()
	other := id + "-other"
	defer client.Del(ctx, id, other)
	if err := client.Set(ctx, id, "stale-voucher", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, other, "unrelated", 0).Err(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ClearNativeRuntimeDeviceCache(ctx, client, id); err != nil {
			t.Fatal(err)
		}
	}
	if client.Exists(ctx, id).Val() != 0 || client.Get(ctx, other).Val() != "unrelated" {
		t.Fatal("wrong cache entry removed")
	}
	if err := ClearNativeRuntimeDeviceCache(ctx, nil, id); err == nil {
		t.Fatal("missing Redis falsely confirmed")
	}
	client.Close()
	if err := ClearNativeRuntimeDeviceCache(ctx, client, id); err == nil {
		t.Fatal("failed Redis falsely confirmed")
	}
}
