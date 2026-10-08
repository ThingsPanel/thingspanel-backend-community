package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type Product struct{}

func (*Product) InitProduct(Router *gin.RouterGroup) {
	url := Router.Group("product")
	{
		url.POST("", api.Controllers.ProductApi.CreateProduct)
		url.PUT("", api.Controllers.ProductApi.UpdateProduct)
		url.DELETE(":id", api.Controllers.ProductApi.DeleteProduct)
		url.GET("", api.Controllers.ProductApi.HandleProductListByPage)
		url.GET("factory-batches", api.Controllers.ProductApi.ListFactoryBatches)
		url.POST("factory-batches", api.Controllers.ProductApi.CreateFactoryBatch)
		url.GET("factory-batches/:batchId", api.Controllers.ProductApi.GetFactoryBatch)
		url.POST("factory-batches/:batchId/status", api.Controllers.ProductApi.SetFactoryBatchStatus)
		url.POST("factory-batches/:batchId/stations", api.Controllers.ProductApi.IssueFactoryStationGrant)
		url.POST("factory-batches/:batchId/stations/:grantId/revoke", api.Controllers.ProductApi.RevokeFactoryStationGrant)
		url.POST("factory-batches/:batchId/units/:deviceId/enable", api.Controllers.ProductApi.EnableFactoryUnit)
	}
}
