package service

import (
	"project/dal"
	"project/model"
	"project/query"
	"time"

	"github.com/go-basic/uuid"

	"github.com/jinzhu/copier"
)

type ServicePlugin struct{}

func (s *ServicePlugin) Create(req *model.CreateServicePluginReq) (map[string]interface{}, error) {
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
		return nil, err
	}
	resp := make(map[string]interface{})
	resp["id"] = servicePlugin.ID
	return resp, err
}

func (s *ServicePlugin) List(req *model.GetServicePluginByPageReq) (map[string]interface{}, error) {
	total, list, err := dal.GetServicePluginListByPage(req)
	listRsp := make(map[string]interface{})
	listRsp["total"] = total
	listRsp["list"] = list

	return listRsp, err
}

func (s *ServicePlugin) Get(id string) (interface{}, error) {
	resp, err := dal.GetServicePlugin(id)
	return resp, err
}

func (s *ServicePlugin) Update(req *model.UpdateServicePluginReq) error {
	updates := make(map[string]interface{})
	if req.ServiceConfig != "" {
		// 要么是更新服务配置，要么是更新服务基本信息
		updates["service_config"] = req.ServiceConfig
	} else {
		updates["name"] = req.Name
		updates["service_identifier"] = req.ServiceIdentifier
		updates["service_type"] = req.ServiceType
		updates["version"] = req.Version
		updates["description"] = req.Description
		updates["remark"] = req.Remark
	}
	updates["update_at"] = time.Now().UTC()
	err := dal.UpdateServicePlugin(req.ID, updates)
	return err
}

func (s *ServicePlugin) Delete(req *model.DeleteServicePluginReq) error {
	err := dal.DeleteServicePlugin(req.ID)
	return err
}

// Heartbeat
func (s *ServicePlugin) Heartbeat(req *model.HeartbeatReq) error {
	// 更新服务插件的心跳时间
	err := dal.UpdateServicePluginHeartbeat(req.ServiceIdentifier)
	return err
}

// GetServiceSelect
func (s *ServicePlugin) GetServiceSelect(req *model.GetServiceSelectReq) (interface{}, error) {
	// 返回数据map
	resp := make(map[string]interface{})
	var protocolList []map[string]interface{}
	var serviceList []map[string]interface{}
	// 获取服务列表
	services, err := dal.GetServiceSelectList()
	if err != nil {
		return nil, err
	}
	for _, service := range services {
		if service.ServiceType == 2 {
			serviceList = append(serviceList, map[string]interface{}{
				"service_identifier": service.ServiceIdentifier,
				"name":               service.Name,
			})
		} else if service.ServiceType == 1 {
			protocolList = append(protocolList, map[string]interface{}{
				"service_identifier": service.ServiceIdentifier,
				"name":               service.Name,
			})
		}
	}
	resp["protocol"] = protocolList
	resp["service"] = serviceList
	return serviceList, err
}
