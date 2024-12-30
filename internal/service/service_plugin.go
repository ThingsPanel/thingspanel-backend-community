package service

import (
	"encoding/json"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/constant"
	"project/pkg/errcode"
	"project/third_party/others/http_client"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"

	"github.com/jinzhu/copier"
)

type ServicePlugin struct{}

func (*ServicePlugin) Create(req *model.CreateServicePluginReq) (map[string]interface{}, error) {
	var servicePlugin model.ServicePlugin
	copier.Copy(&servicePlugin, req)
	servicePlugin.ID = uuid.New()
	servicePlugin.CreateAt = time.Now().UTC()
	servicePlugin.UpdateAt = time.Now().UTC()
	if *servicePlugin.ServiceConfig == "" {
		*servicePlugin.ServiceConfig = "{}"
	}
	err := query.ServicePlugin.Create(&servicePlugin)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	resp := make(map[string]interface{})
	resp["id"] = servicePlugin.ID
	return resp, err
}

func (*ServicePlugin) List(req *model.GetServicePluginByPageReq) (map[string]interface{}, error) {
	total, list, err := dal.GetServicePluginListByPage(req)
	listRsp := make(map[string]interface{})
	listRsp["total"] = total
	if list == nil {
		list = make([]map[string]interface{}, 0)
	}
	listRsp["list"] = list
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return listRsp, err
}

func (*ServicePlugin) Get(id string) (interface{}, error) {
	resp, err := dal.GetServicePlugin(id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return resp, err
}

func (*ServicePlugin) Update(req *model.UpdateServicePluginReq) error {
	updates := make(map[string]interface{})
	// 要么是更新服务配置，要么是更新服务基本信息
	updates["service_config"] = req.ServiceConfig
	updates["name"] = req.Name
	updates["service_identifier"] = req.ServiceIdentifier
	updates["service_type"] = req.ServiceType
	updates["version"] = req.Version
	updates["description"] = req.Description
	updates["remark"] = req.Remark
	updates["update_at"] = time.Now().UTC()
	err := dal.UpdateServicePlugin(req.ID, updates)
	return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
		"sql_error": err.Error(),
	})
}

func (*ServicePlugin) Delete(id string) error {
	err := dal.DeleteServicePlugin(id)
	return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
		"sql_error": err.Error(),
	})
}

// Heartbeat
func (*ServicePlugin) Heartbeat(req *model.HeartbeatReq) error {
	// 更新服务插件的心跳时间
	err := dal.UpdateServicePluginHeartbeat(req.ServiceIdentifier)
	return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
		"sql_error": err.Error(),
	})
}

// GetServiceSelect
func (*ServicePlugin) GetServiceSelect(req *model.GetServiceSelectReq) (interface{}, error) {
	// 返回数据map
	resp := make(map[string]interface{})
	var protocolList []map[string]interface{}
	protocolList = append(protocolList, map[string]interface{}{
		"service_identifier": "MQTT",
		"name":               "MQTT",
	})
	// 定义一个空切片
	var serviceList []map[string]interface{}
	// 获取服务列表
	services, err := dal.GetServiceSelectList()
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	for _, service := range services {
		flag := true
		// 协议类型
		if service.ServiceType == int32(1) {
			if req.DeviceType != nil {
				flag = false
				// 解析service.ServiceConfig
				var serviceAccessConfig model.ProtocolAccessConfig
				err = json.Unmarshal([]byte(*service.ServiceConfig), &serviceAccessConfig)
				if err != nil {
					logrus.Warn("service plugin config error: ", err)
					continue
				}
				switch *req.DeviceType {
				case 1:
					if serviceAccessConfig.DeviceType == 1 {
						flag = true
					}
				case 2, 3:
					if serviceAccessConfig.DeviceType == 2 {
						flag = true
					}
				default:
					logrus.Warn("device type is error: ", *req.DeviceType)
				}
			}
			if flag {
				protocolList = append(protocolList, map[string]interface{}{
					"service_identifier": service.ServiceIdentifier,
					"name":               service.Name,
				})
			}
			flag = true
		} else if service.ServiceType == int32(2) {
			serviceList = append(serviceList, map[string]interface{}{
				"service_identifier": service.ServiceIdentifier,
				"name":               service.Name,
				"service_plugin_id":  service.ID,
			})
		}
	}
	resp["protocol"] = protocolList
	if serviceList == nil {
		serviceList = make([]map[string]interface{}, 0)
	}
	resp["service"] = serviceList
	return resp, err
}

// 去协议插件获取各种表单
// 请求参数：protocol_type,device_type,form_type,voucher_type
func (*ServicePlugin) GetPluginForm(protocolType string, deviceType string, formType string) (interface{}, error) {
	// 根据协议类型获取协议信息
	servicePlugin, err := dal.GetServicePluginByServiceIdentifier(protocolType)
	if err != nil {
		return nil, err
	}
	// 获取协议插件host:
	_, host, err := dal.GetServicePluginHttpAddressByID(servicePlugin.ID)
	if err != nil {
		return nil, err
	}
	// 请求表单
	return http_client.GetPluginFromConfigV2(host, protocolType, deviceType, formType)

}

// 根据协议类型获取设备配置表单
func (p *ServicePlugin) GetProtocolPluginFormByProtocolType(protocolType string, deviceType string) (interface{}, error) {
	if protocolType == "MQTT" {
		// 返回空{}，表示不需要配置
		return nil, nil
	}
	data, err := p.GetPluginForm(protocolType, deviceType, string(constant.CONFIG_FORM))
	if err != nil {
		return nil, errcode.NewWithMessage(105001, err.Error())
	}
	return data, err
}

// 根据服务service_identifier获取服务详情
func (*ServicePlugin) GetServicePluginByServiceIdentifier(serviceIdentifier string) (interface{}, error) {
	data, err := dal.GetServicePluginByServiceIdentifier(serviceIdentifier)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, err
}
