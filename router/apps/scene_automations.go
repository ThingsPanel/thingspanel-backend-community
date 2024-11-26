package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type SceneAutomations struct{}

func (*SceneAutomations) Init(Router *gin.RouterGroup) {
	url := Router.Group("scene_automations")
	{
		// 新
		url.POST("", api.Controllers.SceneAutomationsApi.CreateSceneAutomations)

		// 删
		url.DELETE(":id", api.Controllers.SceneAutomationsApi.DeleteSceneAutomations)

		// 改
		url.PUT("", api.Controllers.SceneAutomationsApi.UpdateSceneAutomations)

		// 启/停
		url.POST("switch/:id", api.Controllers.SceneAutomationsApi.SwitchSceneAutomations)

		// 查列表
		url.GET("list", api.Controllers.SceneAutomationsApi.GetSceneAutomationsByPage)

		// 查详情
		url.GET("detail/:id", api.Controllers.SceneAutomationsApi.GetSceneAutomations)

		// 查日志
		url.GET("log", api.Controllers.SceneAutomationsApi.GetSceneAutomationsLog)

		// 查列表 根据设备id 查询包含告警的场景联动
		url.GET("alarm", api.Controllers.SceneAutomationsApi.GetSceneAutomationsWithAlarmByPage)

	}
}
