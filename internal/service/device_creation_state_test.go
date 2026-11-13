package service

import (
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/utils"
)

// Exercise the ordinary no-template creation path, rather than seeding an
// already-enabled device as the MQTT deployment smoke originally did.
func TestCreateDevicePersistsEnabledCredentials(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	if err := db.AutoMigrate(&model.Device{}, &model.Group{}); err != nil {
		t.Fatal(err)
	}
	previousQuery := *query.Q
	*query.Q = *query.Use(db)
	t.Cleanup(func() { *query.Q = previousQuery })
	name := "new-device"
	device, err := GroupApp.Device.CreateDevice(model.CreateDeviceReq{Name: &name}, &utils.UserClaims{TenantID: "tenant-create"})
	if err != nil {
		t.Fatal(err)
	}
	var stored model.Device
	if err := db.First(&stored, "id = ?", device.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ActivateFlag != "active" || stored.IsEnabled != "enabled" || stored.TenantID != "tenant-create" {
		t.Fatalf("unexpected persisted state: active=%q enabled=%q tenant=%q", stored.ActivateFlag, stored.IsEnabled, stored.TenantID)
	}
	var voucher model.DeviceVoucher
	if err := json.Unmarshal([]byte(stored.Voucher), &voucher); err != nil {
		t.Fatal(err)
	}
	if voucher.Username == "" || voucher.Password == "" || stored.DeviceNumber == "" {
		t.Fatal("new device lacks MQTT credentials or device number")
	}
}
