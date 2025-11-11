package api

import (
	model "project/internal/model"
	service "project/internal/service"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type DeviceTopicMappingApi struct{}

// CreateDeviceTopicMapping 创建主题转换规则
// @Router   /api/v1/device/topic-mappings [post]
func (*DeviceTopicMappingApi) CreateDeviceTopicMapping(c *gin.Context) {
	var req model.CreateDeviceTopicMappingReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTopicMapping.CreateDeviceTopicMapping(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// GetDeviceTopicMappings 获取主题转换列表
// @Router   /api/v1/device/topic-mappings [get]
func (*DeviceTopicMappingApi) GetDeviceTopicMappings(c *gin.Context) {
	var req model.ListDeviceTopicMappingReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTopicMapping.ListDeviceTopicMappings(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// UpdateDeviceTopicMapping 更新主题转换
// @Router   /api/v1/device/topic-mappings/{id} [put]
func (*DeviceTopicMappingApi) UpdateDeviceTopicMapping(c *gin.Context) {
	var req model.UpdateDeviceTopicMappingReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTopicMapping.UpdateDeviceTopicMapping(c.Param("id"), &req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// DeleteDeviceTopicMapping 删除主题转换
// @Router   /api/v1/device/topic-mappings/{id} [delete]
func (*DeviceTopicMappingApi) DeleteDeviceTopicMapping(c *gin.Context) {
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	if err := service.GroupApp.DeviceTopicMapping.DeleteDeviceTopicMapping(c.Param("id"), userClaims); err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}
