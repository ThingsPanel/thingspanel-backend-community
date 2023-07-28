package services

import (
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"fmt"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type OpenApiCommonService struct {
}

// 设备权限验证 deviceId
func (s *OpenApiCommonService) IsAccessDeviceId(ctx *context2.Context, id string) bool {
	//设备范围
	device_access_scope := ctx.Input.GetData("device_access_scope")
	if device_access_scope == "1" {
		return true
	}
	accessDevices := s.getDeviceIds(ctx)
	for _, d := range accessDevices {
		if id == d {
			return true
		}
	}
	return false
}

// 对比 传入ids 和权限ids  返回 id集合
func (s *OpenApiCommonService) GetAccessDeviceIds(ctx *context2.Context, ids []string) (bool, []string) {
	//设备范围
	device_access_scope := ctx.Input.GetData("device_access_scope")
	if device_access_scope == "1" {
		return true, nil
	}
	authIds := s.getDeviceIds(ctx)
	accessIds := []string{}
	for _, id := range ids {
		for _, aid := range authIds {
			if id == aid {
				accessIds = append(accessIds, aid)
			}
		}
	}
	return false, accessIds

}

// 对比 传入ids 和权限ids  返回 设备集合
func (s *OpenApiCommonService) GetAccessDevices(ctx *context2.Context, devices []models.Device) (bool, []models.Device) {
	//设备范围
	device_access_scope := ctx.Input.GetData("device_access_scope")
	if device_access_scope == "1" {
		return true, nil
	}
	authIds := s.getDeviceIds(ctx)
	accessDevices := []models.Device{}
	for _, device := range devices {
		for _, aid := range authIds {
			if device.ID == aid {
				accessDevices = append(accessDevices, device)
			}
		}
	}
	return false, accessDevices

}

// 获取所有有权限的设备id
func (s *OpenApiCommonService) getDeviceIds(ctx *context2.Context) []string {
	appKey := ctx.Input.GetData("app_key")
	var openapiService services.OpenApiService
	apiAuth := openapiService.GetOpenApiAuth(fmt.Sprint(appKey))
	accessDevices := openapiService.GetAuthDevicesByAuthId(apiAuth.ID)
	ids := []string{}
	for _, d := range accessDevices {
		ids = append(ids, d.DeviceId)
	}
	return ids
}

// 对比 传入ids 和权限ids  返回 设备集合
func (s *OpenApiCommonService) GetAllAccessDeviceIds(ctx *context2.Context) (bool, []string) {
	//设备范围
	device_access_scope := ctx.Input.GetData("device_access_scope")
	if device_access_scope == "1" {
		return true, nil
	}
	authIds := s.getDeviceIds(ctx)
	return false, authIds

}
