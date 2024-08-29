package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type ExpectedData struct{}

func (e *ExpectedData) InitExpectedData(Router *gin.RouterGroup) {
	expectedDataApi := Router.Group("expected/data")
	{
		expectedDataApi.GET("list", api.Controllers.ExpectedDataApi.GetExpectedDataList)
		expectedDataApi.POST("", api.Controllers.ExpectedDataApi.CreateExpectedData)
		expectedDataApi.DELETE(":id", api.Controllers.ExpectedDataApi.DeleteExpectedData)
	}
}
