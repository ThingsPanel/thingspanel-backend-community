package api

import (
	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type DeviceDebugApi struct{}

// SetDeviceDebug 开启/关闭设备调试日志
// @Router   /api/v1/device/{device_id}/debug [post]
func (*DeviceDebugApi) SetDeviceDebug(c *gin.Context) {
	deviceID := c.Param("device_id")
	if deviceID == "" {
		c.Error(errcode.NewWithMessage(errcode.CodeParamError, "device_id is required"))
		return
	}

	var req model.SetDeviceDebugReq
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceDebug.SetDeviceDebug(c.Request.Context(), deviceID, &req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetDeviceDebugStatus 查询设备调试状态
// @Router   /api/v1/device/{device_id}/debug/status [get]
func (*DeviceDebugApi) GetDeviceDebugStatus(c *gin.Context) {
	deviceID := c.Param("device_id")
	if deviceID == "" {
		c.Error(errcode.NewWithMessage(errcode.CodeParamError, "device_id is required"))
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceDebug.GetDeviceDebugStatus(c.Request.Context(), deviceID, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetDeviceDebugLogs 查询设备调试日志
// @Router   /api/v1/device/{device_id}/debug/logs [get]
func (*DeviceDebugApi) GetDeviceDebugLogs(c *gin.Context) {
	deviceID := c.Param("device_id")
	if deviceID == "" {
		c.Error(errcode.NewWithMessage(errcode.CodeParamError, "device_id is required"))
		return
	}

	var req model.GetDeviceDebugLogsReq
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceDebug.GetDeviceDebugLogs(c.Request.Context(), deviceID, &req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}
