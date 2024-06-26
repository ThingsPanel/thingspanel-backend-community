package service

import (
	"encoding/json"
	"fmt"
	"project/constant"
	dal "project/dal"
	model "project/model"
	"project/others/http_client"

	"github.com/sirupsen/logrus"
)

type ProtocolPlugin struct{}

func (p *ProtocolPlugin) CreateProtocolPlugin(req *model.CreateProtocolPluginReq) (*model.ProtocolPlugin, error) {
	// 校验是否json格式字符串
	if req.AdditionalInfo != nil && !IsJSON(*req.AdditionalInfo) {
		return nil, fmt.Errorf("additional_info is not a valid JSON")
	}
	data, err := dal.CreateProtocolPluginWithDict(req)
	return data, err
}

func (p *ProtocolPlugin) DeleteProtocolPlugin(id string) error {
	err := dal.DeleteProtocolPluginWithDict(id)
	return err
}

func (p *ProtocolPlugin) UpdateProtocolPlugin(req *model.UpdateProtocolPluginReq) error {
	err := dal.UpdateProtocolPluginWithDict(req)
	return err
}

func (p *ProtocolPlugin) GetProtocolPluginListByPage(req *model.GetProtocolPluginListByPageReq) (interface{}, error) {
	total, list, err := dal.GetProtocolPluginListByPage(req)
	if err != nil {
		return nil, err
	}
	protocolPluginList := make(map[string]interface{})
	protocolPluginList["total"] = total
	protocolPluginList["list"] = list
	return protocolPluginList, err
}

// 根据设备id获取协议插件设备配置表单
func (p *ProtocolPlugin) GetProtocolPluginForm(req *model.GetProtocolPluginFormReq) (interface{}, error) {
	var protocolType string
	var deviceType string
	// 获取设备信息
	d, err := dal.GetDeviceByID(req.DeviceId)
	if err != nil {
		return nil, err
	}
	if d.DeviceConfigID == nil || *d.DeviceConfigID == "" {
		protocolType = "MQTT"
		deviceType = "1"
	} else {
		//获取设备配置信息
		dc, err := dal.GetDeviceConfigByID(req.DeviceId)
		if err != nil {
			return nil, err
		}
		protocolType = *dc.ProtocolType
		deviceType = dc.DeviceType
	}
	// 获取协议插件表单
	return p.GetProtocolPluginFormByProtocolType(protocolType, deviceType)
}

// 根据协议类型获取设备配置表单
func (p *ProtocolPlugin) GetProtocolPluginFormByProtocolType(protocolType string, deviceType string) (interface{}, error) {
	if protocolType == "MQTT" {
		// 返回空{}，表示不需要配置
		return nil, nil
	}
	return p.GetPluginForm(protocolType, deviceType, string(constant.CONFIG_FORM))
}

// 去协议插件获取各种表单
// 请求参数：protocol_type,device_type,form_type,voucher_type
func (p *ProtocolPlugin) GetPluginForm(protocolType string, deviceType string, formType string) (interface{}, error) {
	// 获取协议插件host:127.0.0.1:503
	var protocolPluginDeviceType int16
	switch deviceType {
	case constant.DEVICE_TYPE_1:
		protocolPluginDeviceType = 1
	case constant.DEVICE_TYPE_2:
		protocolPluginDeviceType = 2
	case constant.DEVICE_TYPE_3:
		// 网关子设备对应的协议插件设备类型为2
		protocolPluginDeviceType = 2
	default:
		return nil, fmt.Errorf("device type not found")
	}
	protocolPlugin, err := dal.GetProtocolPluginByDeviceTypeAndProtocolType(protocolPluginDeviceType, protocolType)
	if err != nil {
		logrus.Error(err)
		return nil, fmt.Errorf("get protocol plugin failed: %s", err)
	}
	host := *protocolPlugin.HTTPAddress
	// 请求表单
	return http_client.GetPluginFromConfigV2(host, protocolType, deviceType, formType)

}

