package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type Logo struct {
}

func (*Logo) Init(Router *gin.RouterGroup) {
	url := Router.Group("logo")
	{
		// 改
		url.PUT("", api.Controllers.LogoApi.UpdateLogo)

		// 查 已移动不用验证token
		// url.GET("", api.Controllers.LogoApi.GetLogoList)
	}
}
