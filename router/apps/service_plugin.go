package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type ServicePlugin struct {
}

func (*ServicePlugin) Init(Router *gin.RouterGroup) {
	url := Router.Group("service")
	{
		url.POST("", api.Controllers.ServicePluginApi.Create)

		url.GET("list", api.Controllers.ServicePluginApi.GetList)

		url.GET("/detail/:id", api.Controllers.ServicePluginApi.Get)

		url.PUT("", api.Controllers.ServicePluginApi.Update)

		url.DELETE(":id", api.Controllers.ServicePluginApi.Delete)
		// 获取服务选择器
		url.GET("/plugin/select", api.Controllers.ServicePluginApi.GetServiceSelect)
		// 通过服务标识符获取服务插件信息
		url.GET("/plugin/info", api.Controllers.ServicePluginApi.GetServicePluginByServiceIdentifier)

		access := url.Group("access")
		access.POST("", api.Controllers.ServiceAccessApi.Create)

		access.GET("/list", api.Controllers.ServiceAccessApi.GetList)

		access.PUT("", api.Controllers.ServiceAccessApi.Update)

		access.DELETE(":id", api.Controllers.ServiceAccessApi.Delete)
		// /voucher/form
		access.GET("/voucher/form", api.Controllers.ServiceAccessApi.GetVoucherForm)
		//GetDeviceList
		access.GET("/device/list", api.Controllers.ServiceAccessApi.GetDeviceList)
	}
}
