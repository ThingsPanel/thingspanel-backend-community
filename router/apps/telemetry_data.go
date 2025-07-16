package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type TelemetryData struct{}

func (*TelemetryData) InitTelemetryData(Router *gin.RouterGroup) {
	telemetrydataapi := Router.Group("telemetry/datas")
	{

		// 当前所有key最新数据
		telemetrydataapi.GET("current/:id", api.Controllers.TelemetryDataApi.HandleCurrentData)

		// 根据key获取当前数据，包含标签
		telemetrydataapi.GET("/current/keys", api.Controllers.TelemetryDataApi.HandleCurrentDataKeys)

		// 对应/kv/current/detail
		telemetrydataapi.GET("current/detail/:id", api.Controllers.TelemetryDataApi.ServeCurrentDetailData)

		// 历史记录，不分页
		telemetrydataapi.GET("history", api.Controllers.TelemetryDataApi.ServeHistoryData)

		// 历史记录，不分页
		telemetrydataapi.GET("history/pagination", api.Controllers.TelemetryDataApi.ServeHistoryDataByPage)
		// 历史记录，分页
		telemetrydataapi.GET("history/page", api.Controllers.TelemetryDataApi.ServeHistoryDataByPage)

		// 删除
		telemetrydataapi.DELETE("", api.Controllers.TelemetryDataApi.DeleteData)

		// 统计数据
		telemetrydataapi.GET("statistic", api.Controllers.TelemetryDataApi.ServeStatisticData)

		// 批量查询多个设备的遥测统计数据
		telemetrydataapi.GET("statistic/batch", api.Controllers.TelemetryDataApi.ServeStatisticDataByDeviceId)

		// 遥测数据下发记录
		telemetrydataapi.GET("set/logs", api.Controllers.TelemetryDataApi.ServeSetLogsDataListByPage)

		// 下发遥测
		telemetrydataapi.POST("pub", api.Controllers.TelemetryDataApi.TelemetryPutMessage)

		//获取模拟设备发送遥测数据的回显数据ServeEchoData
		telemetrydataapi.GET("simulation", api.Controllers.TelemetryDataApi.ServeEchoData)

		//模拟设备发送遥测数据SimulationTelemetryData
		telemetrydataapi.POST("simulation", api.Controllers.TelemetryDataApi.SimulationTelemetryData)

		// 返回用户消息大致数量
		telemetrydataapi.GET("msg/count", api.Controllers.TelemetryDataApi.ServeMsgCountByTenant)

	}
}
