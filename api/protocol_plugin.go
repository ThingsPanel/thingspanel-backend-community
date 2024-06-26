package api

import (
	"net/http"

	model "project/model"
	service "project/service"

	"github.com/gin-gonic/gin"
)

type ProtocolPluginApi struct{}

// CreateProtocolPlugin 创建协议插件
// @Tags     协议插件
// @Summary  创建协议插件
// @Description 该接口用于创建协议插件，同时创建字典的两张表
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateProtocolPluginReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/protocol_plugin [post]
func (p *ProtocolPluginApi) CreateProtocolPlugin(c *gin.Context) {
	var req model.CreateProtocolPluginReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.ProtocolPlugin.CreateProtocolPlugin(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create protocol plugin successfully", data)
}

// DeleteProtocolPlugin 删除协议插件
// @Tags     协议插件
// @Summary  删除协议插件
// @Description 删除协议插件，同时删除字典的两张表
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "协议插件ID"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/protocol_plugin/{id} [delete]
func (p *ProtocolPluginApi) DeleteProtocolPlugin(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.ProtocolPlugin.DeleteProtocolPlugin(id)

	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete protocol plugin successfully", id)
}

// UpdateProtocolPlugin 更新协议插件
// @Tags     协议插件
// @Summary  更新协议插件
// @Description 更新协议插件，同时更新字典的两张表
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateProtocolPluginReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/protocol_plugin [put]
func (p *ProtocolPluginApi) UpdateProtocolPlugin(c *gin.Context) {
	var req model.UpdateProtocolPluginReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.ProtocolPlugin.UpdateProtocolPlugin(&req)

	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Update protocol plugin successfully", nil)
}

// UpdateProtocolPlugin 分页查询协议插件
// @Tags     协议插件
// @Summary  分页查询协议插件
// @Description 分页查询协议插件
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.GetProtocolPluginListByPageReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Security ApiKeyAuth
// @Router   /api/v1/protocol_plugin [get]
func (p *ProtocolPluginApi) GetProtocolPluginListByPage(c *gin.Context) {
	var req model.GetProtocolPluginListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	list, err := service.GroupApp.ProtocolPlugin.GetProtocolPluginListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get protocol plugin successfully", list)
}

// GetProtocolPluginForm 获取设备配置表单
// @Tags     协议插件
// @Summary  获取设备配置表单
// @Description 网关设备为网关表单，子设备为子设备表单
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.GetProtocolPluginFormReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "成功"
// @Security ApiKeyAuth
// @Router   /api/v1/protocol_plugin/device_config_form [get]
func (p *ProtocolPluginApi) GetProtocolPluginForm(c *gin.Context) {
	var req model.GetProtocolPluginFormReq
	if !BindAndValidate(c, &req) {
		return
	}

	data, err := service.GroupApp.ProtocolPlugin.GetProtocolPluginForm(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)
}

// GetProtocolPluginForm 根据协议类型获取设备配置表单
// @Tags     协议插件
// @Summary  根据协议类型获取设备配置表单
// @Description 网关设备为网关表单，子设备为子设备表单
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.GetProtocolPluginFormByProtocolType  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "成功"
// @Security ApiKeyAuth
// @Router   /api/v1/protocol_plugin/config_form [get]
func (p *ProtocolPluginApi) GetProtocolPluginFormByProtocolType(c *gin.Context) {
	var req model.GetProtocolPluginFormByProtocolType
	if !BindAndValidate(c, &req) {
		return
	}

	data, err := service.GroupApp.ServicePlugin.GetProtocolPluginFormByProtocolType(req.ProtocolType, req.DeviceType)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)
}

// /api/v1/plugin/device/config
// 协议插件获取设备配置
func (p *ProtocolPluginApi) GetDeviceConfigForProtocolPlugin(c *gin.Context) {
	var req model.GetDeviceConfigReq
	if !BindAndValidate(c, &req) {
		return
	}

	data, err := service.GroupApp.ProtocolPlugin.GetDeviceConfig(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)
}
