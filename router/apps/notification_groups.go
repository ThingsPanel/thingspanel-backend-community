package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type NotificationGroup struct {
}

func (*NotificationGroup) InitNotificationGroup(Router *gin.RouterGroup) {
	url := Router.Group("notification_group")
	// Tenant alarm-default policy is deliberately a separate pointer from
	// per-alarm notification_group_id fields.
	Router.GET("notification-default-policy", api.Controllers.NotificationGroupApi.GetTenantDefaultPolicy)
	Router.PUT("notification-default-policy", api.Controllers.NotificationGroupApi.PutTenantDefaultPolicy)
	{
		// Explicit tenant-scoped bridge between native notification groups and
		// the existing community alarm selector. Static routes must precede :id.
		url.GET("/native-publish", api.Controllers.NotificationGroupApi.GetNativePublishStatus)
		url.POST("/native-publish", api.Controllers.NotificationGroupApi.ApplyNativePublish)

		// 增
		url.POST("", api.Controllers.NotificationGroupApi.CreateNotificationGroup)

		// 删
		url.DELETE("/:id", api.Controllers.NotificationGroupApi.DeleteNotificationGroup)

		// 改
		url.PUT("/:id", api.Controllers.NotificationGroupApi.UpdateNotificationGroup)

		// 查
		url.GET("/list", api.Controllers.NotificationGroupApi.HandleNotificationGroupListByPage)

		// 单条详情
		url.GET("/:id", api.Controllers.NotificationGroupApi.HandleNotificationGroupById)

	}
}
