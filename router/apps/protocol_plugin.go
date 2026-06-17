package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type ProtocolPlugin struct{}

func (*ProtocolPlugin) InitProtocolPlugin(Router *gin.RouterGroup) {
	protocolPluginApi := Router.Group("protocol_plugin")
	{
		// 根据协议类型和设备类型获取设备配置的配置表单
		protocolPluginApi.GET("config_form", api.Controllers.ProtocolPluginApi.HandleProtocolPluginFormByProtocolType)
	}
}
