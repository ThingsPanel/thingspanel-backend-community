package service

import (
	"encoding/json"
	"errors"
	"project/dal"
	"project/model"
	"project/others/http_client"
	"project/query"
	"time"

	"github.com/go-basic/uuid"
	"github.com/jinzhu/copier"
)

type ServiceAccess struct{}

func (s *ServiceAccess) CreateAccess(req *model.CreateAccessReq) (map[string]interface{}, error) {
	var serviceAccess model.ServiceAccess
	copier.Copy(&serviceAccess, req)
	serviceAccess.ID = uuid.New()
	if *serviceAccess.ServiceAccessConfig == "" {
		*serviceAccess.ServiceAccessConfig = "{}"
	}
	serviceAccess.CreateAt = time.Now().UTC()
	serviceAccess.UpdateAt = time.Now().UTC()
	err := query.ServiceAccess.Create(&serviceAccess)
	if err != nil {
		return nil, err
	}
	resp := make(map[string]interface{})
	resp["id"] = serviceAccess.ID
	return resp, nil
}

func (s *ServiceAccess) List(req *model.GetServiceAccessByPageReq) (map[string]interface{}, error) {
	total, list, err := dal.GetServiceAccessListByPage(req)
	listRsp := make(map[string]interface{})
	listRsp["total"] = total
	listRsp["list"] = list

	return listRsp, err
}

func (s *ServiceAccess) Update(req *model.UpdateAccessReq) error {
	updates := make(map[string]interface{})
	updates["service_access_config"] = req.ServiceAccessConfig
	updates["update_at"] = time.Now().UTC()
	err := dal.UpdateServiceAccess(req.ID, updates)
	return err
}

func (s *ServiceAccess) Delete(req *model.DeleteAccessReq) error {
	err := dal.DeleteServiceAccess(req.ID)
	return err
}

// GetVoucherForm
func (s *ServiceAccess) GetVoucherForm(req *model.GetServiceAccessVoucherFormReq) (interface{}, error) {
	// 根据service_plugin_id获取插件服务信息
	servicePlugin, err := dal.GetServicePluginByID(req.ServicePluginID)
	if err != nil {
		return nil, err
	}

	if servicePlugin.ServiceConfig == nil || *servicePlugin.ServiceConfig == "" {
		// 服务配置错误，无法获取表单
		return nil, errors.New("service plugin config error, can not get form")
	}
	// 解析服务配置model.ServicePluginConfig
	var serviceAccessConfig model.ServiceAccessConfig
	err = json.Unmarshal([]byte(*servicePlugin.ServiceConfig), &serviceAccessConfig)
	if err != nil {
		return nil, errors.New("service plugin config error: " + err.Error())
	}
	// 校验服务配置的HttpAddress是否是ip:port格式
	if serviceAccessConfig.HttpAddress == "" {
		return nil, errors.New("service plugin config error: host is empty")
	}
	return http_client.GetPluginFromConfigV2(serviceAccessConfig.HttpAddress, servicePlugin.ServiceIdentifier, "", "SVCRT")
}
