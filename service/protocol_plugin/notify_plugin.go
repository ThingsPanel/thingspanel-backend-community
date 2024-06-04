package protocolplugin

import (
	"encoding/json"
	"fmt"
	"project/dal"
	"project/others/http_client"

	"github.com/sirupsen/logrus"
)

// 设备配置更新后主动断开设备连接
func DeviceConfigUpdateAndDisconnect(deviceConfigID string, protocolType string, deviceType string) error {
	var protocolPluginDeviceType int16
	switch deviceType {
	case "1":
		protocolPluginDeviceType = 1
	case "2":
		protocolPluginDeviceType = 2
	case "3":
		protocolPluginDeviceType = 2
	default:
		logrus.Error("device type not found")
		return fmt.Errorf("device type not found")
	}
	// 获取协议插件信息
	protocolPlugin, err := dal.GetProtocolPluginByDeviceTypeAndProtocolType(protocolPluginDeviceType, protocolType)
	if err != nil {
		logrus.Error(err)
		return fmt.Errorf("get protocol plugin failed: %s", err)
	}
	if !(protocolPlugin != nil && protocolPlugin.HTTPAddress != nil) {
		logrus.Error("protocol plugin not found")
		return fmt.Errorf("protocol plugin not found")
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
			DisconnectDevice(deviceID, *protocolPlugin.HTTPAddress)
		}
	} else if deviceType == "1" || deviceType == "2" {
		// 根据设备配置ID获取设备列表
		devices, err := dal.GetDevicesByDeviceConfigID(deviceConfigID)
		if err != nil {
			return err
		}
		// 断开设备连接
		for _, device := range devices {
			DisconnectDevice(device.ID, *protocolPlugin.HTTPAddress)
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
	var protocolPluginDeviceType int16
	switch deviceConfig.DeviceType {
	case "1":
		protocolPluginDeviceType = 1
	case "2":
		protocolPluginDeviceType = 2
	case "3":
		protocolPluginDeviceType = 2
	default:
		logrus.Error("device type not found")
		return fmt.Errorf("device type not found")
	}
	if deviceConfig.ProtocolType == nil {
		return fmt.Errorf("protocol type not found")
	}
	if *deviceConfig.ProtocolType == "MQTT" {
		return nil
	}
	// 获取协议插件信息
	protocolPlugin, err := dal.GetProtocolPluginByDeviceTypeAndProtocolType(protocolPluginDeviceType, *deviceConfig.ProtocolType)
	if err != nil {
		return err
	}
	if !(protocolPlugin != nil && protocolPlugin.HTTPAddress != nil) {
		logrus.Error("protocol plugin not found")
		return fmt.Errorf("protocol plugin not found")
	}
	// 断开设备连接
	if deviceConfig.DeviceType == "3" {
		err = DisconnectDevice(*device.ParentID, *protocolPlugin.HTTPAddress)
		if err != nil {
			return err
		}
	} else {
		err = DisconnectDevice(deviceID, *protocolPlugin.HTTPAddress)
		if err != nil {
			return err
		}
	}
	return nil
}
