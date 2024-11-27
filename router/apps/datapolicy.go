package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type DataPolicy struct {
}

func (*DataPolicy) Init(Router *gin.RouterGroup) {
	url := Router.Group("datapolicy")
	{
		// 改
		url.PUT("", api.Controllers.DataPolicyApi.UpdateDataPolicy)

		// 查
		url.GET("", api.Controllers.DataPolicyApi.HandleDataPolicyListByPage)
	}
}
