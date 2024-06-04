package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type Casbin struct{}

func (p *Casbin) Init(Router *gin.RouterGroup) {
	url := Router.Group("casbin")
	{
		//角色-功能
		url.POST("function", api.Controllers.CasbinApi.AddFunctionToRole)
		url.DELETE("function/:id", api.Controllers.CasbinApi.DeleteFunctionFromRole)
		url.PUT("function", api.Controllers.CasbinApi.UpdateFunctionFromRole)
		url.GET("function", api.Controllers.CasbinApi.GetFunctionFromRole)

		//角色-用户
		url.POST("user", api.Controllers.CasbinApi.AddRoleToUser)
		url.DELETE("user/:id", api.Controllers.CasbinApi.DeleteRolesFromUser)
		url.PUT("user", api.Controllers.CasbinApi.UpdateRolesFromUser)
		url.GET("user", api.Controllers.CasbinApi.GetRolesFromUser)
	}
}
