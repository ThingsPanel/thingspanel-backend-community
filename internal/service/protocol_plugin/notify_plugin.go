package protocolplugin

import (
	"encoding/json"
	"fmt"
	"project/internal/dal"
	"project/third_party/others/http_client"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"
)

// 设备配置更新后主动断开设备连接
func DeviceConfigUpdateAndDisconnect(deviceConfigID string, protocolType string, deviceType string) error {

	// 根据协议类型获取协议信息
	servicePlugin, err := dal.GetServicePluginByServiceIdentifier(protocolType)
	if err != nil {
		return err
	}
	// 获取协议插件host:
	_, host, err := dal.GetServicePluginHttpAddressByID(servicePlugin.ID)
	if err != nil {
		return err
	}
	// 通知所有相关网关断开连接
	if deviceType == "3" {
		// 获取已绑定网关的关联的子设备列表
		deviceIDs, err := dal.GetGatewayDevicesBySubDeviceConfigID(deviceConfigID)
		if err != nil {
			return err
		}
		// 断开设备连接
		for _, deviceID := range deviceIDs {
			DisconnectDevice(deviceID, host)
		}
	} else if deviceType == "1" || deviceType == "2" {
		// 根据设备配置ID获取设备列表
		devices, err := dal.GetDevicesByDeviceConfigID(deviceConfigID)
		if err != nil {
			return err
		}
		// 断开设备连接
		for _, device := range devices {
			DisconnectDevice(device.ID, host)
		}
		return nil
	}
	return nil

}

// 通知协议插件
func DisconnectDevice(deviceID string, httpAddress string) error {
	type ReqData struct {
		DeviceID string `json:"device_id"`
	}
	reqData := ReqData{DeviceID: deviceID}
	reqDataBytes, err := json.Marshal(reqData)
	if err != nil {
		return err
	}
	rsp, err := http_client.DisconnectDevice(reqDataBytes, httpAddress)
	if err != nil {
		logrus.Warnf("update succeeded, but connect plugin failed: %s", err)
		return err
	}
	//解析返回数据
	var rspData http_client.RspData
	err = json.NewDecoder(rsp.Body).Decode(&rspData)
	if err != nil {
		logrus.Warnf("update succeeded, but plugin rspdata decode failed: %s", err)
		return err
	}
	if rspData.Code != 200 {
		logrus.Warnf("update succeeded, but plugin rsp: %s", rspData.Message)
		return err
	}
	return nil
}

// 根据设备ID通知协议插件
// 修改设备调用
// 删除设备调用
// 新增网关子设备的时候使用（deviceID送网关设备ID）
// 移除网关子设备调用
func DisconnectDeviceByDeviceID(deviceID string) error {
	// 获取设备信息
	device, err := dal.GetDeviceByID(deviceID)
	if err != nil {
		return err
	}
	if device.DeviceConfigID == nil {
		return nil
	}
	// 获取设备配置
	deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
	if err != nil {
		return err
	}
	if deviceConfig == nil {
		return nil
	}
	if deviceConfig.ProtocolType == nil {
		return fmt.Errorf("protocol type not found")
	}
	if *deviceConfig.ProtocolType == "MQTT" {
		return nil
	}
	// 根据协议类型获取协议信息
	servicePlugin, err := dal.GetServicePluginByServiceIdentifier(*deviceConfig.ProtocolType)
	if err != nil {
		return err
	}
	// 获取协议插件host:
	_, host, err := dal.GetServicePluginHttpAddressByID(servicePlugin.ID)
	if err != nil {
		return err
	}
	// 断开设备连接
	if deviceConfig.DeviceType == "3" {
		err = DisconnectDevice(*device.ParentID, host)
		if err != nil {
			return err
		}
	} else {
		err = DisconnectDevice(deviceID, host)
		if err != nil {
			return err
		}
	}
	return nil
}

