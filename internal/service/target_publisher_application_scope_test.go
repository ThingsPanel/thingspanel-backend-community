package service

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/brokercommands"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
)

// The MQTT publisher is a first-party system role. The business entrypoint must
// therefore reject a caller from another tenant before preparing or sending.
func TestTargetPublisherApplicationRejectsForeignTenantBeforeEnqueue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:target-publisher-scope?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Device{}, &model.User{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	oldDevice, oldProfile := query.Device, viper.Get("mqtt.input_profile")
	q := query.Use(db)
	query.Device = &q.Device
	viper.Set("mqtt.input_profile", "ingress_v1")
	t.Cleanup(func() { query.Device = oldDevice; viper.Set("mqtt.input_profile", oldProfile) })
	device := model.Device{ID: "foreign-device", TenantID: "tenant-b", DeviceNumber: "foreign-number", Voucher: "fixture", IsEnabled: "enabled", ActivateFlag: "active"}
	if err = db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	value := `{"enabled":true}`
	// No bus, queue, MQTT connection or config is needed: tenant denial precedes them.
	id, err := (&CommandData{}).CommandPutMessageWithResult(context.Background(), "user-a", &model.PutMessageForCommand{DeviceID: device.ID, Identify: "switch", Value: &value}, "1", "tenant-a")
	var businessError *errcode.Error
	if !errors.As(err, &businessError) || id != "" || businessError.CustomMsg != "device not found" {
		t.Fatalf("foreign target did not fail at application tenant boundary: %q %v", id, err)
	}
	status, tenant := "N", "tenant-a"
	if err = db.Create(&model.User{ID: "user-a", TenantID: &tenant, Status: &status, Email: "fixture@example.invalid", Password: "unused"}).Error; err != nil {
		t.Fatal(err)
	}
	row := brokercommands.Row{TenantID: "tenant-b", CallerID: "user-a", Intent: manualCommandIntent}
	if err = ValidateDurableCommandCaller(context.Background(), db, row); !errors.Is(err, brokercommands.ErrRejected) {
		t.Fatalf("forged accepted-row tenant survived dispatcher validation: %v", err)
	}
	// Authorization service failure must not turn the platform transport role into an allow.
	oldEnforcer := global.CasbinEnforcer
	global.CasbinEnforcer = nil
	t.Cleanup(func() { global.CasbinEnforcer = oldEnforcer })
	row.TenantID = tenant
	if err = ValidateDurableCommandCaller(context.Background(), db, row); err == nil {
		t.Fatal("unavailable authorization allowed dispatch")
	}
}
