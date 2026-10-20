package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type CommandData struct{}

func (*CommandData) InitCommandData(Router *gin.RouterGroup) {
	commandDataApi := Router.Group("command/datas")
	{
		// 获取命令下发记录（分页）
		commandDataApi.GET("set/logs", api.Controllers.CommandSetLogApi.ServeSetLogsDataListByPage)

		// 下发命令
		commandDataApi.POST("pub", api.Controllers.CommandSetLogApi.CommandPutMessage)

		// 按 message_id 查询单条命令状态
		commandDataApi.GET("status/:message_id", api.Controllers.CommandSetLogApi.GetCommandStatus)

		// 命令标识符下拉菜单
		commandDataApi.GET(":id", api.Controllers.CommandSetLogApi.HandleCommandList)
	}
}
