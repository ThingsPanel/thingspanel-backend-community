package service

import (
	"project/dal"
	"project/model"
	"project/others/http_client"
	"project/query"
	utils "project/utils"
	"strconv"
	"time"

	"github.com/go-basic/uuid"
	"github.com/jinzhu/copier"
)

type ServiceAccess struct{}

func (s *ServiceAccess) CreateAccess(req *model.CreateAccessReq, userClaims *utils.UserClaims) (map[string]interface{}, error) {
	var serviceAccess model.ServiceAccess
	copier.Copy(&serviceAccess, req)
	serviceAccess.ID = uuid.New()
	serviceAccess.TenantID = userClaims.TenantID
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
	// 根据service_plugin_id获取插件服务信息http地址
	servicePlugin, httpAddress, err := dal.GetServicePluginHttpAddressByID(req.ServicePluginID)
	if err != nil {
		return nil, err
	}
	return http_client.GetPluginFromConfigV2(httpAddress, servicePlugin.ServiceIdentifier, "", "SVCRT")
}

// GetServiceAccessDeviceList
func (s *ServiceAccess) GetServiceAccessDeviceList(req *model.ServiceAccessDeviceListReq, userClaims *utils.UserClaims) (interface{}, error) {
	// 通过voucher获取service_plugin_id
	serviceAccess, err := dal.GetServiceAccessByVoucher(req.Voucher, userClaims.TenantID)
	if err != nil {
		return nil, err
	}
	// 根据service_plugin_id获取插件服务信息的http地址
	_, httpAddress, err := dal.GetServicePluginHttpAddressByID(serviceAccess.ID)
	if err != nil {
		return nil, err
	}
	data, err := http_client.GetServiceAccessDeviceList(httpAddress, req.Voucher, strconv.Itoa(req.PageSize), strconv.Itoa(req.Page))
	if err != nil {
		return nil, err
	}
	// 查询已绑定设备列表
	devices, err := dal.GetServiceDeviceList(serviceAccess.ID)
	if err != nil {
		return nil, err
	}
	for _, dataDevice := range data.List {
		for _, device := range devices {
			if dataDevice.DeviceNumber == device.DeviceNumber {
				dataDevice.IsBind = true
			}
		}
	}
	return data, nil
}

// 通过service_identifier获取插件服务信息
func (s *ServiceAccess) GetPluginServiceAccessList(req *model.GetPluginServiceAccessListReq) (interface{}, error) {
	// 通过service_identifier获取插件服务信息
	servicePlugin, err := dal.GetServicePluginByServiceIdentifier(req.ServiceIdentifier)
	if err != nil {
		return nil, err
	}
	// 根据service_plugin_id获取服务接入点列表
	serviceAccessList, err := dal.GetServiceAccessListByServicePluginID(servicePlugin.ID)
	if err != nil {
		return nil, err
	}
	var serviceAccessMapList []map[string]interface{}

	// 遍历serviceAccessMap获取每个接入点的设备信息
	for _, serviceAccess := range serviceAccessList {
		// 获取设备列表
		devices, err := dal.GetServiceDeviceList(serviceAccess.ID)
		if err != nil {
			return nil, err
		}
		if len(devices) > 0 {
			serviceAccessMap := StructToMap(serviceAccess)
			serviceAccessMap["devices"] = devices
			serviceAccessMapList = append(serviceAccessMapList, serviceAccessMap)
		}
	}
	return serviceAccessMapList, nil
}
