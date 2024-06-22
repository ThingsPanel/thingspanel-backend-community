package service

import (
	"github.com/go-basic/uuid"
	"project/dal"
	"project/model"
	"project/query"
	"time"

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

func (s *ServicePlugin) Get(req *model.GetServicePluginReq) (interface{}, error) {
	resp, err := dal.GetServicePlugin(req)
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