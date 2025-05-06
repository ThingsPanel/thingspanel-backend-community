package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

// DeviceAuth 设备动态认证路由
type DeviceAuth struct{}

// Init 初始化设备动态认证相关路由
func (*DeviceAuth) Init(Router *gin.RouterGroup) {
	url := Router.Group("device")
	{
		// 设备动态认证接口
		url.POST("auth", api.Controllers.DeviceAuthApi.DeviceAuth)
	}
}
