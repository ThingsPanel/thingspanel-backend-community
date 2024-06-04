package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type NotificationServicesConfig struct{}

func (n *NotificationServicesConfig) Init(Router *gin.RouterGroup) {
	url := Router.Group("notification/services/config")
	{
		// 创建/修改
		url.POST("", api.Controllers.NotificationServicesConfigApi.SaveNotificationServicesConfig)

		// 查询
		url.GET(":type", api.Controllers.NotificationServicesConfigApi.GetNotificationServicesConfig)

		// 调试
		url.POST("e-mail/test", api.Controllers.NotificationServicesConfigApi.SendTestEmail)
	}
}
