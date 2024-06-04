package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type DataScript struct {
}

func (p *DataScript) Init(Router *gin.RouterGroup) {
	url := Router.Group("data_script")
	{
		// 增
		url.POST("", api.Controllers.DataScriptApi.CreateDataScript)

		// 删
		url.DELETE(":id", api.Controllers.DataScriptApi.DeleteDataScript)

		// 改
		url.PUT("", api.Controllers.DataScriptApi.UpdateDataScript)

		// 查
		url.GET("", api.Controllers.DataScriptApi.GetDataScriptListByPage)

		// 调试
		url.POST("quiz", api.Controllers.DataScriptApi.QuizDataScript)

		// 启用禁用
		url.PUT("enable", api.Controllers.DataScriptApi.EnableDataScript)
	}
}
