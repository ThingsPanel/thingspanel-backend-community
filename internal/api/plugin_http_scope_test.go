package api

import (
	"net/http/httptest"
	"testing"

	"project/internal/middleware"
	"project/internal/model"
	"project/internal/plugininstall"
	"project/internal/query"
	"project/pkg/global"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSharedHTTPInstallationScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Device{}, &model.DeviceConfig{}, &plugininstall.Installation{}); err != nil {
		t.Fatal(err)
	}
	old := global.DB
	global.DB = db
	query.SetDefault(db)
	t.Cleanup(func() {
		global.DB = old
		if old != nil {
			query.SetDefault(old)
		}
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
	proto, typ := "HTTP", "1"
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		cfg := model.DeviceConfig{ID: tenant, Name: tenant, TenantID: tenant, ProtocolType: &proto, DeviceType: typ}
		if err = db.Create(&cfg).Error; err != nil {
			t.Fatal(err)
		}
		d := model.Device{ID: tenant, DeviceNumber: tenant, TenantID: tenant, DeviceConfigID: &cfg.ID, IsEnabled: "enabled", ActivateFlag: "active"}
		if err = db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	row := plugininstall.Installation{ID: "install", TenantID: "", PluginID: "http-adapter", Generation: 2, State: plugininstall.StateEnabled, Desired: plugininstall.DesiredEnabled}
	if err = db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	id := &middleware.PluginRuntimeIdentity{TenantID: "", PluginInstance: "http-adapter", ServiceIdentifier: "HTTP", Generation: 2}
	check := func(device string, identity *middleware.PluginRuntimeIdentity, want bool) {
		t.Helper()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(middleware.PluginRuntimeIdentityKey, identity)
		if got := pluginMayReadDevice(c, model.GetDeviceConfigReq{DeviceId: device}); got != want {
			t.Fatalf("device %s allowed=%v want %v", device, got, want)
		}
	}
	check("tenant-a", id, true)
	check("tenant-b", id, true)
	copyID := *id
	copyID.PluginInstance = "another-instance"
	check("tenant-a", &copyID, false)
	copyID = *id
	copyID.Generation = 1
	check("tenant-a", &copyID, false)
	db.Model(&model.DeviceConfig{}).Where("id = ?", "tenant-b").Update("protocol_type", "MQTT")
	check("tenant-b", id, false)
	db.Model(&model.Device{}).Where("id = ?", "tenant-a").Update("is_enabled", "disabled")
	check("tenant-a", id, false)
	db.Model(&model.Device{}).Where("id = ?", "tenant-a").Update("is_enabled", "enabled")
	list, err := sharedHTTPDeviceList(model.GetDevicesByProtocolPluginReq{PageReq: model.PageReq{Page: 1, PageSize: 10}})
	if err != nil || list.Total != 1 || len(list.List) != 1 || list.List[0].ID != "tenant-a" {
		t.Fatalf("filtered list: %+v %v", list, err)
	}
	for _, state := range []string{plugininstall.StateInstalled, plugininstall.StateFailed, plugininstall.StateUninstalled} {
		db.Model(&row).Updates(map[string]interface{}{"state": state, "desired": plugininstall.DesiredDisabled})
		check("tenant-a", id, false)
		if !legacyHTTPClaimed("HTTP") {
			t.Fatal("managed HTTP must not downgrade to legacy")
		}
	}
	if legacyHTTPClaimed("ModbusTCP") {
		t.Fatal("other protocols must stay compatible")
	}
}
