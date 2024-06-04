package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type UiElements struct {
}

func (p *UiElements) Init(Router *gin.RouterGroup) {
	url := Router.Group("ui_elements")
	{
		// 增
		url.POST("", api.Controllers.UiElementsApi.CreateUiElements)

		// 删
		url.DELETE(":id", api.Controllers.UiElementsApi.DeleteUiElements)

		// 改
		url.PUT("", api.Controllers.UiElementsApi.UpdateUiElements)

		// 分页查询,按照树状结构返回，父节点包含一个"children"，其中是子节点，按照order排序
		url.GET("", api.Controllers.UiElementsApi.GetUiElementsListByPage)

		// 根据用户权限查询
		url.GET("menu", api.Controllers.UiElementsApi.GetUiElementsListByAuthority)

		// 菜单配置表单
		url.GET("select/form", api.Controllers.UiElementsApi.GetUiElementsListByTenant)
	}
}
