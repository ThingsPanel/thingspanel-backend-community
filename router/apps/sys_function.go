package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type SysFunction struct{}

func (s *SysFunction) Init(Router *gin.RouterGroup) {
	url := Router.Group("sys_function")
	{
		// 改
		url.PUT(":id", api.Controllers.SysFunctionApi.UpdateSysFcuntion)

	}
}
