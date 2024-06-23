package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type ServicePlugin struct {
}

func (p *ServicePlugin) Init(Router *gin.RouterGroup) {
	url := Router.Group("service")
	{
		url.POST("/", api.Controllers.ServicePluginApi.Create)

		url.GET("list", api.Controllers.ServicePluginApi.GetList)

		url.GET("/select", api.Controllers.ServicePluginApi.Get)

		url.PUT("/", api.Controllers.ServicePluginApi.Update)

		url.DELETE("/", api.Controllers.ServicePluginApi.Delete)

		access := url.Group("access")
		access.POST("/", api.Controllers.ServiceAccessApi.Create)

		access.GET("/list", api.Controllers.ServiceAccessApi.GetList)

		access.PUT("/", api.Controllers.ServiceAccessApi.Update)

		access.DELETE("/", api.Controllers.ServiceAccessApi.Delete)
		// /voucher/form
		access.GET("/voucher/form", api.Controllers.ServiceAccessApi.GetVoucherForm)
		//GetDeviceList
		access.GET("/device/list", api.Controllers.ServiceAccessApi.GetDeviceList)
	}
}
