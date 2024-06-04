package logic

import (
	model "project/model"
)

type DeviceLogic struct {
}

func (DeviceLogic) GetTenantDeviceList(list []*model.Device, configsList []*model.DeviceConfig) []*model.GetTenantDeviceListReq {
	var (
		res = make([]*model.GetTenantDeviceListReq, 0, len(list))

		configMap = make(map[string]*model.DeviceConfig)
	)

	for _, info := range configsList {
		configMap[info.ID] = info
	}

	for _, info := range list {
		resInfo := &model.GetTenantDeviceListReq{
			ID: info.ID,
		}
		if info.Name != nil {
			resInfo.Name = *info.Name
		}
		if info.DeviceConfigID != nil {
			resInfo.DeviceConfigID = *info.DeviceConfigID
		}
		if info.DeviceConfigID != nil {
			if configInfo, ok := configMap[*info.DeviceConfigID]; ok {
				resInfo.DeviceConfigName = configInfo.Name
			}
		}
		res = append(res, resInfo)
	}
	return res
}

func (DeviceLogic) GetDeviceList(list []*model.Device, configsList []*model.DeviceConfig) []*model.GetTenantDeviceListReq {
	var (
		res = make([]*model.GetTenantDeviceListReq, 0, len(list))

		configMap = make(map[string]*model.DeviceConfig)
	)

	for _, info := range configsList {
		configMap[info.ID] = info
	}

	for _, info := range list {
		resInfo := &model.GetTenantDeviceListReq{
			ID: info.ID,
		}
		// 不存在子设备类型，则直接跳过
		if info.DeviceConfigID == nil {
			continue
		}
		configInfo, ok := configMap[*info.DeviceConfigID]
		if !ok {
			continue
		}
		resInfo.DeviceConfigName = configInfo.Name

		if info.Name != nil {
			resInfo.Name = *info.Name
		}
		if info.DeviceConfigID != nil {
			resInfo.DeviceConfigID = *info.DeviceConfigID
		}
		res = append(res, resInfo)
	}
	return res
}
