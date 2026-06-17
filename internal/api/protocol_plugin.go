package api

import (
	"project/initialize"
	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/errcode"

	"github.com/gin-gonic/gin"
)

type ProtocolPluginApi struct{}

// GetProtocolPluginForm 根据协议类型获取设备配置表单
// @Router   /api/v1/protocol_plugin/config_form [get]
func (*ProtocolPluginApi) HandleProtocolPluginFormByProtocolType(c *gin.Context) {
	var req model.GetProtocolPluginFormByProtocolType
	if !BindAndValidate(c, &req) {
		return
	}

	data, err := service.GroupApp.ServicePlugin.GetProtocolPluginFormByProtocolType(req.ProtocolType, req.DeviceType)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/plugin/device/config
// 协议插件获取设备配置
func (*ProtocolPluginApi) HandleDeviceConfigForProtocolPlugin(c *gin.Context) {
	var req model.GetDeviceConfigReq
	if !BindAndValidate(c, &req) {
		return
	}

	// 限流检查：只对voucher和device_number进行限流
	var limitKey string
	if req.Voucher != "" {
		limitKey = "device_auth_voucher:" + req.Voucher
	} else if req.DeviceNumber != "" {
		limitKey = "device_auth_device_number:" + req.DeviceNumber
	}

	if limitKey != "" {
		limiter := initialize.NewDeviceAuthLimiter()
		if !limiter.Allow(limitKey) {
			c.Error(errcode.WithData(errcode.CodeRateLimit, map[string]interface{}{
				"error": "Request rate limit exceeded, please try again later",
			}))
			return
		}
	}

	data, err := service.GroupApp.ProtocolPlugin.GetDeviceConfig(req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// 通过协议标识符获取设备配置（包含设备信息）
// /api/v1/plugin/devices
func (*ProtocolPluginApi) HandleDeviceConfigForProtocolPluginByProtocolType(c *gin.Context) {
	var req model.GetDevicesByProtocolPluginReq
	if !BindAndValidate(c, &req) {
		return
	}

	data, err := service.GroupApp.ProtocolPlugin.GetDevicesByProtocolPlugin(req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}
