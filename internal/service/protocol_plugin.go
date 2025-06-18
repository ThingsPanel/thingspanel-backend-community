package service

import (
	"encoding/json"
	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/constant"
	"project/pkg/errcode"
	"project/third_party/others/http_client"

	"github.com/sirupsen/logrus"
)

type ProtocolPlugin struct{}

func (*ProtocolPlugin) CreateProtocolPlugin(req *model.CreateProtocolPluginReq) (*model.ProtocolPlugin, error) {
	// 校验是否json格式字符串
	if req.AdditionalInfo != nil && !IsJSON(*req.AdditionalInfo) {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "additional_info must be a json string",
		})
	}
	data, err := dal.CreateProtocolPluginWithDict(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, err
}

func (*ProtocolPlugin) DeleteProtocolPlugin(id string) error {
	err := dal.DeleteProtocolPluginWithDict(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return err
}

func (*ProtocolPlugin) UpdateProtocolPlugin(req *model.UpdateProtocolPluginReq) error {
	err := dal.UpdateProtocolPluginWithDict(req)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return err
}

func (*ProtocolPlugin) GetProtocolPluginListByPage(req *model.GetProtocolPluginListByPageReq) (interface{}, error) {
	total, list, err := dal.GetProtocolPluginListByPage(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	protocolPluginList := make(map[string]interface{})
	protocolPluginList["total"] = total
	protocolPluginList["list"] = list
	// 如果没有数据，返回空数组
	if total == 0 {
		protocolPluginList["list"] = make([]interface{}, 0)
	}
	return protocolPluginList, err
}

// 根据设备id获取协议插件设备配置表单
func (p *ProtocolPlugin) GetProtocolPluginForm(req *model.GetProtocolPluginFormReq) (interface{}, error) {
	var protocolType string
	var deviceType string
	// 获取设备信息
	d, err := dal.GetDeviceByID(req.DeviceId)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if d.DeviceConfigID == nil || *d.DeviceConfigID == "" {
		protocolType = "MQTT"
		deviceType = "1"
	} else {
		//获取设备配置信息
		dc, err := dal.GetDeviceConfigByID(req.DeviceId)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		protocolType = *dc.ProtocolType
		deviceType = dc.DeviceType
	}
	// 获取协议插件表单
	data, err := p.GetProtocolPluginFormByProtocolType(protocolType, deviceType)
	if err != nil {
		return nil, errcode.WithVars(105001, map[string]interface{}{
			"error": err.Error(),
		})
	}
	return data, err
}

// 根据协议类型获取设备配置表单
func (p *ProtocolPlugin) GetProtocolPluginFormByProtocolType(protocolType string, deviceType string) (interface{}, error) {
	if protocolType == "MQTT" {
		// 返回空{}，表示不需要配置
		return nil, nil
	}
	data, err := p.GetPluginForm(protocolType, deviceType, string(constant.CONFIG_FORM))
	if err != nil {
		return nil, errcode.WithVars(105001, map[string]interface{}{
			"error": err.Error(),
		})
	}
	return data, err
}

// 去协议插件获取各种表单
// 请求参数：protocol_type,device_type,form_type,voucher_type
func (*ProtocolPlugin) GetPluginForm(protocolType string, deviceType string, formType string) (interface{}, error) {
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
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "device type not found",
		})
	}
	protocolPlugin, err := dal.GetProtocolPluginByDeviceTypeAndProtocolType(protocolPluginDeviceType, protocolType)
	if err != nil {
		logrus.Error(err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	host := *protocolPlugin.HTTPAddress
	// 请求表单
	data, err := http_client.GetPluginFromConfigV2(host, protocolType, deviceType, formType)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	return data, err

}

// 获取设备配置，接口提供给协议插件
// 请求参数：device_id,voucher
func (*ProtocolPlugin) GetDeviceConfig(req model.GetDeviceConfigReq) (interface{}, error) {
	// 校验device_id和voucher必须有一个
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
	if deviceConfig != nil {
		deviceConfigForProtocolPlugin.DeviceType = deviceConfig.DeviceType
		deviceConfigForProtocolPlugin.ProtocolType = *deviceConfig.ProtocolType
		if deviceConfig.ProtocolConfig != nil && IsJSON(*deviceConfig.ProtocolConfig) {
			// 转换为map[string]interface{}
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
		// 判断device.ProtocolConfig是否为json格式字符串
		if IsJSON(*device.ProtocolConfig) {
			// 转换为map[string]interface{}
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
	// 如果是协议插件相关设备，必定有deviceConfig
	deviceConfigForProtocolPlugin.DeviceType = deviceConfig.DeviceType

	// 判断设备类型是否为网关
	if deviceConfig.DeviceType == "2" {
		var subDeviceList []*model.Device
		// 获取子设备列表
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
			if subDevice.SubDeviceAddr == nil {
				logrus.Warn("subDeviceAddr is nil")
				return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error": "subDeviceAddr not found",
				})
			}
			subDeviceConfigForProtocolPlugin.SubDeviceAddr = *subDevice.SubDeviceAddr
			// 判断device.ProtocolConfig是否为json格式字符串
			if IsJSON(*subDevice.ProtocolConfig) {
				// 转换为map[string]interface{}
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
			// 获取子设备配置信息
			if subDevice.DeviceConfigID == nil {
				return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error": "sub device config not found",
				})
			}
			deviceConfig, err := dal.GetDeviceConfigByID(*subDevice.DeviceConfigID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"sql_error": err.Error(),
				})
			}
			// 判断deviceConfig.ProtocolConfig是否为json格式字符串
			if deviceConfig.ProtocolConfig != nil && IsJSON(*deviceConfig.ProtocolConfig) {
				// 转换为map[string]interface{}
				var config map[string]interface{}
				err := json.Unmarshal([]byte(*deviceConfig.ProtocolConfig), &config)
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
	// 返回设备配置信息
	logrus.Info("deviceConfigForProtocolPlugin:", deviceConfigForProtocolPlugin)
	return deviceConfigForProtocolPlugin, nil
}
