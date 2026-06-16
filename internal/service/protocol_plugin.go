package service

import (
	"encoding/json"

	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"

	"github.com/sirupsen/logrus"
)

type ProtocolPlugin struct{}

// 获取设备配置，接口提供给协议插件
// 请求参数：device_id,voucher,device_number
func (*ProtocolPlugin) GetDeviceConfig(req model.GetDeviceConfigReq) (interface{}, error) {
	// 校验device_id、voucher、device_number必须有一个
	if req.DeviceId == "" && req.Voucher == "" && req.DeviceNumber == "" {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "device id and voucher and device_number must have one",
		})
	}
	var device *model.Device
	var deviceConfig *model.DeviceConfig
	var deviceConfigForProtocolPlugin model.DeviceConfigForProtocolPlugin

	// 获取设备信息
	if req.DeviceId != "" {
		d, err := dal.GetDeviceByID(req.DeviceId)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		device = d
	} else if req.Voucher != "" {
		d, err := dal.GetDeviceByVoucher(req.Voucher)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		device = d
	} else if req.DeviceNumber != "" {
		d, err := dal.GetDeviceByDeviceNumber(req.DeviceNumber)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		device = d
	}
	// 获取设备配置信息
	if device.DeviceConfigID != nil {
		dc, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		deviceConfig = dc
	} else {
		logrus.Warn("deviceConfigID is nil")
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "device config not found",
		})
	}

	// 给deviceConfigForProtocolPlugin赋值
	deviceConfigForProtocolPlugin.ID = device.ID
	deviceConfigForProtocolPlugin.Voucher = device.Voucher
	deviceConfigForProtocolPlugin.DeviceNumber = device.DeviceNumber
	if deviceConfig != nil {
		deviceConfigForProtocolPlugin.DeviceType = deviceConfig.DeviceType
		if deviceConfig.ProtocolType != nil {
			deviceConfigForProtocolPlugin.ProtocolType = *deviceConfig.ProtocolType
		}
		if deviceConfig.ProtocolConfig != nil && IsJSON(*deviceConfig.ProtocolConfig) {
			var config map[string]interface{}
			err := json.Unmarshal([]byte(*deviceConfig.ProtocolConfig), &config)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error": err.Error(),
				})
			}
			deviceConfigForProtocolPlugin.ProtocolConfigTemplate = config
		} else {
			deviceConfigForProtocolPlugin.ProtocolConfigTemplate = nil
		}
		if device.ProtocolConfig != nil && IsJSON(*device.ProtocolConfig) {
			var config map[string]interface{}
			err := json.Unmarshal([]byte(*device.ProtocolConfig), &config)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error": err.Error(),
				})
			}
			deviceConfigForProtocolPlugin.Config = config
		} else {
			deviceConfigForProtocolPlugin.Config = nil
		}
	} else {
		logrus.Warn("deviceConfig is nil")
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "device config not found",
		})
	}
	deviceConfigForProtocolPlugin.DeviceType = deviceConfig.DeviceType

	// 判断设备类型是否为网关
	if deviceConfig.DeviceType == "2" {
		var subDeviceList []*model.Device
		subDeviceList, err := dal.GetSubDeviceListByParentID(device.ID)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		for _, subDevice := range subDeviceList {
			var subDeviceConfigForProtocolPlugin model.SubDeviceConfigForProtocolPlugin
			subDeviceConfigForProtocolPlugin.DeviceID = subDevice.ID
			subDeviceConfigForProtocolPlugin.Voucher = subDevice.Voucher
			subDeviceConfigForProtocolPlugin.DeviceNumber = subDevice.DeviceNumber
			if subDevice.SubDeviceAddr == nil {
				logrus.Warn("subDeviceAddr is nil")
				return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error": "subDeviceAddr not found",
				})
			}
			subDeviceConfigForProtocolPlugin.SubDeviceAddr = *subDevice.SubDeviceAddr
			if subDevice.ProtocolConfig != nil && IsJSON(*subDevice.ProtocolConfig) {
				var config map[string]interface{}
				err := json.Unmarshal([]byte(*subDevice.ProtocolConfig), &config)
				if err != nil {
					return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
						"error": err.Error(),
					})
				}
				subDeviceConfigForProtocolPlugin.Config = config
			} else {
				subDeviceConfigForProtocolPlugin.Config = nil
			}
			if subDevice.DeviceConfigID == nil {
				return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error": "sub device config not found",
				})
			}
			subDeviceConfig, err := dal.GetDeviceConfigByID(*subDevice.DeviceConfigID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"sql_error": err.Error(),
				})
			}
			if subDeviceConfig.ProtocolConfig != nil && IsJSON(*subDeviceConfig.ProtocolConfig) {
				var config map[string]interface{}
				err := json.Unmarshal([]byte(*subDeviceConfig.ProtocolConfig), &config)
				if err != nil {
					return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
						"error": err.Error(),
					})
				}
				subDeviceConfigForProtocolPlugin.ProtocolConfigTemplate = config
			} else {
				subDeviceConfigForProtocolPlugin.ProtocolConfigTemplate = nil
			}
			deviceConfigForProtocolPlugin.SubDivices = append(deviceConfigForProtocolPlugin.SubDivices, subDeviceConfigForProtocolPlugin)
		}
	}
	logrus.Info("deviceConfigForProtocolPlugin:", deviceConfigForProtocolPlugin)
	return deviceConfigForProtocolPlugin, nil
}

// 通过协议标识符获取设备列表（包含设备配置信息）
func (*ProtocolPlugin) GetDevicesByProtocolPlugin(req model.GetDevicesByProtocolPluginReq) (interface{}, error) {
	var devicesRsp model.GetDevicesByProtocolPluginRsp
	if req.DeviceType == "1" {
		err := dal.GetDeviceListByProtocolType(req, &devicesRsp)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
	}
	return devicesRsp, nil
}
