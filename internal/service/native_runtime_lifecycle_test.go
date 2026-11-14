package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"project/internal/dal"
	"project/internal/model"
	"testing"
	"time"
)

func TestNativeRuntimeLifecycleOwnershipReplayAndLeaseFence(t *testing.T) {
	db := automationPG(t)
	ctx := context.Background()
	if err := db.Exec("CREATE TABLE users (tenant_id varchar(36)); INSERT INTO users VALUES ('tenant-a')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&dal.DeviceIDReservation{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"018_native_runtime_registrations.sql", "019_native_runtime_credential_lifecycle.sql", "020_native_credential_leases.sql", "021_native_runtime_lifecycle.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(raw)).Error; err != nil {
			t.Fatal(err)
		}
	}
	tenant := nativeTenantIdentity("tenant-a")
	reg := NativeRuntimeRegistration{DeviceID: uuid.NewString(), CredentialID: uuid.NewString(), EventID: uuid.NewString(), Change: "activation", DeviceNumber: "before", Voucher: `{"username":"test-native"}`}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", reg); err != nil {
		t.Fatal(err)
	}
	op := NativeRuntimeLifecycle{DeviceID: reg.DeviceID, EventID: uuid.NewString(), Generation: 2, Kind: "device_renumber", DeviceNumber: "after"}
	for _, identity := range [][2]string{{nativeTenantIdentity("other"), "dep"}, {tenant, "other"}} {
		if err := ApplyNativeRuntimeLifecycle(ctx, db, identity[0], identity[1], op); !errors.Is(err, ErrNativeRegistrationConflict) {
			t.Fatalf("ownership: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", op); err != nil {
			t.Fatal(err)
		}
	}
	changed := op
	changed.DeviceNumber = "tampered"
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", changed); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("digest fence: %v", err)
	}
	changed = op
	changed.EventID = uuid.NewString()
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", changed); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("generation fence: %v", err)
	}
	var owner nativeRuntimeRegistration
	db.First(&owner, "device_id=?", reg.DeviceID)
	if err := db.Exec(`INSERT INTO native_runtime_credential_leases(connection_id,device_id,tenant_id,broker_cell,broker_instance,voucher_sha256,expires_at) VALUES (?,?,?,?,?,?,?)`, "old", reg.DeviceID, "tenant-a", "cell", "broker", owner.VoucherSHA256, time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	// Native deletion carries the current resource version (not version+1).
	del := NativeRuntimeLifecycle{DeviceID: reg.DeviceID, EventID: uuid.NewString(), Generation: op.Generation, Kind: "device_deletion"}
	staleDelete := del
	staleDelete.Generation = op.Generation - 1
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", staleDelete); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("stale deletion accepted: %v", err)
	}
	staleRename := op
	staleRename.EventID = uuid.NewString()
	staleRename.Generation = op.Generation - 1
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", staleRename); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("stale renumber accepted: %v", err)
	}
	if err := db.Exec("CREATE TABLE runtime_history_cascade(device_id text REFERENCES devices(id) ON DELETE CASCADE, value int); CREATE TABLE runtime_history_restrict(device_id text REFERENCES devices(id) ON DELETE RESTRICT, value int)").Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"runtime_history_cascade", "runtime_history_restrict"} {
		if err := db.Exec("INSERT INTO "+table+"(device_id,value) VALUES (?,?)", reg.DeviceID, 42).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", del); err == nil {
		t.Fatal("unrevoked lease acknowledged deletion")
	}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", reg); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("deleting projection revived: %v", err)
	}
	var count int64
	db.Model(&model.Device{}).Where("id=?", reg.DeviceID).Count(&count)
	if count != 1 {
		t.Fatal("deleted before lease confirmation")
	}
	if err := db.Exec("UPDATE native_runtime_credential_leases SET revoked_ack_at=now() WHERE connection_id='old'").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", del); err != nil {
			t.Fatal(err)
		}
	}
	db.Model(&model.Device{}).Where("id=?", reg.DeviceID).Count(&count)
	if count != 1 {
		t.Fatal("runtime tombstone lost")
	}
	var device model.Device
	if err := db.First(&device, "id=?", reg.DeviceID).Error; err != nil || device.IsEnabled != "disabled" {
		t.Fatalf("runtime tombstone active: %v", err)
	}
	for _, table := range []string{"runtime_history_cascade", "runtime_history_restrict"} {
		var value int
		if err := db.Table(table).Select("value").Where("device_id=?", reg.DeviceID).Scan(&value).Error; err != nil || value != 42 {
			t.Fatalf("history lost: %s %v", table, err)
		}
	}
	var reservation dal.DeviceIDReservation
	db.First(&reservation, "id=?", reg.DeviceID)
	if reservation.State != "tombstoned" {
		t.Fatal("identity not reserved")
	}
}
