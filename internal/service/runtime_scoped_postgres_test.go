package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

func runtimeFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db := brokerEvidenceTestDB(t)
	oldDB, oldRedis := global.DB, global.REDIS
	global.DB, global.REDIS = db, nil
	t.Cleanup(func() { global.DB, global.REDIS = oldDB, oldRedis })
	if err := db.AutoMigrate(&model.ExpectedData{}); err != nil {
		t.Fatal("create expected fixture")
	}
	for _, d := range []model.Device{
		{ID: "device-b", TenantID: "tenant-b", Voucher: "fixture-b", DeviceNumber: "number-b", IsEnabled: "enabled", ActivateFlag: "active"},
		{ID: "parent-a", TenantID: "tenant-a", Voucher: "fixture-parent", DeviceNumber: "number-parent", IsEnabled: "enabled", ActivateFlag: "active"},
	} {
		if err := db.Create(&d).Error; err != nil {
			t.Fatal("seed runtime device")
		}
	}

	return db
}

func TestRuntimeScopedPostgresWritersRejectForeignAndTombstonedTargets(t *testing.T) {
	db := runtimeFixture(t)
	claims := &utils.UserClaims{TenantID: "tenant-a", ID: "caller", Authority: "SYS_ADMIN"}
	ctx := context.Background()
	for _, id := range []string{"device-b", "missing"} {
		if _, err := GroupApp.Device.RuntimeUpdateVoucher(ctx, &model.UpdateDeviceVoucherReq{DeviceID: id, Voucher: "fixture-new"}, claims); err == nil {
			t.Fatal("foreign voucher accepted")
		}
		if err := GroupApp.Device.RuntimeRemoveSubDevice(ctx, id, claims); err == nil {
			t.Fatal("foreign subdevice accepted")
		}
	}
	if err := db.Model(&dal.DeviceIDReservation{}).Where("id=?", "device-a").Update("state", "tombstoned").Error; err != nil {
		t.Fatal(err)
	}
	called := false
	if err := runtimeDeviceTx(ctx, "tenant-a", "device-a", func(*gorm.DB, *model.Device) error { called = true; return nil }); err == nil || called {
		t.Fatal("tombstoned device mutation accepted")
	}
}

func TestRuntimeScopedPostgresVoucherSubdeviceAndExpectedMutations(t *testing.T) {
	db := runtimeFixture(t)
	claims := &utils.UserClaims{TenantID: "tenant-a", ID: "caller"}
	ctx := context.Background()
	if _, err := GroupApp.Device.RuntimeUpdateVoucher(ctx, &model.UpdateDeviceVoucherReq{DeviceID: "device-a", Voucher: "fixture-rotation"}, claims); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := db.Model(&model.Device{}).Where("id=?", "device-a").Update("parent_id", "device-b").Error; err != nil {
		t.Fatal(err)
	}
	if err := GroupApp.Device.RuntimeRemoveSubDevice(ctx, "device-a", claims); err == nil {
		t.Fatal("foreign parent association accepted")
	}
	if err := db.Model(&model.Device{}).Where("id=?", "device-a").Update("parent_id", "parent-a").Error; err != nil {
		t.Fatal(err)
	}
	if err := GroupApp.Device.RuntimeRemoveSubDevice(ctx, "device-a", claims); err != nil {
		t.Fatalf("remove: %v", err)
	}
	var d model.Device
	if err := db.Where("id=?", "device-a").Take(&d).Error; err != nil || d.ParentID != nil || d.Voucher != "fixture-rotation" {
		t.Fatal("authorized mutation not persisted")
	}
	for _, ed := range []model.ExpectedData{
		{ID: "expected-a", DeviceID: "device-a", TenantID: "tenant-a", Status: "pending", SendType: "telemetry", Payload: "{}", CreatedAt: time.Now()},
		{ID: "expected-b", DeviceID: "device-b", TenantID: "tenant-b", Status: "pending", SendType: "telemetry", Payload: "{}", CreatedAt: time.Now()},
		{ID: "expected-misbound", DeviceID: "device-b", TenantID: "tenant-a", Status: "pending", SendType: "telemetry", Payload: "{}", CreatedAt: time.Now()},
	} {
		if err := db.Create(&ed).Error; err != nil {
			t.Fatal("seed expected")
		}
	}
	for _, id := range []string{"expected-b", "expected-misbound"} {
		if err := GroupApp.ExpectedData.RuntimeDelete(ctx, id, claims); err == nil {
			t.Fatal("foreign record delete accepted")
		}
	}
	if err := GroupApp.ExpectedData.RuntimeDelete(ctx, "expected-a", claims); err != nil {
		t.Fatalf("delete scoped expected: %v", err)
	}
	var count int64
	db.Model(&model.ExpectedData{}).Count(&count)
	if count != 2 {
		t.Fatalf("deleted unrelated expected records: remaining=%d", count)
	}
}

func TestRuntimeScopedPostgresFenceBlocksConcurrentTenantTransfer(t *testing.T) {
	db := runtimeFixture(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		done <- runtimeDeviceTx(ctx, "tenant-a", "device-a", func(tx *gorm.DB, _ *model.Device) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			return tx.Model(&model.Device{}).Where("id=? AND tenant_id=?", "device-a", "tenant-a").Update("name", "authorized").Error
		})
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("fence failed: %v", err)
	case <-ctx.Done():
		t.Fatal("fence timed out")
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SET LOCAL lock_timeout='150ms'").Error; e != nil {
			return e
		}
		return tx.Model(&model.Device{}).Where("id=?", "device-a").Update("tenant_id", "tenant-b").Error
	})
	if err == nil {
		t.Fatal("tenant transfer bypassed active mutation fence")
	}
	// A real PostgreSQL lock timeout demonstrates serialization rather than a
	// mock expectation; release the first transaction and require its completion.
	release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("fenced mutation: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("fenced mutation did not finish")
	}
	var d model.Device
	db.Where("id=?", "device-a").Take(&d)
	if d.TenantID != "tenant-a" || d.Name == nil || *d.Name != "authorized" {
		t.Fatal("scope changed during mutation")
	}
}
