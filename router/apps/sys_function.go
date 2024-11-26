package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type SysFunction struct{}

func (*SysFunction) Init(Router *gin.RouterGroup) {
	url := Router.Group("sys_function")
	{
		// 改
		url.PUT(":id", api.Controllers.SysFunctionApi.UpdateSysFcuntion)

	}
}
