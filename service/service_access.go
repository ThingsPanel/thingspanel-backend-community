package service

import (
	"encoding/json"
	"project/dal"
	"project/model"
	"project/others/http_client"
	"project/query"
	utils "project/utils"
	"strconv"
	"time"

	"github.com/go-basic/uuid"
	"github.com/jinzhu/copier"
	"github.com/sirupsen/logrus"
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
	// 查询服务接入点信息
	serviceAccess, err := dal.GetServiceAccessByID(req.ID)
	if err != nil {
		return err
	}
	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = req.Name
	}
	if req.ServiceAccessConfig != nil {
		if *req.ServiceAccessConfig == "" {
			*req.ServiceAccessConfig = "{}"
		}
		serviceAccess.ServiceAccessConfig = req.ServiceAccessConfig
	}
	if req.Voucher != nil {
		updates["voucher"] = req.Voucher
	}
	updates["update_at"] = time.Now().UTC()
	err = dal.UpdateServiceAccess(req.ID, updates)
	if err != nil {
		return err
	}
	if serviceAccess.Voucher != "" {
		// 查询服务地址
		_, host, err := dal.GetServicePluginHttpAddressByID(serviceAccess.ServicePluginID)
		if err != nil {
			return err
		}
		dataMap := make(map[string]interface{})
		dataMap["service_access_id"] = req.ID
		// 将dataMap转json字符串
		dataBytes, err := json.Marshal(dataMap)
		if err != nil {
			return err
		}
		// 通知服务插件
		logrus.Debug("发送通知给服务插件")

		rsp, err := http_client.Notification("1", string(dataBytes), host)
		if err != nil {
			return err
		}
		logrus.Debug("通知服务插件成功")
		logrus.Debug(string(rsp))
	}
	return nil
}

func (s *ServiceAccess) Delete(id string) error {
	err := dal.DeleteServiceAccess(id)
	return err
}

// GetVoucherForm
func (s *ServiceAccess) GetVoucherForm(req *model.GetServiceAccessVoucherFormReq) (interface{}, error) {
	// 根据service_plugin_id获取插件服务信息http地址
	servicePlugin, httpAddress, err := dal.GetServicePluginHttpAddressByID(req.ServicePluginID)
	if err != nil {
		return nil, err
	}
	return http_client.GetPluginFromConfigV2(httpAddress, servicePlugin.ServiceIdentifier, "", "SVCR")
}

// GetServiceAccessDeviceList
func (s *ServiceAccess) GetServiceAccessDeviceList(req *model.ServiceAccessDeviceListReq, userClaims *utils.UserClaims) (interface{}, error) {
	// 通过voucher获取service_plugin_id
	serviceAccess, err := dal.GetServiceAccessByVoucher(req.Voucher, userClaims.TenantID)
	if err != nil {
		return nil, err
	}
	// 根据service_plugin_id获取插件服务信息的http地址
	_, httpAddress, err := dal.GetServicePluginHttpAddressByID(serviceAccess.ServicePluginID)
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

// GetPluginServiceAccess
func (s *ServiceAccess) GetPluginServiceAccess(req *model.GetPluginServiceAccessReq) (interface{}, error) {
	// 通过service_access_id获取服务接入点信息
	serviceAccess, err := dal.GetServiceAccessByID(req.ServiceAccessID)
	if err != nil {
		return nil, err
	}
	// 获取设备列表
	devices, err := dal.GetServiceDeviceList(serviceAccess.ID)
	if err != nil {
		return nil, err
	}
	serviceAccessMap := StructToMap(serviceAccess)
	serviceAccessMap["devices"] = devices
	return serviceAccessMap, nil
}
