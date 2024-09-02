package api

import (
	"net/http"
	"project/common"

	model "project/internal/model"
	service "project/service"
	utils "project/utils"

	"github.com/gin-gonic/gin"
)

type DeviceConfigApi struct{}

// CreateDeviceConfig 创建设备配置
// @Tags     设备配置
// @Summary  创建设备配置
// @Description 创建设备配置
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateDeviceConfigReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "创建设备配置成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device_config [post]
func (api *DeviceConfigApi) CreateDeviceConfig(c *gin.Context) {
	var req model.CreateDeviceConfigReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceConfig.CreateDeviceConfig(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create deviceconfig successfully", data)
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
func (api *DeviceConfigApi) UpdateDeviceConfig(c *gin.Context) {
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
// @Tags     设备配置
// @Summary  删除设备配置
// @Description 删除设备配置
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "ID"
// @Success  200  {object}  ApiResponse  "更新设备配置成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device_config/{id} [delete]
func (api *DeviceConfigApi) DeleteDeviceConfig(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DeviceConfig.DeleteDeviceConfig(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete deviceconfig successfully", nil)
}

// GetDeviceConfigById 根据ID获取设备配置
// @Tags     设备配置
// @Summary  根据ID获取设备配置
// @Description 根据ID获取设备配置
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "ID"
// @Success  200  {object}  GetDeviceConfigResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device_config/{id} [get]
func (api *DeviceConfigApi) GetDeviceConfigById(c *gin.Context) {
	id := c.Param("id")
	info, err := service.GroupApp.DeviceConfig.GetDeviceConfigByID(c, id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, info)
}

// GetDeviceConfigListByPage 设备配置分页查询
// @Tags     设备配置
// @Summary  设备配置分页查询
// @Description 设备配置分页查询
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetDeviceConfigListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device_config [get]
func (api *DeviceConfigApi) GetDeviceConfigListByPage(c *gin.Context) {
	var req model.GetDeviceConfigListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	deviceconfigList, err := service.GroupApp.DeviceConfig.GetDeviceConfigListByPage(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get deviceconfig list successfully", deviceconfigList)
}

// @Router   /api/v1/device_config/menu [get]
func (api *DeviceConfigApi) GetDeviceConfigListMenu(c *gin.Context) {
	var req model.GetDeviceConfigListMenuReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	deviceconfigList, err := service.GroupApp.DeviceConfig.GetDeviceConfigListMenu(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get deviceconfig list successfully", deviceconfigList)
}

// BatchUpdateDeviceConfig 批量修改设备配置
// @Tags     设备配置
// @Summary  批量修改设备配置
// @Description 批量修改设备配置
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.BatchUpdateDeviceConfigReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "批量修改设备配置成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device_config/batch [put]
func (api *DeviceConfigApi) BatchUpdateDeviceConfig(c *gin.Context) {
	var req model.BatchUpdateDeviceConfigReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.DeviceConfig.BatchUpdateDeviceConfig(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Batch update deviceconfig successfully", nil)
}

// /api/v1/device_config/connect
func (api *DeviceConfigApi) GetDeviceConfigConnect(c *gin.Context) {
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
func (api *DeviceConfigApi) GetVoucherType(c *gin.Context) {
	var param model.GetVoucherTypeReq
	if !BindAndValidate(c, &param) {
		return
	}
	data, err := service.GroupApp.DeviceConfig.GetVoucherTypeForm(param.DeviceType, param.ProtocolType)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, data)
}

// 根据设备配置id获取自动化动作中下拉列表
// /api/v1/device_config/metrics/menu
func (d *DeviceConfigApi) GetActionByDeviceConfigID(c *gin.Context) {
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
func (d *DeviceConfigApi) GetConditionByDeviceConfigID(c *gin.Context) {
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
