package api

import (
	"errors"
	"net/http"
	model "project/internal/model"
	service "project/internal/service"
	common "project/pkg/common"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
)

type DeviceApi struct{}

// CreateDevice 创建设备
// @Router   /api/v1/device [post]
func (*DeviceApi) CreateDevice(c *gin.Context) {
	var req model.CreateDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.CreateDevice(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create product successfully", data)
}

// 服务接入点批量创建设备
// /api/v1/device/service/access/batch
func (*DeviceApi) CreateDeviceBatch(c *gin.Context) {
	var req model.BatchCreateDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.CreateDeviceBatch(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// DeleteDevice 删除设备
// @Router   /api/v1/device/{id} [delete]
func (*DeviceApi) DeleteDevice(c *gin.Context) {
	id := c.Param("id")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Device.DeleteDevice(id, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete product successfully", nil)
}

// UpdateDevice 更新设备
// @Tags     设备管理
// @Summary  更新设备
// @Description 更新设备
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateDeviceReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device [put]
func (*DeviceApi) UpdateDevice(c *gin.Context) {
	var req model.UpdateDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.UpdateDevice(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update product successfully", data)
}

// ActiveDevice 激活设备
// @Tags     设备管理
// @Summary  激活设备
// @Description 激活设备
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.ActiveDeviceReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "激活设备成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/active [put]
func (*DeviceApi) ActiveDevice(c *gin.Context) {
	var req model.ActiveDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}

	device, err := service.GroupApp.Device.ActiveDevice(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Active product successfully", device)
}

// GetDevice 设备详情
// @Router   /api/v1/device/detail/{id} [get]
func (*DeviceApi) HandleDeviceByID(c *gin.Context) {
	id := c.Param("id")
	device, err := service.GroupApp.Device.GetDeviceByIDV1(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "GetDevice successfully", device)
}

// GetDeviceListByPage 分页查询设备
// @Router   /api/v1/device [get]
func (*DeviceApi) HandleDeviceListByPage(c *gin.Context) {
	var req model.GetDeviceListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	list, err := service.GroupApp.Device.GetDeviceListByPage(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get devices successfully", list)
}

// @Tags     设备管理
// @Router   /api/v1/device/check/{deviceNumber} [get]
func (*DeviceApi) CheckDeviceNumber(c *gin.Context) {
	deviceNumber := c.Param("deviceNumber")
	ok, msg := service.GroupApp.Device.CheckDeviceNumber(deviceNumber)
	data := map[string]interface{}{"is_available": ok}
	SuccessHandler(c, msg, data)
}

// CreateDeviceTemplate 创建设备模版
// @Router   /api/v1/device/template [post]
func (*DeviceApi) CreateDeviceTemplate(c *gin.Context) {
	var req model.CreateDeviceTemplateReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.CreateDeviceTemplate(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// UpdateDeviceTemplate 更新设备模版
// @Router   /api/v1/device/template [put]
func (*DeviceApi) UpdateDeviceTemplate(c *gin.Context) {
	var req model.UpdateDeviceTemplateReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.UpdateDeviceTemplate(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// GetDeviceTemplateListByPage 分页获取设备模版
// @Router   /api/v1/device/template [get]
func (*DeviceApi) HandleDeviceTemplateListByPage(c *gin.Context) {
	var req model.GetDeviceTemplateListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateListByPage(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	serilizedData, err := utils.SerializeData(data, GetDeviceTemplateListData{})
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}

	c.Set("data", serilizedData)
}

// @Router   /api/v1/device/template/menu [get]
func (*DeviceApi) HandleDeviceTemplateMenu(c *gin.Context) {
	var req model.GetDeviceTemplateMenuReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateMenu(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// DeleteDeviceTemplate 删除设备模版
// @Router   /api/v1/device/template/{id} [delete]
func (*DeviceApi) DeleteDeviceTemplate(c *gin.Context) {
	id := c.Param("id")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceTemplate.DeleteDeviceTemplate(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GetDeviceTemplate 获取设备模版详情
// @Router   /api/v1/device/template/detail/{id} [get]
func (*DeviceApi) HandleDeviceTemplateById(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateById(id)
	if err != nil {
		c.Error(err)
		return
	}
	serilizedData, err := utils.SerializeData(data, DeviceTemplateReadSchema{})
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}
	c.Set("data", serilizedData)
}

// 根据设备id获取设备模板详情
// @Router   /api/v1/device/template/chart [get]
func (*DeviceApi) HandleDeviceTemplateByDeviceId(c *gin.Context) {
	deviceId := c.Query("device_id")
	if deviceId == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"device_id": deviceId,
			"msg":       "device_id is required",
		}))
		return
	}
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateByDeviceId(deviceId)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// CreateDeviceGroup 创建设备分组
// @Router   /api/v1/device/group [post]
func (*DeviceApi) CreateDeviceGroup(c *gin.Context) {
	var req model.CreateDeviceGroupReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceGroup.CreateDeviceGroup(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)

}

// DeleteDeviceGroup 删除设备分组
// @Router   /api/v1/device/group/{id} [delete]
func (*DeviceApi) DeleteDeviceGroup(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DeviceGroup.DeleteDeviceGroup(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete device group successfully", nil)
}

// UpdateDeviceGroup 修改设备分组
// @Router   /api/v1/device/group [put]
func (*DeviceApi) UpdateDeviceGroup(c *gin.Context) {
	var req model.UpdateDeviceGroupReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceGroup.UpdateDeviceGroup(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GetDeviceGroupByPage 分页获取设备分组
// @Router   /api/v1/device/group [get]
func (*DeviceApi) HandleDeviceGroupByPage(c *gin.Context) {
	var req model.GetDeviceGroupsListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupListByPage(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetDeviceGroupByTree 获取设备分组树
// @Router   /api/v1/device/group/tree [get]
func (*DeviceApi) HandleDeviceGroupByTree(c *gin.Context) {
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupByTree(userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetDeviceGroupByDetail 获取设备分组详情
// @Router   /api/v1/device/group/detail/{id} [get]
func (*DeviceApi) HandleDeviceGroupByDetail(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupDetail(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// CreateDeviceGroupRelation 创建设备分组关系
// @Router   /api/v1/device/group/relation [post]
func (*DeviceApi) CreateDeviceGroupRelation(c *gin.Context) {
	var req model.CreateDeviceGroupRelationReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceGroup.CreateDeviceGroupRelation(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create device group relation successfully", nil)

}

// DeleteDeviceGroupRelation 删除设备分组关系
// @Router   /api/v1/device/group/relation [delete]
func (*DeviceApi) DeleteDeviceGroupRelation(c *gin.Context) {
	var req model.DeleteDeviceGroupRelationReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.DeviceGroup.DeleteDeviceGroupRelation(req.GroupId, req.DeviceId)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete device group relation successfully", nil)
}

// GetDeviceGroupRelation 获取设备分组关系
// @Router   /api/v1/device/group/relation/list [get]
func (*DeviceApi) HandleDeviceGroupRelation(c *gin.Context) {
	var req model.GetDeviceListByGroup
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupRelation(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get device group relation successfully", data)
}

// GetDeviceGroupListByDeviceId 获取设备所属分组列表
// @Router   /api/v1/device/group/relation [get]
func (*DeviceApi) HandleDeviceGroupListByDeviceId(c *gin.Context) {
	var req model.GetDeviceGroupListByDeviceIdReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupByDeviceId(req.DeviceId)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)
}

// 移除子设备
// /api/v1/device/sub-remove
func (*DeviceApi) RemoveSubDevice(c *gin.Context) {
	var req model.RemoveSonDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Device.RemoveSubDevice(req.SubDeviceId, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Remove sub device successfully", nil)
}

// GetTenantDeviceList
// @AUTHOR:zxq
// @DATE: 2024-04-06 18:04
// @DESCRIPTIONS: 获得租户下设备列表
// /api/v1/device/tenant/list
func (*DeviceApi) HandleTenantDeviceList(c *gin.Context) {
	var req model.GetDeviceMenuReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.GetTenantDeviceList(&req, userClaims.TenantID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, data)
}

// GetDeviceList
// @AUTHOR:zxq
// @DATE: 2024-04-07 17:04
// @DESCRIPTIONS: 获得设备列表（默认：设置类型-子设备&无 parent_id 关联 可扩展，查询可添加条件）
// /api/v1/device/list
func (*DeviceApi) HandleDeviceList(c *gin.Context) {
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.GetDeviceList(c, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, data)
}

// CreateSonDevice
// @AUTHOR:zxq
// @DATE: 2024-04-07 17:04
// @DESCRIPTIONS: 添加子设备
// /api/v1/device/son/add
func (*DeviceApi) CreateSonDevice(c *gin.Context) {
	var param model.CreateSonDeviceRes
	if !BindAndValidate(c, &param) {
		return
	}

	err := service.GroupApp.Device.CreateSonDevice(c, &param)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessOK(c)
}

// DeviceConnectForm
// @AUTHOR:zxq
// @DATE: 2024-04-08 20:04
// @DESCRIPTIONS: 连接-凭单
// /api/v1/device/connect/form
func (*DeviceApi) DeviceConnectForm(c *gin.Context) {
	var param model.DeviceConnectFormReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.Device.DeviceConnectForm(c, &param)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, list)
}

// DeviceConnect
// @AUTHOR:zxq
// @DATE: 2024-04-09 16:04
// @DESCRIPTIONS: 连接
// /api/v1/device/connect/info
func (*DeviceApi) DeviceConnect(c *gin.Context) {
	var param model.DeviceConnectFormReq
	if !BindAndValidate(c, &param) {
		return
	}
	// 获取语言设置
	lang := c.Request.Header.Get("Accept-Language")
	if lang == "" {
		lang = "zh_CN"
	}

	list, err := service.GroupApp.Device.DeviceConnect(c, &param, lang)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// UpdateDeviceVoucher
// @AUTHOR:zxq
// @DATE: 2024-04-15 16:04
// @DESCRIPTIONS: 更新
// /api/v1/device/update/voucher
func (*DeviceApi) UpdateDeviceVoucher(c *gin.Context) {
	var param model.UpdateDeviceVoucherReq
	if !BindAndValidate(c, &param) {
		return
	}
	voucher, err := service.GroupApp.Device.UpdateDeviceVoucher(c, &param)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, voucher)
}

// GetSubList
// @AUTHOR:wzc
// @DATE: 2024-03-15 16:04
// @DESCRIPTIONS: 更新
// /api/v1/device/sub-list/{id}
func (*DeviceApi) HandleSubList(c *gin.Context) {
	var req model.PageReq
	parant_id := c.Param("id")
	if parant_id == "" {
		ErrorHandler(c, http.StatusInternalServerError, errors.New("缺少参数"))
		return
	}
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	list, total, err := service.GroupApp.Device.GetSubList(c, parant_id, int64(req.Page), int64(req.PageSize), userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, map[string]interface{}{
		"total": total,
		"list":  list,
	})
}

// /api/v1/device/metrics/{id}
func (*DeviceApi) HandleMetrics(c *gin.Context) {
	id := c.Param("id")
	list, err := service.GroupApp.Device.GetMetrics(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// GetActionByDeviceID
// 单设备动作选择下拉菜单
// /api/v1/device/metrics/menu
func (*DeviceApi) HandleActionByDeviceID(c *gin.Context) {
	var param model.GetActionByDeviceIDReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.Device.GetActionByDeviceID(param.DeviceID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", list)
}

// GetConditionByDeviceID
// 单设备动作选择下拉菜单
// /api/v1/device/metrics/condition/menu
func (*DeviceApi) HandleConditionByDeviceID(c *gin.Context) {
	var param model.GetActionByDeviceIDReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.Device.GetConditionByDeviceID(param.DeviceID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", list)
}

// /api/v1/device/map/telemetry/{id}
func (*DeviceApi) HandleMapTelemetry(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.Device.GetMapTelemetry(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)
}

// 有模板且有图表配置的设备下拉列表
// /api/v1/device/template/chart/select
func (*DeviceApi) HandleDeviceTemplateChartSelect(c *gin.Context) {
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	list, err := service.GroupApp.Device.GetDeviceTemplateChartSelect(userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// 更换设备配置UpdateDeviceConfig
// /api/v1/device/update/config
func (*DeviceApi) UpdateDeviceConfig(c *gin.Context) {
	var param model.ChangeDeviceConfigReq
	if !BindAndValidate(c, &param) {
		return
	}
	err := service.GroupApp.Device.UpdateDeviceConfig(&param)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessOK(c)
}

func (*DeviceApi) HandleDeviceOnlineStatus(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.Device.GetDeviceOnlineStatus(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)

}

func (*DeviceApi) GatewayRegister(c *gin.Context) {
	var req model.GatewayRegisterReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.Device.GatewayRegister(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create product successfully", data)
}

func (*DeviceApi) GatewaySubRegister(c *gin.Context) {
	var req model.DeviceRegisterReq
	if !BindAndValidate(c, &req) {
		logrus.Warningf("GatewaySubRegister:%#v", req)
		return
	}
	logrus.Warningf("GatewaySubRegister:%#v", req)
	data, err := service.GroupApp.Device.GatewayDeviceRegister(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create product successfully", data)
}
