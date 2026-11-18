package api

import (
	"encoding/json"
	"strings"

	"project/internal/middleware"
	"project/internal/model"
	"project/internal/plugininstall"
	"project/pkg/global"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func modbusRuntimeIdentity(c *gin.Context) *middleware.PluginRuntimeIdentity {
	value, _ := c.Get(middleware.PluginRuntimeIdentityKey)
	identity, _ := value.(*middleware.PluginRuntimeIdentity)
	if identity == nil || identity.PluginInstance != plugininstall.ModbusPluginID || legacyPluginRequest(c) {
		return nil
	}
	return identity
}

func sharedModbusInstallationLive(db *gorm.DB, id *middleware.PluginRuntimeIdentity) bool {
	if db == nil || id.TenantID != "" || id.ServiceIdentifier != plugininstall.ModbusServiceIdentifier {
		return false
	}
	var count int64
	err := db.Model(&plugininstall.Installation{}).
		Where("tenant_id = ? AND plugin_id = ? AND generation = ? AND desired = ? AND state IN ?", "",
			plugininstall.ModbusPluginID, id.Generation, plugininstall.DesiredEnabled,
			[]string{plugininstall.StateInstalling, plugininstall.StateEnabled}).Count(&count).Error
	return err == nil && count == 1
}

func legacyModbusClaimed(protocol string) bool {
	protocol = strings.ToUpper(strings.TrimSpace(protocol))
	if global.DB == nil || !plugininstall.IsModbusProtocol(protocol) {
		return false
	}
	var row model.ServicePlugin
	if err := global.DB.Select("service_config").Where("service_identifier = ?", protocol).Limit(1).Find(&row).Error; err != nil {
		return true
	}
	if row.ServiceConfig == nil {
		return false
	}
	var config struct {
		ManagedPluginID string `json:"managed_plugin_id"`
	}
	if err := json.Unmarshal([]byte(*row.ServiceConfig), &config); err != nil {
		return true
	}
	return config.ManagedPluginID == plugininstall.ModbusPluginID
}

func modbusDevices(db *gorm.DB, deviceType string) *gorm.DB {
	return db.Table("devices AS d").Select("d.*").
		Joins("JOIN device_configs c ON c.id = d.device_config_id AND c.tenant_id = d.tenant_id").
		Where("c.device_type = ? AND d.is_enabled = ? AND d.activate_flag = ? AND (d.service_access_id IS NULL OR d.service_access_id = '')",
			deviceType, "enabled", "active").
		Where("(d.access_way IS NULL OR d.access_way <> ?) AND NOT EXISTS (SELECT 1 FROM device_topic_mappings m WHERE CAST(m.device_config_id AS text) = CAST(c.id AS text) AND m.enabled = ?)",
			"B", true)
}

func managedModbusGateway(db *gorm.DB, req model.GetDeviceConfigReq) (*model.Device, error) {
	gateways := modbusDevices(db, "2").Where("c.protocol_type IN ?", []string{plugininstall.ModbusTCP, plugininstall.ModbusRTU})
	switch {
	case req.DeviceId != "":
		gateways = gateways.Where("d.id = ?", req.DeviceId)
	case req.Voucher != "":
		gateways = gateways.Where("d.voucher = ?", req.Voucher)
	case req.DeviceNumber != "":
		gateways = gateways.Where("d.device_number = ?", req.DeviceNumber)
	default:
		return nil, gorm.ErrRecordNotFound
	}
	var rows []model.Device
	err := gateways.Limit(2).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return &rows[0], nil
}

func managedModbusChildren(db *gorm.DB, gateway *model.Device) ([]*model.Device, error) {
	var children []*model.Device
	err := modbusDevices(db, "3").
		Joins("JOIN device_configs gateway_config ON gateway_config.id = ? AND gateway_config.tenant_id = d.tenant_id", *gateway.DeviceConfigID).
		Where("d.parent_id = ? AND d.tenant_id = ? AND c.protocol_type = gateway_config.protocol_type", gateway.ID, gateway.TenantID).
		Find(&children).Error
	return children, err
}
