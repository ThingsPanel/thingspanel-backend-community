// router/apps/open_api_keys.go
package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type OpenAPIKey struct{}

func (*OpenAPIKey) InitOpenAPIKey(Router *gin.RouterGroup) {
	openAPIRouter := Router.Group("open/keys")
	{
		// OpenAPI密钥管理
		openAPIRouter.POST("", api.Controllers.OpenAPIKeyApi.CreateOpenAPIKey)      // 创建密钥
		openAPIRouter.GET("", api.Controllers.OpenAPIKeyApi.GetOpenAPIKeyList)      // 获取列表
		openAPIRouter.PUT("", api.Controllers.OpenAPIKeyApi.UpdateOpenAPIKey)       // 更新密钥
		openAPIRouter.DELETE(":id", api.Controllers.OpenAPIKeyApi.DeleteOpenAPIKey) // 删除密钥
	}
}
