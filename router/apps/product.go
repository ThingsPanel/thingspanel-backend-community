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
	}
}
