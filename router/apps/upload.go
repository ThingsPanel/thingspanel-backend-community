package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type UpLoad struct{}

func (*UpLoad) Init(Router *gin.RouterGroup) {
	uploadapi := Router.Group("file")
	{
		// 文件上传
		uploadapi.POST("up", api.Controllers.UpLoadApi.UpFile)
	}
}
