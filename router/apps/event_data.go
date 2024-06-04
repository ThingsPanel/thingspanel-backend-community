package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type EventData struct{}

func (e *EventData) InitEventData(Router *gin.RouterGroup) {
	evnetDataApi := Router.Group("event/datas")
	{
		evnetDataApi.GET("", api.Controllers.EventDataApi.GetEventDatasListByPage)
	}
}
