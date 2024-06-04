package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type DataPolicy struct {
}

func (p *DataPolicy) Init(Router *gin.RouterGroup) {
	url := Router.Group("datapolicy")
	{
		// 改
		url.PUT("", api.Controllers.DataPolicyApi.UpdateDataPolicy)

		// 查
		url.GET("", api.Controllers.DataPolicyApi.GetDataPolicyListByPage)
	}
}
