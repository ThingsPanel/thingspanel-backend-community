package api

import (
	"project/internal/model"
	"project/internal/service"

	"github.com/gin-gonic/gin"
)

// DeviceAuthApi 设备动态认证API处理器
type DeviceAuthApi struct{}

// DeviceAuth 设备动态认证
// @Summary      设备动态认证
// @Description  实现一型一密认证机制，设备通过此接口获取凭证
// @Tags         设备认证
// @Accept       json
// @Produce      json
// @Param        request body model.DeviceAuthReq true "认证请求参数"
// @Success      200 {object} model.DeviceAuthRes "成功"
// @Failure      400 {object} errcode.Error "错误响应"
// @Router       /api/v1/device/auth [post]
// @example request - "请求示例" {"template_secret":"tpl_secret123", "device_number":"dev001", "device_name":"测试设备", "product_key":"prod123"}
func (*DeviceAuthApi) DeviceAuth(c *gin.Context) {
	var req model.DeviceAuthReq
	if !BindAndValidate(c, &req) {
		return
	}

	// 调用服务层进行设备认证
	resp, err := service.GroupApp.DeviceAuth.Auth(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", resp)
}
