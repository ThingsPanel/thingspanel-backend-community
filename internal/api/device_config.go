package api

import (
	"net/http"
	"project/pkg/common"

	model "project/internal/model"
	service "project/internal/service"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type DeviceConfigApi struct{}

// CreateDeviceConfig 创建设备配置
// @Router   /api/v1/device_config [post]
func (*DeviceConfigApi) CreateDeviceConfig(c *gin.Context) {
	var req model.CreateDeviceConfigReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceConfig.CreateDeviceConfig(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// UpdateDeviceConfig 更新设备配置
// @Tags     设备配置
// @Summary  更新设备配置
// @Description 更新设备配置
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateDeviceConfigReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "更新设备配置成功"
// @Security ApiKeyAuth
// @Router   /api/v1/device_config [put]
func (*DeviceConfigApi) UpdateDeviceConfig(c *gin.Context) {
	var req model.UpdateDeviceConfigReq
	if !BindAndValidate(c, &req) {
		return
	}

	data, err := service.GroupApp.DeviceConfig.UpdateDeviceConfig(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update deviceconfig successfully", data)
}

// DeleteDeviceConfig 删除设备配置
// @Router   /api/v1/device_config/{id} [delete]
func (*DeviceConfigApi) DeleteDeviceConfig(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DeviceConfig.DeleteDeviceConfig(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GetDeviceConfigById 根据ID获取设备配置
// @Router   /api/v1/device_config/{id} [get]
func (*DeviceConfigApi) HandleDeviceConfigById(c *gin.Context) {
	id := c.Param("id")
	info, err := service.GroupApp.DeviceConfig.GetDeviceConfigByID(c, id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", info)
}

// GetDeviceConfigListByPage 设备配置分页查询
// @Router   /api/v1/device_config [get]
func (*DeviceConfigApi) HandleDeviceConfigListByPage(c *gin.Context) {
	var req model.GetDeviceConfigListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	deviceconfigList, err := service.GroupApp.DeviceConfig.GetDeviceConfigListByPage(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", deviceconfigList)
}

// @Router   /api/v1/device_config/menu [get]
func (*DeviceConfigApi) HandleDeviceConfigListMenu(c *gin.Context) {
	var req model.GetDeviceConfigListMenuReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	deviceconfigList, err := service.GroupApp.DeviceConfig.GetDeviceConfigListMenu(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", deviceconfigList)
}

// BatchUpdateDeviceConfig 批量修改设备配置
// @Router   /api/v1/device_config/batch [put]
func (*DeviceConfigApi) BatchUpdateDeviceConfig(c *gin.Context) {
	var req model.BatchUpdateDeviceConfigReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.DeviceConfig.BatchUpdateDeviceConfig(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// /api/v1/device_config/connect
func (*DeviceConfigApi) HandleDeviceConfigConnect(c *gin.Context) {
	var param model.DeviceIDReq
	if !BindAndValidate(c, &param) {
		return
	}
	data, err := service.GroupApp.DeviceConfig.GetDeviceConfigConnect(c, param.DeviceID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, data)
}

// /api/v1/device_config/voucher_type
func (*DeviceConfigApi) HandleVoucherType(c *gin.Context) {
	var param model.GetVoucherTypeReq
	if !BindAndValidate(c, &param) {
		return
	}
	data, err := service.GroupApp.DeviceConfig.GetVoucherTypeForm(param.DeviceType, param.ProtocolType)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// 根据设备配置id获取自动化动作中下拉列表
// /api/v1/device_config/metrics/menu
func (*DeviceConfigApi) HandleActionByDeviceConfigID(c *gin.Context) {
	var param model.GetActionByDeviceConfigIDReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.DeviceConfig.GetActionByDeviceConfigID(param.DeviceConfigID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", list)
}

// 根据设备配置id获取自动化动作中下拉列表
// /api/v1/device_config/metrics/condition/menu
func (*DeviceConfigApi) HandleConditionByDeviceConfigID(c *gin.Context) {
	var param model.GetActionByDeviceConfigIDReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.DeviceConfig.GetConditionByDeviceConfigID(param.DeviceConfigID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", list)
}
