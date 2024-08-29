package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type NotificationHistoryGroup struct {
}

func (p *NotificationHistoryGroup) InitNotificationHistory(Router *gin.RouterGroup) {
	url := Router.Group("notification_history")
	{

		// 查
		url.GET("/list", api.Controllers.NotificationHistoryApi.GetNotificationHistoryListByPage)

	}
}
