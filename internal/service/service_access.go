package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	utils "project/pkg/utils"
	"project/third_party/others/http_client"

	"github.com/go-basic/uuid"
	"github.com/jinzhu/copier"
	"github.com/sirupsen/logrus"
)

type ServiceAccess struct{}

func (*ServiceAccess) CreateAccess(req *model.CreateAccessReq, userClaims *utils.UserClaims) (map[string]interface{}, error) {
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
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	resp := make(map[string]interface{})
	resp["id"] = serviceAccess.ID
	return resp, nil
}

func (*ServiceAccess) List(req *model.GetServiceAccessByPageReq, userClaims *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetServiceAccessListByPage(req, userClaims.TenantID)
	listRsp := make(map[string]interface{})
	listRsp["total"] = total
	listRsp["list"] = list
	if err != nil {
		return listRsp, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	return listRsp, err
}

func (*ServiceAccess) Update(req *model.UpdateAccessReq) error {
	// 查询服务接入点信息
	serviceAccess, err := dal.GetServiceAccessByID(req.ID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
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
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if serviceAccess.Voucher != "" {
		// 查询服务地址
		_, host, err := dal.GetServicePluginHttpAddressByID(serviceAccess.ServicePluginID)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		dataMap := make(map[string]interface{})
		dataMap["service_access_id"] = req.ID
		// 将dataMap转json字符串
		dataBytes, err := json.Marshal(dataMap)
		if err != nil {
			return errcode.WithData(100004, map[string]interface{}{
				"error":     err.Error(),
				"data_type": fmt.Sprintf("%T", dataMap),
			})
		}
		// 通知服务插件
		logrus.Debug("发送通知给服务插件")

		rsp, err := http_client.Notification("1", string(dataBytes), host)
		if err != nil {
			return errcode.WithVars(105001, map[string]interface{}{
				"error": err.Error(),
			})
		}
		logrus.Debug("通知服务插件成功")
		logrus.Debug(string(rsp))
	}
	return nil
}

func (*ServiceAccess) Delete(id string) error {
	// 查询是否还有未删除的设备
	devices, err := dal.GetServiceDeviceList(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if len(devices) > 0 {
		return errcode.New(200064)
	}
	err = dal.DeleteServiceAccess(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return err
}

// GetVoucherForm
func (*ServiceAccess) GetVoucherForm(req *model.GetServiceAccessVoucherFormReq) (interface{}, error) {
	// 根据service_plugin_id获取插件服务信息http地址
	servicePlugin, httpAddress, err := dal.GetServicePluginHttpAddressByID(req.ServicePluginID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	data, err := http_client.GetPluginFromConfigV2(httpAddress, servicePlugin.ServiceIdentifier, "", "SVCR")
	if err != nil {
		return nil, errcode.NewWithMessage(105001, err.Error())
	}
	return data, nil
}

// GetServiceAccessDeviceList
func (*ServiceAccess) GetServiceAccessDeviceList(req *model.ServiceAccessDeviceListReq, userClaims *utils.UserClaims) (interface{}, error) {
	// 通过voucher获取service_plugin_id
	serviceAccess, err := dal.GetServiceAccessByVoucher(req.Voucher, userClaims.TenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 根据service_plugin_id获取插件服务信息的http地址
	_, httpAddress, err := dal.GetServicePluginHttpAddressByID(serviceAccess.ServicePluginID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	data, err := http_client.GetServiceAccessDeviceList(httpAddress, req.Voucher, strconv.Itoa(req.PageSize), strconv.Itoa(req.Page))
	if err != nil {
		return nil, errcode.NewWithMessage(105001, err.Error())
	}
	// 查询已绑定设备列表
	devices, err := dal.GetServiceDeviceList(serviceAccess.ID)
	if err != nil {
		return nil, err
	}
	for i, dataDevice := range data.List {
		for _, device := range devices {
			if dataDevice.DeviceNumber == device.DeviceNumber {
				data.List[i].IsBind = true
				if device.DeviceConfigID != nil {
					data.List[i].DeviceConfigID = *device.DeviceConfigID
				}
			}
		}
	}
	return data, nil
}

// 通过service_identifier获取插件服务信息
func (*ServiceAccess) GetPluginServiceAccessList(req *model.GetPluginServiceAccessListReq) (interface{}, error) {
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
		} else {
			serviceAccessMap := StructToMap(serviceAccess)
			serviceAccessMap["devices"] = []interface{}{}
			serviceAccessMapList = append(serviceAccessMapList, serviceAccessMap)
		}
	}
	return serviceAccessMapList, nil
}

// GetPluginServiceAccess
func (*ServiceAccess) GetPluginServiceAccess(req *model.GetPluginServiceAccessReq) (interface{}, error) {
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
