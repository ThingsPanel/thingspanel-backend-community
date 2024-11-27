package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type Scene struct{}

func (*Scene) Init(Router *gin.RouterGroup) {
	url := Router.Group("scene")
	{
		// 新增
		url.POST("", api.Controllers.SceneApi.CreateScene)

		// 删除
		url.DELETE(":id", api.Controllers.SceneApi.DeleteScene)

		// list
		url.GET("", api.Controllers.SceneApi.HandleSceneByPage)

		// detail
		url.GET("/detail/:id", api.Controllers.SceneApi.GetScene)

		// 更新
		url.PUT("", api.Controllers.SceneApi.UpdateScene)

		// 激活
		url.POST("active/:id", api.Controllers.SceneApi.ActiveScene)

		// 场景日志查询
		url.GET("log", api.Controllers.SceneApi.HandleSceneLog)

	}
}
