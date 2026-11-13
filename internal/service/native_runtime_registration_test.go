package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
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
	tenant := nativeTenantIdentity("63a4b529")
	ctx := context.Background()
	in := NativeRuntimeRegistration{DeviceID: uuid.NewString(), CredentialID: uuid.NewString(), DeviceNumber: "native-test", Name: "native", Voucher: `{"username":"fresh-secret"}`}
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
