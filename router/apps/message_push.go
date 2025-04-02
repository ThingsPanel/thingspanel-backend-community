package apps

import (
	"github.com/gin-gonic/gin"
	"project/internal/api"
)

type MessagePush struct {
}

func (*MessagePush) Init(Router *gin.RouterGroup) {
	url := Router.Group("message_push")
	{
		// 增
		url.POST("", api.Controllers.MessagePushApi.CreateMessagePush)
		//注销
		url.POST("/logout", api.Controllers.MessagePushApi.MessagePushMangeLogout)
		//获取配置
		url.GET("/config", api.Controllers.MessagePushApi.GetMessagePushConfig)
		//设置配置
		url.POST("/config", api.Controllers.MessagePushApi.SetMessagePushConfig)
	}
}
