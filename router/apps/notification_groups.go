package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type NotificationGroup struct {
}

func (p *NotificationGroup) InitNotificationGroup(Router *gin.RouterGroup) {
	url := Router.Group("notification_group")
	{
		// 增
		url.POST("", api.Controllers.NotificationGroupApi.CreateNotificationGroup)

		// 删
		url.DELETE("/:id", api.Controllers.NotificationGroupApi.DeleteNotificationGroup)

		// 改
		url.PUT("/:id", api.Controllers.NotificationGroupApi.UpdateNotificationGroup)

		// 查
		url.GET("/list", api.Controllers.NotificationGroupApi.GetNotificationGroupListByPage)

		// 单条详情
		url.GET("/:id", api.Controllers.NotificationGroupApi.GetNotificationGroupById)

	}
}
