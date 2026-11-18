package api

import (
	"testing"

	"project/internal/middleware"
	"project/internal/model"
	"project/internal/plugininstall"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestManagedModbusGatewayScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&model.Device{}, &model.DeviceConfig{}, &plugininstall.Installation{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE device_topic_mappings (device_config_id text, enabled boolean)").Error; err != nil {
		t.Fatal(err)
	}
	insert := func(id, tenant, protocol, kind, parent, enabled string) {
		t.Helper()
		cfg := model.DeviceConfig{ID: id, Name: id, TenantID: tenant, DeviceType: kind, ProtocolType: &protocol}
		if err := db.Create(&cfg).Error; err != nil {
			t.Fatal(err)
		}
		row := model.Device{ID: id, TenantID: tenant, DeviceNumber: id, Voucher: "voucher-" + id,
			DeviceConfigID: &cfg.ID, ParentID: &parent, IsEnabled: enabled, ActivateFlag: "active"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	insert("gateway", "tenant-a", plugininstall.ModbusTCP, "2", "", "enabled")
	insert("rtu-gateway", "tenant-b", plugininstall.ModbusRTU, "2", "", "enabled")
	insert("same-tenant", "tenant-a", plugininstall.ModbusTCP, "3", "gateway", "enabled")
	insert("other-tenant", "tenant-b", plugininstall.ModbusTCP, "3", "gateway", "enabled")
	insert("other-protocol", "tenant-a", plugininstall.ModbusRTU, "3", "gateway", "enabled")
	insert("disabled", "tenant-a", plugininstall.ModbusTCP, "3", "gateway", "disabled")
	insert("http", "tenant-a", "HTTP", "2", "", "enabled")
	gateway, err := managedModbusGateway(db, model.GetDeviceConfigReq{Voucher: "voucher-gateway"})
	if err != nil || gateway.ID != "gateway" {
		t.Fatalf("lookup TCP gateway: %+v %v", gateway, err)
	}
	if _, err := managedModbusGateway(db, model.GetDeviceConfigReq{DeviceId: "rtu-gateway"}); err != nil {
		t.Fatalf("lookup RTU gateway: %v", err)
	}
	for _, id := range []string{"other-tenant", "same-tenant", "http"} {
		if _, err := managedModbusGateway(db, model.GetDeviceConfigReq{DeviceId: id}); err == nil {
			t.Fatalf("unexpected gateway match: %s", id)
		}
	}
	children, err := managedModbusChildren(db, gateway)
	if err != nil || len(children) != 1 || children[0].ID != "same-tenant" {
		t.Fatalf("child scope: %+v %v", children, err)
	}
	row := plugininstall.Installation{ID: "install", PluginID: plugininstall.ModbusPluginID, Generation: 2,
		Desired: plugininstall.DesiredEnabled, State: plugininstall.StateEnabled}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	id := &middleware.PluginRuntimeIdentity{PluginInstance: plugininstall.ModbusPluginID,
		ServiceIdentifier: plugininstall.ModbusServiceIdentifier, Generation: 2}
	if !sharedModbusInstallationLive(db, id) {
		t.Fatal("current enabled generation must be live")
	}
	id.Generation--
	if sharedModbusInstallationLive(db, id) {
		t.Fatal("stale generation must be denied")
	}
	id.Generation++
	if err := db.Model(&row).Update("desired", plugininstall.DesiredDisabled).Error; err != nil {
		t.Fatal(err)
	}
	if sharedModbusInstallationLive(db, id) {
		t.Fatal("disabled generation must be denied")
	}
}
