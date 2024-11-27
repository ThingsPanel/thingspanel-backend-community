package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type OperationLog struct{}

func (*OperationLog) Init(Router *gin.RouterGroup) {
	url := Router.Group("operation_logs")
	{
		// 分页查询
		url.GET("", api.Controllers.OperationLogsApi.HandleListByPage)
	}
}