// 根据设备ID通知协议插件设备实例配置发生变化
func UpdateDeviceConfigByDeviceID(deviceID string, currentConfig map[string]any, nextConfig map[string]any) error {
	device, err := dal.GetDeviceByID(deviceID)
	if err != nil {
		return err
	}
	if device.DeviceConfigID == nil {
		return nil
	}

	deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
	if err != nil {
		return err
	}
	if deviceConfig == nil || deviceConfig.ProtocolType == nil {
		return fmt.Errorf("protocol type not found")
	}
	if *deviceConfig.ProtocolType == "MQTT" {
		return nil
	}

	parseTemplateConfig := func(raw *string) map[string]any {
		if raw == nil || strings.TrimSpace(*raw) == "" {
			return map[string]any{}
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(*raw), &cfg); err != nil {
			logrus.Warnf("parse protocol config template failed for device %s: %v", deviceID, err)
			return map[string]any{}
		}
		if cfg == nil {
			return map[string]any{}
		}
		return cfg
	}

	mergeConfig := func(base map[string]any, override map[string]any) map[string]any {
		merged := make(map[string]any, len(base)+len(override))
		for k, v := range base {
			merged[k] = v
		}
		for k, v := range override {
			merged[k] = v
		}
		return merged
	}

	templateConfig := parseTemplateConfig(deviceConfig.ProtocolConfig)
	currentMerged := mergeConfig(templateConfig, currentConfig)
	nextMerged := mergeConfig(templateConfig, nextConfig)

	servicePlugin, err := dal.GetServicePluginByServiceIdentifier(*deviceConfig.ProtocolType)
	if err != nil {
		return err
	}
	_, host, err := dal.GetServicePluginHttpAddressByID(servicePlugin.ID)
	if err != nil {
		return err
	}

	reqDataBytes, err := json.Marshal(map[string]any{
		"device_id":      deviceID,
		"current_config": currentMerged,
		"device_config":  nextMerged,
	})
	if err != nil {
		return err
	}

	rsp, err := http_client.UpdateDeviceConfig(reqDataBytes, host)
	if err != nil {
		logrus.Warnf("update succeeded, but notify plugin config update failed: %s", err)
		return err
	}
	var rspData http_client.RspData
	if err := json.NewDecoder(rsp.Body).Decode(&rspData); err != nil {
		logrus.Warnf("update succeeded, but plugin config update rsp decode failed: %s", err)
		return err
	}
	if rspData.Code != 200 {
		logrus.Warnf("update succeeded, but plugin config update rsp: %s", rspData.Message)
		return fmt.Errorf("plugin config update failed: %s", rspData.Message)
	}
	return nil
}

func SyncDeviceAddByDeviceID(deviceID string) error {
	device, err := dal.GetDeviceByID(deviceID)
	if err != nil {
		return err
	}
	if device.DeviceConfigID == nil || strings.TrimSpace(*device.DeviceConfigID) == "" {
		return nil
	}

	deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
	if err != nil {
		return err
	}
	if deviceConfig == nil || deviceConfig.ProtocolType == nil {
		return fmt.Errorf("protocol type not found")
	}
	if *deviceConfig.ProtocolType == "MQTT" {
		return nil
	}
	logrus.Infof("syncing protocol plugin device binding: device_id=%s protocol=%s", device.ID, *deviceConfig.ProtocolType)

	templateConfig := map[string]any{}
	if deviceConfig.ProtocolConfig != nil && strings.TrimSpace(*deviceConfig.ProtocolConfig) != "" {
		if err := json.Unmarshal([]byte(*deviceConfig.ProtocolConfig), &templateConfig); err != nil {
			logrus.Warnf("parse protocol config template failed for device %s: %v", deviceID, err)
			templateConfig = map[string]any{}
		}
	}

	instanceConfig := map[string]any{}
	if device.ProtocolConfig != nil && strings.TrimSpace(*device.ProtocolConfig) != "" {
		if err := json.Unmarshal([]byte(*device.ProtocolConfig), &instanceConfig); err != nil {
			logrus.Warnf("parse protocol config instance failed for device %s: %v", deviceID, err)
			instanceConfig = map[string]any{}
		}
	}

	merged := make(map[string]any, len(templateConfig)+len(instanceConfig))
	for k, v := range templateConfig {
		merged[k] = v
	}
	for k, v := range instanceConfig {
		merged[k] = v
	}

	servicePlugin, err := dal.GetServicePluginByServiceIdentifier(*deviceConfig.ProtocolType)
	if err != nil {
		return err
	}
	_, host, err := dal.GetServicePluginHttpAddressByID(servicePlugin.ID)
	if err != nil {
		return err
	}

	reqDataBytes, err := json.Marshal(map[string]any{
		"device_id":     device.ID,
		"device_config": merged,
	})
	if err != nil {
		return err
	}

	rsp, err := http_client.AddDevice(reqDataBytes, host)
	if err != nil {
		return err
	}
	if rsp.StatusCode != 200 {
		return fmt.Errorf("plugin add device failed: status=%s", rsp.Status)
	}
	return nil
}

func SyncAllDevicesByProtocolType(protocolType string) error {
	devices, err := dal.ListActiveDevicesByProtocolType(protocolType)
	if err != nil {
		return err
	}
	logrus.Infof("syncing protocol plugin devices by protocol_type=%s count=%d", protocolType, len(devices))
	sort.SliceStable(devices, func(i, j int) bool {
		leftGateway := devices[i].ParentID == nil || strings.TrimSpace(*devices[i].ParentID) == ""
		rightGateway := devices[j].ParentID == nil || strings.TrimSpace(*devices[j].ParentID) == ""
		if leftGateway != rightGateway {
			return leftGateway
		}
		return devices[i].ID < devices[j].ID
	})
	for _, device := range devices {
		if err := SyncDeviceAddByDeviceID(device.ID); err != nil {
			return err
		}
	}
	return nil
}
