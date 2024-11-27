package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type Role struct {
}

func (*Role) Init(Router *gin.RouterGroup) {
	url := Router.Group("role")
	{
		// 增
		url.POST("", api.Controllers.RoleApi.CreateRole)

		// 删
		url.DELETE(":id", api.Controllers.RoleApi.DeleteRole)

		// 改
		url.PUT("", api.Controllers.RoleApi.UpdateRole)

		// 查
		url.GET("", api.Controllers.RoleApi.HandleRoleListByPage)
	}
}
