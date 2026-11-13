package api

import (
	"errors"
	"net/http"
	model "project/internal/model"
	service "project/internal/service"
	utils "project/pkg/utils"
	"strings"

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
// @Router   /api/v1/device_config [put]
func (*DeviceConfigApi) UpdateDeviceConfig(c *gin.Context) {
	var req model.UpdateDeviceConfigReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	matchHeaders := c.Request.Header.Values("If-Match")
	match := strings.Join(matchHeaders, ",")
	condition, err := model.ParseDeviceConfigCondition(match)
	if len(matchHeaders) > 0 && strings.TrimSpace(match) == "" {
		err = model.ErrInvalidDeviceConfigCondition
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 100002, "message": "Invalid If-Match condition"})
		return
	}
	data, err := service.GroupApp.DeviceConfig.UpdateDeviceConfig(c, req, userClaims, c.GetHeader("x-token"), condition)
	if errors.Is(err, model.ErrDeviceConfigVersionConflict) {
		c.AbortWithStatusJSON(http.StatusPreconditionFailed, gin.H{"code": 201002, "message": "Template changed; reload before saving", "data": gin.H{"reason": "template_revision_conflict"}})
		return
	}
	if err != nil {
		c.Error(err)
		return
	}

	if !setDeviceConfigVersion(c, data) {
		return
	}
	c.Set("data", data)
}

// DeleteDeviceConfig 删除设备配置
// @Router   /api/v1/device_config/{id} [delete]
func (*DeviceConfigApi) DeleteDeviceConfig(c *gin.Context) {
	id := c.Param("id")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.DeviceConfig.DeleteDeviceConfig(id, userClaims)
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
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	info, err := service.GroupApp.DeviceConfig.GetDeviceConfigByID(c, id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	if !setDeviceConfigVersion(c, info) {
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

// BatchUpdateDeviceConfig 批量绑定设备到网关（支持多级网关）
// @Router   /api/v1/device_config/batch [put]
func (*DeviceConfigApi) BatchUpdateDeviceConfig(c *gin.Context) {
	var req model.BatchUpdateDeviceConfigReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.DeviceConfig.BatchUpdateDeviceConfig(&req, c.MustGet("claims").(*utils.UserClaims))
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
	lang := c.GetHeader("Accept-Language")
	data, err := service.GroupApp.DeviceConfig.GetDeviceConfigConnect(c, param.DeviceID, lang)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/device_config/voucher_type
func (*DeviceConfigApi) HandleVoucherType(c *gin.Context) {
	var param model.GetVoucherTypeReq
	if !BindAndValidate(c, &param) {
		return
	}
	lang := c.GetHeader("Accept-Language")
	data, err := service.GroupApp.DeviceConfig.GetVoucherTypeForm(param.DeviceType, param.ProtocolType, lang)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// 根据设备配置id获取自动化动作中下拉列表
// /api/v1/device_config/metrics/menu [get]
func (*DeviceConfigApi) HandleActionByDeviceConfigID(c *gin.Context) {
	var param model.GetActionByDeviceConfigIDReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.DeviceConfig.GetActionByDeviceConfigID(c.Request.Context(), param.DeviceConfigID, c.MustGet("claims").(*utils.UserClaims), communitySessionAuthorization(c))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// 根据设备配置id获取自动化动作中下拉列表
// /api/v1/device_config/metrics/condition/menu
func (*DeviceConfigApi) HandleConditionByDeviceConfigID(c *gin.Context) {
	var param model.GetActionByDeviceConfigIDReq
	if !BindAndValidate(c, &param) {
		return
	}
	list, err := service.GroupApp.DeviceConfig.GetConditionByDeviceConfigID(c.Request.Context(), param.DeviceConfigID, c.MustGet("claims").(*utils.UserClaims), communitySessionAuthorization(c))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// Use the very same row as the response, never a second unlocked read.
func setDeviceConfigVersion(c *gin.Context, value any) bool {
	config, ok := value.(*model.DeviceConfig)
	if !ok {
		c.AbortWithStatusJSON(500, gin.H{"code": 100000, "message": "Template version unavailable"})
		return false
	}
	tag, err := model.DeviceConfigETag(config)
	if err != nil {
		c.Error(err)
		return false
	}
	c.Header("ETag", tag)
	c.Header("Cache-Control", "no-store")
	return true
}
