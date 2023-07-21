package services

import (
	"fmt"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type OpenApiCommonService struct {
	OpenApiService
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

// 获取所有有权限的设备id
func (s *OpenApiCommonService) getDeviceIds(ctx *context2.Context) []string {
	appKey := ctx.Input.GetData("app_key")
	apiAuth := s.GetOpenApiAuth(fmt.Sprint(appKey))
	accessDevices := s.getAuthDevicesByAuthId(apiAuth.ID)
	ids := []string{}
	for _, d := range accessDevices {
		ids = append(ids, d.TpDeviceId)
	}
	return ids
}
