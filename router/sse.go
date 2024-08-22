package router

import (
	sseapi "project/api/sse"

	"github.com/gin-gonic/gin"
)

func SSERouter(Router *gin.RouterGroup) {
	var sseApi sseapi.SSEApi
	sa := Router.Group("events")
	{
		sa.GET("", sseApi.GetSystemEvents)

	}
}