// 获取设备配置，接口提供给协议插件
// 请求参数：device_id,voucher
func (p *ProtocolPlugin) GetDeviceConfig(req model.GetDeviceConfigReq) (interface{}, error) {
	// 校验device_id和voucher必须有一个
	if req.DeviceId == "" && req.Voucher == "" && req.DeviceNumber == "" {
		return nil, fmt.Errorf("device_id and voucher and device_number must have one")
	}
	var device *model.Device
	var deviceConfig *model.DeviceConfig
	var deviceConfigForProtocolPlugin model.DeviceConfigForProtocolPlugin

	// 获取设备信息
	if req.DeviceId != "" {
		d, err := dal.GetDeviceByID(req.DeviceId)
		if err != nil {
			return nil, err
		}
		device = d
	} else if req.Voucher != "" {
		d, err := dal.GetDeviceByVoucher(req.Voucher)
		if err != nil {
			return nil, err
		}
		device = d
	} else if req.DeviceNumber != "" {
		d, err := dal.GetDeviceByDeviceNumber(req.DeviceNumber)
		if err != nil {
			return nil, err
		}
		device = d
	}
	// 获取设备配置信息
	if device.DeviceConfigID != nil {
		dc, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
		if err != nil {
			return nil, err
		}
		deviceConfig = dc
	} else {
		logrus.Warn("deviceConfigID is nil")
		return nil, fmt.Errorf("device config id is nil")
	}

	// 给deviceConfigForProtocolPlugin赋值
	deviceConfigForProtocolPlugin.ID = device.ID
	deviceConfigForProtocolPlugin.Voucher = device.Voucher
	if deviceConfig != nil {
		deviceConfigForProtocolPlugin.DeviceType = deviceConfig.DeviceType
		deviceConfigForProtocolPlugin.ProtocolType = *deviceConfig.ProtocolType
		if deviceConfig.ProtocolConfig != nil && IsJSON(*deviceConfig.ProtocolConfig) {
			// 转换为map[string]interface{}
			var config map[string]interface{}
			err := json.Unmarshal([]byte(*deviceConfig.ProtocolConfig), &config)
			if err != nil {
				return nil, err
			}
			deviceConfigForProtocolPlugin.ProtocolConfigTemplate = config
		} else {
			deviceConfigForProtocolPlugin.ProtocolConfigTemplate = nil
		}
		// 判断device.ProtocolConfig是否为json格式字符串
		if IsJSON(*device.ProtocolConfig) {
			// 转换为map[string]interface{}
			var config map[string]interface{}
			err := json.Unmarshal([]byte(*device.ProtocolConfig), &config)
			if err != nil {
				return nil, err
			}
			deviceConfigForProtocolPlugin.Config = config
		} else {
			deviceConfigForProtocolPlugin.Config = nil
		}
	} else {
		logrus.Warn("deviceConfig is nil")
		return nil, fmt.Errorf("device config not found")
	}
	// 如果是协议插件相关设备，必定有deviceConfig
	deviceConfigForProtocolPlugin.DeviceType = deviceConfig.DeviceType

	// 判断设备类型是否为网关
	if deviceConfig.DeviceType == "2" {
		var subDeviceList []*model.Device
		// 获取子设备列表
		subDeviceList, err := dal.GetSubDeviceListByParentID(device.ID)
		if err != nil {
			return nil, err
		}
		for _, subDevice := range subDeviceList {
			var subDeviceConfigForProtocolPlugin model.SubDeviceConfigForProtocolPlugin
			subDeviceConfigForProtocolPlugin.DeviceID = subDevice.ID
			subDeviceConfigForProtocolPlugin.Voucher = subDevice.Voucher
			if subDevice.SubDeviceAddr == nil {
				logrus.Warn("subDeviceAddr is nil")
				return nil, fmt.Errorf("subDeviceAddr is nil")
			}
			subDeviceConfigForProtocolPlugin.SubDeviceAddr = *subDevice.SubDeviceAddr
			// 判断device.ProtocolConfig是否为json格式字符串
			if IsJSON(*subDevice.ProtocolConfig) {
				// 转换为map[string]interface{}
				var config map[string]interface{}
				err := json.Unmarshal([]byte(*subDevice.ProtocolConfig), &config)
				if err != nil {
					return nil, err
				}
				subDeviceConfigForProtocolPlugin.Config = config
			} else {
				subDeviceConfigForProtocolPlugin.Config = nil
			}
			// 获取子设备配置信息
			if subDevice.DeviceConfigID == nil {
				return nil, fmt.Errorf("subDevice.DeviceConfigID is nil")
			}
			deviceConfig, err := dal.GetDeviceConfigByID(*subDevice.DeviceConfigID)
			if err != nil {
				return nil, err
			}
			// 判断deviceConfig.ProtocolConfig是否为json格式字符串
			if deviceConfig.ProtocolConfig != nil && IsJSON(*deviceConfig.ProtocolConfig) {
				// 转换为map[string]interface{}
				var config map[string]interface{}
				err := json.Unmarshal([]byte(*deviceConfig.ProtocolConfig), &config)
				if err != nil {
					return nil, err
				}
				subDeviceConfigForProtocolPlugin.ProtocolConfigTemplate = config
			} else {
				subDeviceConfigForProtocolPlugin.ProtocolConfigTemplate = nil
			}
			deviceConfigForProtocolPlugin.SubDivices = append(deviceConfigForProtocolPlugin.SubDivices, subDeviceConfigForProtocolPlugin)
		}
	}
	// 返回设备配置信息
	logrus.Info("deviceConfigForProtocolPlugin:", deviceConfigForProtocolPlugin)
	return deviceConfigForProtocolPlugin, nil
}
