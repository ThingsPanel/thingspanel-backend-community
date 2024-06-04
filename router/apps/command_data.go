package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type CommandData struct{}

func (t *CommandData) InitCommandData(Router *gin.RouterGroup) {
	commandDataApi := Router.Group("command/datas")
	{
		// 获取命令下发记录（分页）
		commandDataApi.GET("set/logs", api.Controllers.CommandSetLogApi.GetSetLogsDataListByPage)

		// 下发命令
		commandDataApi.POST("pub", api.Controllers.CommandSetLogApi.CommandPutMessage)

		// 命令标识符下拉菜单
		commandDataApi.GET(":id", api.Controllers.CommandSetLogApi.GetCommandList)
	}
}
