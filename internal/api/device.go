package api

import (
	"project/internal/diagnostics"
	model "project/internal/model"
	service "project/internal/service"
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.CreateDevice(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// 服务接入点批量创建设备
// /api/v1/device/service/access/batch [post]
func (*DeviceApi) CreateDeviceBatch(c *gin.Context) {
	var req model.BatchCreateDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Device.DeleteDevice(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// UpdateDevice 更新设备
// @Router   /api/v1/device [put]
func (*DeviceApi) UpdateDevice(c *gin.Context) {
	var req model.UpdateDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.UpdateDevice(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// ActiveDevice 激活设备
// @Router   /api/v1/device/active [put]
func (*DeviceApi) ActiveDevice(c *gin.Context) {
	var req model.ActiveDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}

	device, err := service.GroupApp.Device.ActiveDevice(req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", device)
}

// GetDevice 设备详情
// @Router   /api/v1/device/detail/{id} [get]
func (*DeviceApi) HandleDeviceByID(c *gin.Context) {
	id := c.Param("id")
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	device, err := service.GroupApp.Device.GetDeviceByIDV1(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", device)
}

// GetDeviceListByPage 分页查询设备
// @Router   /api/v1/device [get]
func (*DeviceApi) HandleDeviceListByPage(c *gin.Context) {
	var req model.GetDeviceListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	list, err := service.GroupApp.Device.GetDeviceListByPage(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// @Tags     设备管理
// @Router   /api/v1/device/check/{deviceNumber} [get]
func (*DeviceApi) CheckDeviceNumber(c *gin.Context) {
	deviceNumber := c.Param("deviceNumber")
	ok, _ := service.GroupApp.Device.CheckDeviceNumber(deviceNumber)
	data := map[string]interface{}{"is_available": ok}
	c.Set("data", data)
}

// CreateDeviceTemplate 创建设备模版
// @Router   /api/v1/device/template [post]
func (*DeviceApi) CreateDeviceTemplate(c *gin.Context) {
	var req model.CreateDeviceTemplateReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateMenu(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// HandleDeviceTemplateStats 获取设备物模型统计信息
// @Router   /api/v1/device/template/stats [get]
func (*DeviceApi) HandleDeviceTemplateStats(c *gin.Context) {
	var req model.GetDeviceTemplateStatsReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateStats(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// HandleDeviceTemplateSelector 获取设备物模型选择器
// @Router   /api/v1/device/template/selector [get]
func (*DeviceApi) HandleDeviceTemplateSelector(c *gin.Context) {
	var req model.GetDeviceTemplateSelectorReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceTemplate.GetDeviceTemplateSelector(req, userClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
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

// MarketLogin 登录市场获取 Token
// @Router   /api/v1/device/template/market/login [post]
func (*DeviceApi) MarketLogin(c *gin.Context) {
	var req model.MarketLoginReq
	if !BindAndValidate(c, &req) {
		return
	}

	client := service.NewMarketClient()
	token, err := client.Login(c, req.Username, req.Password)
	if err != nil {
		c.Error(errcode.NewWithMessage(errcode.CodeSystemError, err.Error()))
		return
	}

	c.Set("data", map[string]string{
		"token": token,
	})
}

// PublishToMarket 发布模板到市场
// @Router   /api/v1/device/template/market/publish [post]
func (*DeviceApi) PublishToMarket(c *gin.Context) {
	var req model.PublishToMarketReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	// 调用服务层
	apiResp, err := service.GroupApp.DeviceTemplate.PublishToMarket(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", apiResp)
}

// ListMarketTemplates 获取市场模板列表
// @Router   /api/v1/device/template/market/list [get]
func (*DeviceApi) ListMarketTemplates(c *gin.Context) {
	var req model.MarketTemplateListReq
	if !BindAndValidate(c, &req) {
		return
	}

	var keyword, category, sortBy string
	if req.Keyword != nil {
		keyword = *req.Keyword
	}
	if req.Category != nil {
		category = *req.Category
	}
	if req.SortBy != nil {
		sortBy = *req.SortBy
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	client := service.NewMarketClient()
	data, err := client.ListMarketTemplates(c, keyword, category, sortBy, req.Page, req.PageSize)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to list market templates: " + err.Error(),
		}))
		return
	}
	c.Set("data", data)
}

// GetMarketTemplateDetail 获取市场模板详情
// @Router   /api/v1/device/template/market/detail/:market_id [get]
func (*DeviceApi) GetMarketTemplateDetail(c *gin.Context) {
	marketID := c.Param("market_id")
	if marketID == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "market_id is required"))
		return
	}

	client := service.NewMarketClient()
	data, err := client.GetMarketTemplateDetail(c, marketID)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to get market template detail: " + err.Error(),
		}))
		return
	}
	c.Set("data", data)
}

// InstallFromMarket 从市场安装模板
// @Router   /api/v1/device/template/market/install [post]
func (*DeviceApi) InstallFromMarket(c *gin.Context) {
	var req model.InstallFromMarketReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	data, err := service.GroupApp.DeviceTemplate.InstallFromMarket(req, userClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceGroup.DeleteDeviceGroup(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// UpdateDeviceGroup 修改设备分组
// @Router   /api/v1/device/group [put]
func (*DeviceApi) UpdateDeviceGroup(c *gin.Context) {
	var req model.UpdateDeviceGroupReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupDetail(id, userClaims)
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
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceGroup.CreateDeviceGroupRelation(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// DeleteDeviceGroupRelation 删除设备分组关系
// @Router   /api/v1/device/group/relation [delete]
func (*DeviceApi) DeleteDeviceGroupRelation(c *gin.Context) {
	var req model.DeleteDeviceGroupRelationReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceGroup.DeleteDeviceGroupRelation(req.GroupId, req.DeviceId, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GetDeviceGroupRelation 获取设备分组关系
// @Router   /api/v1/device/group/relation/list [get]
func (*DeviceApi) HandleDeviceGroupRelation(c *gin.Context) {
	var req model.GetDeviceListByGroup
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupRelation(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetDeviceGroupListByDeviceId 获取设备所属分组列表
// @Router   /api/v1/device/group/relation [get]
func (*DeviceApi) HandleDeviceGroupListByDeviceId(c *gin.Context) {
	var req model.GetDeviceGroupListByDeviceIdReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceGroup.GetDeviceGroupByDeviceId(req.DeviceId, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// 移除子设备
// /api/v1/device/sub-remove
func (*DeviceApi) RemoveSubDevice(c *gin.Context) {
	var req model.RemoveSonDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Device.RemoveSubDevice(req.SubDeviceId, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GetTenantDeviceList
// @AUTHOR:zxq
// @DATE: 2024-04-06 18:04
// @DESCRIPTIONS: 获得租户下设备列表
// /api/v1/device/tenant/list [get]
func (*DeviceApi) HandleTenantDeviceList(c *gin.Context) {
	var req model.GetDeviceMenuReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.GetTenantDeviceList(&req, userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetDeviceList
// @AUTHOR:zxq
// @DATE: 2024-04-07 17:04
// @DESCRIPTIONS: 获得未绑定的设备列表（支持网关设备和子设备，可通过device_type参数过滤）
// /api/v1/device/list [get]
func (*DeviceApi) HandleDeviceList(c *gin.Context) {
	var req model.GetUnboundGatewaySubDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.GetDeviceList(c, userClaims, &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
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
		c.Error(err)
		return
	}
	c.Set("data", nil)
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
		c.Error(err)
		return
	}
	c.Set("data", list)
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
// /api/v1/device/update/voucher [post]
func (*DeviceApi) UpdateDeviceVoucher(c *gin.Context) {
	var param model.UpdateDeviceVoucherReq
	if !BindAndValidate(c, &param) {
		return
	}
	voucher, err := service.GroupApp.Device.UpdateDeviceVoucher(c, &param)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", voucher)
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
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"msg": "no parant_id",
		}))
		return
	}
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	list, total, err := service.GroupApp.Device.GetSubList(c, parant_id, int64(req.Page), int64(req.PageSize), userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", map[string]interface{}{
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
// /api/v1/device/metrics/menu [get]
func (*DeviceApi) HandleActionByDeviceID(c *gin.Context) {
	var param model.GetActionByDeviceIDReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.Device.GetActionByDeviceID(param.DeviceID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// GetConditionByDeviceID
// 单设备动作选择下拉菜单
// /api/v1/device/metrics/condition/menu [get]
func (*DeviceApi) HandleConditionByDeviceID(c *gin.Context) {
	var param model.GetActionByDeviceIDReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.Device.GetConditionByDeviceID(param.DeviceID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// /api/v1/device/map/telemetry/{id}
func (*DeviceApi) HandleMapTelemetry(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.Device.GetMapTelemetry(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// 有模板且有图表配置的设备下拉列表
// /api/v1/device/template/chart/select
func (*DeviceApi) HandleDeviceTemplateChartSelect(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	list, err := service.GroupApp.Device.GetDeviceTemplateChartSelect(userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// 更换设备配置UpdateDeviceConfig
// /api/v1/device/update/config [put]
func (*DeviceApi) UpdateDeviceConfig(c *gin.Context) {
	var param model.ChangeDeviceConfigReq
	if !BindAndValidate(c, &param) {
		return
	}
	err := service.GroupApp.Device.UpdateDeviceConfig(&param)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/device/online/status/{id} [get]
func (*DeviceApi) HandleDeviceOnlineStatus(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.Device.GetDeviceOnlineStatus(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

func (*DeviceApi) GatewayRegister(c *gin.Context) {
	var req model.GatewayRegisterReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.Device.GatewayRegister(req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
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
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// 设备单指标图表数据查询
// /api/v1/device/metrics/chart [get]
func (*DeviceApi) HandleDeviceMetricsChart(c *gin.Context) {
	var param model.GetDeviceMetricsChartReq
	if !BindAndValidate(c, &param) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)

	data, err := service.GroupApp.Device.GetDeviceMetricsChart(&param, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// 设备选择器
// /api/v1/device/selector [get]
func (*DeviceApi) HandleDeviceSelector(c *gin.Context) {
	var req model.DeviceSelectorReq
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	list, err := service.GroupApp.Device.GetDeviceSelector(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// 获取租户下最近上报数据的三个设备的遥测数据
// /api/v1/device/telemetry/latest [get]
func (*DeviceApi) HandleTenantTelemetryData(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	data, err := service.GroupApp.Device.GetTenantTelemetryData(userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetDeviceDiagnostics 获取设备诊断数据
// @Router   /api/v1/devices/{device_id}/diagnostics [get]
func (*DeviceApi) GetDeviceDiagnostics(c *gin.Context) {
	deviceID := c.Param("device_id")
	if deviceID == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "device_id is required"))
		return
	}

	collector := diagnostics.GetInstance()
	data, err := collector.GetDiagnostics(deviceID)
	if err != nil {
		// 如果未初始化或数据不存在，返回空数据
		if err == diagnostics.ErrNotInitialized {
			c.Set("data", &diagnostics.DiagnosticsResponse{
				DeviceID:       deviceID,
				Stats:          nil,
				RecentFailures: []diagnostics.FailureRecord{},
			})
			return
		}
		c.Error(errcode.WithData(errcode.CodeSystemError, err.Error()))
		return
	}

	c.Set("data", data)
}

// GetDeviceStatusHistory 获取设备状态历史记录
// @Router   /api/v1/device/status/history [get]
func (*DeviceApi) GetDeviceStatusHistory(c *gin.Context) {
	var req model.GetDeviceStatusHistoryReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Device.GetDeviceStatusHistory(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// PublishBundleDraft 发布 Bundle 草稿到市场
// @Router   /api/v1/device/market/bundles/publish-draft [post]
func (*DeviceApi) PublishBundleDraft(c *gin.Context) {
	var req model.PublishDraftRequest
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	// 调用服务层
	publishService := service.NewMarketBundlePublish()
	response, precheckReport, err := publishService.PublishDraft(c.Request.Context(), req, userClaims)
	if err != nil {
		// 如果有预检报告，返回预检报告而不是直接报错
		if precheckReport != nil && !precheckReport.Passed {
			c.Set("data", gin.H{
				"precheckReport": precheckReport,
			})
			c.Set("code", errcode.CodeParamError)
			return
		}
		c.Error(err)
		return
	}

	c.Set("data", gin.H{
		"bundleKey":      response.BundleKey,
		"version":        response.Version,
		"contentHash":    response.ContentHash,
		"status":         response.Status,
		"precheckReport": precheckReport,
	})
}

// AnalyzeDashboardBundle 分析一个看板引用的真实设备及字段
// @Router /api/v1/device/market/dashboard-bundles/analyze [post]
func (*DeviceApi) AnalyzeDashboardBundle(c *gin.Context) {
	var req model.AnalyzeDashboardBundleRequest
	if !BindAndValidate(c, &req) {
		return
	}
	publicOrigin, err := requestPublicOrigin(c)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, err.Error()))
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	result, err := service.NewMarketDashboardBundleService(publicOrigin+"/thingsvis-api").Analyze(
		c.Request.Context(),
		req.DashboardID,
		c.GetHeader("Authorization"),
		claims,
	)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", result)
}

// PublishDashboardBundle 发布一个看板商品并进入人工审核
// @Router /api/v1/device/market/dashboard-bundles [post]
func (*DeviceApi) PublishDashboardBundle(c *gin.Context) {
	var req model.PublishDashboardBundleRequest
	if !BindAndValidate(c, &req) {
		return
	}
	publicOrigin, err := requestPublicOrigin(c)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, err.Error()))
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	result, err := service.NewMarketDashboardBundleService(publicOrigin+"/thingsvis-api").Publish(
		c.Request.Context(),
		&req,
		c.GetHeader("Authorization"),
		claims,
	)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", result)
}

// InstallBundleFromMarket 从市场安装 Bundle
// @Router   /api/v1/device/market/bundles/install [post]
func (*DeviceApi) InstallBundleFromMarket(c *gin.Context) {
	var req model.InstallBundleRequest
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	installService := service.NewMarketBundleInstallService()
	response, err := installService.InstallBundle(c.Request.Context(), &req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", response)
}

// DownloadDashboardTemplateFromMarket downloads reusable templates and their
// device-template dependencies without creating runtime dashboards.
// @Router /api/v1/device/market/bundles/download [post]
func (*DeviceApi) DownloadDashboardTemplateFromMarket(c *gin.Context) {
	var req model.DownloadDashboardTemplateRequest
	if !BindAndValidate(c, &req) {
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	result, err := service.NewDashboardTemplateService().Download(c.Request.Context(), &req, claims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", result)
}

// ListLocalDashboardTemplates lists tenant-owned reusable dashboard templates.
// @Router /api/v1/device/dashboard-templates [get]
func (*DeviceApi) ListLocalDashboardTemplates(c *gin.Context) {
	var req model.ListLocalDashboardTemplatesRequest
	if !BindAndValidate(c, &req) {
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	result, err := service.NewDashboardTemplateService().List(c.Request.Context(), claims.TenantID, &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", result)
}

// ListDashboardTemplateCompatibleDevices resolves compatible real devices from
// the locally installed device template IDs.
// @Router /api/v1/device/dashboard-templates/:id/compatible-devices [get]
func (*DeviceApi) ListDashboardTemplateCompatibleDevices(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "dashboard template ID is required"))
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	result, err := service.NewDashboardTemplateService().CompatibleDevices(
		c.Request.Context(),
		claims.TenantID,
		templateID,
	)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", result)
}

// CreateDashboardFromLocalTemplate binds compatible real devices and creates a
// concrete ThingsVis dashboard.
// @Router /api/v1/device/dashboard-templates/:id/instances [post]
func (*DeviceApi) CreateDashboardFromLocalTemplate(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "dashboard template ID is required"))
		return
	}
	var req model.CreateDashboardTemplateInstanceRequest
	if !BindAndValidate(c, &req) {
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	result, err := service.NewDashboardTemplateService().CreateInstance(
		c.Request.Context(),
		claims.TenantID,
		claims.ID,
		templateID,
		c.GetHeader("Authorization"),
		&req,
	)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", result)
}

// GetBundleInstallStatus 获取 Bundle 安装状态
// @Router   /api/v1/device/market/bundles/install/:id [get]
// ListMarketBundles lists published dashboard templates from Horizon.
// @Router /api/v1/device/market/bundles [get]
func (*DeviceApi) ListMarketBundles(c *gin.Context) {
	var req model.MarketBundleListQuery
	if !BindAndValidate(c, &req) {
		return
	}
	result, err := service.NewMarketBundleCatalogService().List(c.Request.Context(), req)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, err.Error()))
		return
	}
	c.Set("data", result)
}

// GetMarketBundleDetail returns a safe dashboard-template catalog projection.
// @Router /api/v1/device/market/bundles/:bundleKey [get]
func (*DeviceApi) GetMarketBundleDetail(c *gin.Context) {
	bundleKey := c.Param("bundleKey")
	if bundleKey == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "bundle key is required"))
		return
	}
	result, err := service.NewMarketBundleCatalogService().Detail(c.Request.Context(), bundleKey)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, err.Error()))
		return
	}
	c.Set("data", result)
}

// GetMarketBundlePrecheck returns device roles and compatibility requirements.
// @Router /api/v1/device/market/bundles/:bundleKey/precheck [get]
func (*DeviceApi) GetMarketBundlePrecheck(c *gin.Context) {
	bundleKey := c.Param("bundleKey")
	if bundleKey == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "bundle key is required"))
		return
	}
	result, err := service.NewMarketBundleCatalogService().Precheck(c.Request.Context(), bundleKey, c.Query("version"))
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, err.Error()))
		return
	}
	c.Set("data", result)
}

func (*DeviceApi) GetBundleInstallStatus(c *gin.Context) {
	installID := c.Param("id")
	if installID == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "installation ID is required"))
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	installService := service.NewMarketBundleInstallService()
	response, err := installService.GetInstallationStatus(c.Request.Context(), installID, userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", response)
}

// UpdateBundleBinding 更新 Bundle 设备绑定
// @Router   /api/v1/device/market/bundles/install/:id/bindings [put]
func (*DeviceApi) UpdateBundleBinding(c *gin.Context) {
	installID := c.Param("id")
	if installID == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "installation ID is required"))
		return
	}

	var req model.UpdateBindingRequest
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	installService := service.NewMarketBundleInstallService()
	err := installService.UpdateDeviceBinding(c.Request.Context(), installID, userClaims.TenantID, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", gin.H{"message": "binding updated"})
}

// RetryBundleInstall 重试失败的 Bundle 安装
// @Router   /api/v1/device/market/bundles/install/:id/retry [post]
func (*DeviceApi) RetryBundleInstall(c *gin.Context) {
	installID := c.Param("id")
	if installID == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "installation ID is required"))
		return
	}

	var req model.RetryInstallationRequest
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	installService := service.NewMarketBundleInstallService()
	response, err := installService.RetryInstallation(c.Request.Context(), installID, userClaims.TenantID, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", response)
}

// CompensateBundleInstall 补偿（清理）失败的 Bundle 安装
// @Router   /api/v1/device/market/bundles/install/:id/compensate [post]
func (*DeviceApi) CompensateBundleInstall(c *gin.Context) {
	installID := c.Param("id")
	if installID == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "installation ID is required"))
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	installService := service.NewMarketBundleInstallService()
	err := installService.CompensateInstallation(c.Request.Context(), installID, userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", gin.H{"message": "compensation completed"})
}

// ListBundleInstallations 列出租户的 Bundle 安装记录
// @Router   /api/v1/device/market/bundles/installations [get]
func (*DeviceApi) ListBundleInstallations(c *gin.Context) {
	var req model.ListInstallationsRequest
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	installService := service.NewMarketBundleInstallService()
	response, err := installService.ListInstallations(c.Request.Context(), userClaims.TenantID, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", response)
}
