package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type Alarm struct{}

func (p *Alarm) Init(Router *gin.RouterGroup) {
	url := Router.Group("alarm")
	alarmconfig(url)
	alarminfo(url)
}

func alarmconfig(Router *gin.RouterGroup) {
	url := Router.Group("config")
	{
		// 增
		url.POST("", api.Controllers.AlarmApi.CreateAlarmConfig)

		// 删
		url.DELETE(":id", api.Controllers.AlarmApi.DeleteAlarmConfig)

		// 改
		url.PUT("", api.Controllers.AlarmApi.UpdateAlarmConfig)

		// 查
		url.GET("", api.Controllers.AlarmApi.GetAlarmConfigListByPage)
	}
}

func alarminfo(Router *gin.RouterGroup) {
	url := Router.Group("info")
	{
		// 改
		url.PUT("", api.Controllers.AlarmApi.UpdateAlarmInfo)

		// 批量改
		url.PUT("batch", api.Controllers.AlarmApi.BatchUpdateAlarmInfo)

		// 查
		url.GET("", api.Controllers.AlarmApi.GetAlarmInfoListByPage)

		url.GET("history", api.Controllers.AlarmApi.GetAlarmHisttoryListByPage)

		url.PUT("history", api.Controllers.AlarmApi.AlarmHistoryDescUpdate)

		url.GET("history/device", api.Controllers.AlarmApi.GetDeviceAlarmStatus)

		url.GET("config/device", api.Controllers.AlarmApi.GetConfigByDevice)
	}
}
