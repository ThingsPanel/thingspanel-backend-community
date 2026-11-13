package api

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"
	"project/internal/middleware"
	"project/internal/model"
	"project/internal/plugininstall"
	"project/pkg/global"
)

func sharedHTTPDeviceList(req model.GetDevicesByProtocolPluginReq) (*model.GetDevicesByProtocolPluginRsp, error) {
	result := &model.GetDevicesByProtocolPluginRsp{List: []model.DeviceConfigForProtocolPlugin{}}
	base := func() *gorm.DB {
		return global.DB.Table("devices AS d").Joins("JOIN device_configs AS c ON c.id = d.device_config_id AND c.tenant_id = d.tenant_id").Where("c.protocol_type = ? AND c.device_type = ? AND d.is_enabled = ? AND d.activate_flag = ? AND (d.service_access_id IS NULL OR d.service_access_id = '')", "HTTP", "1", "enabled", "active")
	}
	if err := base().Count(&result.Total).Error; err != nil {
		return nil, err
	}
	var rows []struct {
		ID, Voucher, DeviceNumber      string
		Config, ProtocolConfigTemplate *string
	}
	err := base().Select("d.id, d.voucher, d.device_number, d.protocol_config AS config, c.protocol_config AS protocol_config_template").Order("d.id").Limit(req.PageSize).Offset((req.Page - 1) * req.PageSize).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		item := model.DeviceConfigForProtocolPlugin{ID: row.ID, Voucher: row.Voucher, DeviceNumber: row.DeviceNumber, DeviceType: "1", ProtocolType: "HTTP", Config: map[string]interface{}{}, ProtocolConfigTemplate: map[string]interface{}{}, SubDivices: []model.SubDeviceConfigForProtocolPlugin{}}
		if row.Config != nil {
			_ = json.Unmarshal([]byte(*row.Config), &item.Config)
		}
		if row.ProtocolConfigTemplate != nil {
			_ = json.Unmarshal([]byte(*row.ProtocolConfigTemplate), &item.ProtocolConfigTemplate)
		}
		result.List = append(result.List, item)
	}
	return result, nil
}

// A signed catalog entry installed by SYS_ADMIN delegates the HTTP protocol to
// this particular shared instance. An empty tenant alone is never sufficient.
func sharedHTTPInstallationLive(id *middleware.PluginRuntimeIdentity) bool {
	if id == nil || id.TenantID != "" || id.PluginInstance != "http-adapter" || id.ServiceIdentifier != "HTTP" || global.DB == nil {
		return false
	}
	var n int64
	err := global.DB.Model(&plugininstall.Installation{}).Where("tenant_id = ? AND plugin_id = ? AND generation = ? AND desired = ? AND state IN ?", "", "http-adapter", id.Generation, plugininstall.DesiredEnabled, []string{plugininstall.StateInstalling, plugininstall.StateEnabled}).Count(&n).Error
	return err == nil && n == 1
}

// Once enrolled, an HTTP installation cannot be reached through the old
// headerless compatibility route, including after disable or uninstall.
func legacyHTTPClaimed(service string) bool {
	if !strings.EqualFold(strings.TrimSpace(service), "HTTP") || global.DB == nil {
		return false
	}
	var n int64
	err := global.DB.Model(&plugininstall.Installation{}).Where("tenant_id = ? AND plugin_id = ? AND generation > 0", "", "http-adapter").Count(&n).Error
	return err != nil || n > 0
}

func sharedHTTPDeviceAllowed(id *middleware.PluginRuntimeIdentity, d *model.Device) bool {
	if !sharedHTTPInstallationLive(id) || d == nil || d.IsEnabled != "enabled" || d.ActivateFlag != "active" || d.DeviceConfigID == nil || (d.ServiceAccessID != nil && *d.ServiceAccessID != "") {
		return false
	}
	var n int64
	err := global.DB.Model(&model.DeviceConfig{}).Where("id = ? AND tenant_id = ? AND protocol_type = ? AND device_type = ?", *d.DeviceConfigID, d.TenantID, "HTTP", "1").Count(&n).Error
	return err == nil && n == 1
}
