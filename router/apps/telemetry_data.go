package apps

import (
	"project/internal/api"
	"project/internal/automationguard"
	"project/pkg/global"

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
		// 自动化下发的请求带 X-Idempotency-Key，由守卫做代际与幂等校验；
		// 原有调用方不带该头，行为完全不变。
		telemetrydataapi.POST("pub", automationguard.Guard(global.DB), api.Controllers.TelemetryDataApi.TelemetryPutMessage)

		//获取模拟设备发送遥测数据的回显数据ServeEchoData
		telemetrydataapi.GET("simulation", api.Controllers.TelemetryDataApi.ServeEchoData)

		//模拟设备发送遥测数据SimulationTelemetryData
		telemetrydataapi.POST("simulation", api.Controllers.TelemetryDataApi.SimulationTelemetryData)

		// 获取模拟表单初始值（重构版本）
		telemetrydataapi.GET("simulation/init", api.Controllers.TelemetryDataApi.GetSimulationInit)

		// 发送模拟数据（重构版本）
		telemetrydataapi.POST("simulation/send", api.Controllers.TelemetryDataApi.SimulationSend)

		// 返回用户消息大致数量
		telemetrydataapi.GET("msg/count", api.Controllers.TelemetryDataApi.ServeMsgCountByTenant)

	}
}
