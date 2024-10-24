package api

import (
	"errors"
	"net/http"
	model "project/internal/model"
	service "project/internal/service"
	common "project/pkg/common"
	utils "project/pkg/utils"

	"github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
)

type DeviceApi struct{}

// CreateDevice 创建设备
// @Tags     设备管理
// @Summary  创建设备
// @Description 创建设备
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateDeviceReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Security ApiKeyAuth
// @Router   /api/v1/device [post]
func (d *DeviceApi) CreateDevice(c *gin.Context) {
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
func (d *DeviceApi) CreateDeviceBatch(c *gin.Context) {
	var req model.BatchCreateDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.CreateDeviceBatch(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create product successfully", data)
}

// DeleteDevice 删除设备
// @Router   /api/v1/device/{id} [delete]
func (d *DeviceApi) DeleteDevice(c *gin.Context) {
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
func (d *DeviceApi) UpdateDevice(c *gin.Context) {
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
func (d *DeviceApi) ActiveDevice(c *gin.Context) {
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
func (d *DeviceApi) GetDeviceByID(c *gin.Context) {
	id := c.Param("id")
	device, err := service.GroupApp.Device.GetDeviceByID(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "GetDevice successfully", device)
}

// GetDeviceListByPage 分页查询设备
// @Tags     设备管理
// @Summary  分页查询设备
// @Description 分页查询设备
// @accept    application/json
// @Produce   application/json
// @Param     data  query      model.GetDeviceListByPageReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Security ApiKeyAuth
// @Router   /api/v1/device [get]
func (d *DeviceApi) GetDeviceListByPage(c *gin.Context) {
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

// CheckDeviceNumber 编号校验
// @Tags     设备管理
// @Summary  编号校验
// @Description 编号校验
// @accept    application/json
// @Produce   application/json
// @Param    deviceNumber  path      string     true  "设备编号"
// @Success  200  {object}  ApiResponse  "编号校验成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/check/{deviceNumber} [get]
func (d *DeviceApi) CheckDeviceNumber(c *gin.Context) {
	deviceNumber := c.Param("deviceNumber")
	ok, msg := service.GroupApp.Device.CheckDeviceNumber(deviceNumber)
	data := map[string]interface{}{"is_available": ok}
	SuccessHandler(c, msg, data)
}

// CreateDeviceTemplate 创建设备模版
// @Tags     设备模版管理
// @Summary  创建设备模版
// @Description 创建设备模版
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateDeviceTemplateReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Security ApiKeyAuth
// @Router   /api/v1/device/template [post]
func (d *DeviceApi) CreateDeviceTemplate(c *gin.Context) {
	var req model.CreateDeviceTemplateReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.CreateDeviceTemplate(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create device template successfully", data)
}

// UpdateDeviceTemplate 更新设备模版
// @Tags     设备模版管理
// @Summary  更新设备模版
// @Description 更新设备模版
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateDeviceTemplateReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Security ApiKeyAuth
// @Router   /api/v1/device/template [put]
func (d *DeviceApi) UpdateDeviceTemplate(c *gin.Context) {
	var req model.UpdateDeviceTemplateReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.UpdateDeviceTemplate(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update device template successfully", data)
}

// GetDeviceTemplateListByPage 分页获取设备模版
// @Tags     设备模版管理
// @Summary  分页获取设备模版
// @Description 分页获取设备模版
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetDeviceTemplateListByPageReq true "见下方JSON"
// @Success  200  {object}  GetDeviceTemplateListResponse  "获取设备模版成功"
// @Security ApiKeyAuth
// @Router   /api/v1/device/template [get]
func (d *DeviceApi) GetDeviceTemplateListByPage(c *gin.Context) {
	var req model.GetDeviceTemplateListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateListByPage(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	serilizedData, err := utils.SerializeData(data, GetDeviceTemplateListData{})
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get device template successfully", serilizedData)
}

// @Router   /api/v1/device/template/menu [get]
func (d *DeviceApi) GetDeviceTemplateMenu(c *gin.Context) {
	var req model.GetDeviceTemplateMenuReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateMenu(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get device template successfully", data)
}

// DeleteDeviceTemplate 删除设备模版
// @Tags     设备模版管理
// @Summary  删除设备模版
// @Description 删除设备模版
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "设备模版ID"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/template/{id} [delete]
func (d *DeviceApi) DeleteDeviceTemplate(c *gin.Context) {
	id := c.Param("id")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceTemplate.DeleteDeviceTemplate(id, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete device template successfully", nil)
}

// GetDeviceTemplate 获取设备模版详情
// @Tags     设备模版管理
// @Summary  获取设备模版详情
// @Description 获取设备模版详情
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "设备模版ID"
// @Success  200  {object}  GetDeviceTemplateResponse  "获取设备模版详情成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/template/detail/{id} [get]
func (d *DeviceApi) GetDeviceTemplateById(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateById(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	serilizedData, err := utils.SerializeData(data, DeviceTemplateReadSchema{})
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get device template successfully", serilizedData)
}

// 根据设备id获取设备模板详情
// @Router   /api/v1/device/template/chart [get]
func (d *DeviceApi) GetDeviceTemplateByDeviceId(c *gin.Context) {
	deviceId := c.Query("device_id")
	if deviceId == "" {
		ErrorHandler(c, http.StatusBadRequest, errors.New("device_id is required"))
		return
	}
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateByDeviceId(deviceId)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get device template successfully", data)
}

// CreateDeviceGroup 创建设备分组
// @Tags     设备分组管理
// @Summary  创建设备分组
// @Description 创建设备分组
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateDeviceGroupReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "分组创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group [post]
func (d *DeviceApi) CreateDeviceGroup(c *gin.Context) {
	var req model.CreateDeviceGroupReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceGroup.CreateDeviceGroup(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create device group successfully", nil)

}

// DeleteDeviceGroup 删除设备分组
// @Tags     设备分组管理
// @Summary  删除设备分组
// @Description 删除设备分组
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "设备模版ID"
// @Success  200  {object}  ApiResponse  "设备分组删除成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group/{id} [delete]
func (d *DeviceApi) DeleteDeviceGroup(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DeviceGroup.DeleteDeviceGroup(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete device group successfully", nil)
}

// UpdateDeviceGroup 修改设备分组
// @Tags     设备分组管理
// @Summary  修改设备分组
// @Description 创建设备分组
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateDeviceGroupReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "分组修改成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group [put]
func (d *DeviceApi) UpdateDeviceGroup(c *gin.Context) {
	var req model.UpdateDeviceGroupReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceGroup.UpdateDeviceGroup(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Update device group successfully", nil)
}

// GetDeviceGroupByPage 分页获取设备分组
// @Tags     设备分组管理
// @Summary  分页获取设备分组
// @Description 分页获取设备分组
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.GetDeviceGroupsListByPageReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "分组获取成功"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group [get]
func (d *DeviceApi) GetDeviceGroupByPage(c *gin.Context) {
	var req model.GetDeviceGroupsListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupListByPage(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get device group successfully", data)
}

// GetDeviceGroupByTree 获取设备分组树
// @Tags     设备分组管理
// @Summary  获取设备分组树
// @Description 获取设备分组树
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse  "查询成功"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group/tree [get]
func (d *DeviceApi) GetDeviceGroupByTree(c *gin.Context) {
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupByTree(userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get device group successfully", data)
}

// GetDeviceGroupByDetail 获取设备分组详情
// @Tags     设备分组管理
// @Summary  获取设备分组详情
// @Description 获取设备分组详情
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "设备分组ID"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group/detail/{id} [get]
func (d *DeviceApi) GetDeviceGroupByDetail(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupDetail(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get device group successfully", data)
}

// CreateDeviceGroupRelation 创建设备分组关系
// @Tags     设备分组关系管理
// @Summary  创建设备分组关系
// @Description 创建设备分组关系
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateDeviceGroupRelationReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "分组创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group/relation [post]
func (d *DeviceApi) CreateDeviceGroupRelation(c *gin.Context) {
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
// @Tags     设备分组关系管理
// @Summary  删除设备分组关系
// @Description 删除设备分组关系
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.DeleteDeviceGroupRelationReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "删除分组成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group/relation [delete]
func (d *DeviceApi) DeleteDeviceGroupRelation(c *gin.Context) {
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
// @Tags     设备分组管理
// @Summary  获取设备分组关系
// @Description 获取设备分组关系
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "设备分组ID"
// @Success  200  {object}  ApiResponse  "设备分组删除成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group/relation/list [get]
func (d *DeviceApi) GetDeviceGroupRelation(c *gin.Context) {
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
// @Tags     设备分组管理
// @Summary  获取设备所属分组列表
// @Description 获取设备所属分组列表
// @accept    application/json
// @Produce   application/json
// @Param    device_id  path      string     true  "设备ID"
// @Success  200  {object}  ApiResponse  "success"
// @Security ApiKeyAuth
// @Router   /api/v1/device/group/relation [get]
func (d *DeviceApi) GetDeviceGroupListByDeviceId(c *gin.Context) {
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
func (d *DeviceApi) RemoveSubDevice(c *gin.Context) {
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
func (d *DeviceApi) GetTenantDeviceList(c *gin.Context) {
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
func (d *DeviceApi) GetDeviceList(c *gin.Context) {
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
func (d *DeviceApi) CreateSonDevice(c *gin.Context) {
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
func (d *DeviceApi) DeviceConnectForm(c *gin.Context) {
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
func (d *DeviceApi) DeviceConnect(c *gin.Context) {
	var param model.DeviceConnectFormReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.Device.DeviceConnect(c, &param)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, list)
}

// UpdateDeviceVoucher
// @AUTHOR:zxq
// @DATE: 2024-04-15 16:04
// @DESCRIPTIONS: 更新
// /api/v1/device/update/voucher
func (d *DeviceApi) UpdateDeviceVoucher(c *gin.Context) {
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
func (d *DeviceApi) GetSubList(c *gin.Context) {
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
func (d *DeviceApi) GetMetrics(c *gin.Context) {
	id := c.Param("id")
	list, err := service.GroupApp.Device.GetMetrics(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", list)
}

// GetActionByDeviceID
// 单设备动作选择下拉菜单
// /api/v1/device/metrics/menu
func (d *DeviceApi) GetActionByDeviceID(c *gin.Context) {
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
func (d *DeviceApi) GetConditionByDeviceID(c *gin.Context) {
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
func (d *DeviceApi) GetMapTelemetry(c *gin.Context) {
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
func (d *DeviceApi) GetDeviceTemplateChartSelect(c *gin.Context) {
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	list, err := service.GroupApp.Device.GetDeviceTemplateChartSelect(userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", list)
}

// 更换设备配置UpdateDeviceConfig
// /api/v1/device/update/config
func (d *DeviceApi) UpdateDeviceConfig(c *gin.Context) {
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

func (d *DeviceApi) GetDeviceOnlineStatus(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.Device.GetDeviceOnlineStatus(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)

}

func (d *DeviceApi) GatewayRegister(c *gin.Context) {
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

func (d *DeviceApi) GatewaySubRegister(c *gin.Context) {
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
