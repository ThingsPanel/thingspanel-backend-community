package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type Northbound struct {
}

// InitNorthbound 注册数据转发（北向服务）相关路由。
//
// /api/v1/northbound/* 是纯传输代理，Authorization 原样转发给北向服务，本地不做
// 授权判定；/api/v1/northbound-source/* 是本机源端状态，只读。
func (*Northbound) InitNorthbound(Router *gin.RouterGroup) {
	url := Router.Group("northbound-source")
	{
		url.GET("/status", api.Controllers.NorthboundSourceApi.HandleSourceStatus)
	}
	proxy := Router.Group("northbound")
	{
		proxy.Any("/*path", api.Controllers.NorthboundSourceApi.HandleProxy)
	}
}
