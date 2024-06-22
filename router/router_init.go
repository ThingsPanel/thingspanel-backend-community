package router

import (
	middleware "project/middleware"
	"project/router/apps"

	"github.com/gin-gonic/gin"
	ginSwagger "github.com/swaggo/gin-swagger"

	// gin-swagger middleware
	swaggerFiles "github.com/swaggo/files"

	api "project/api"
)

// swagger embed files

func RouterInit() *gin.Engine {
	//gin.SetMode(gin.ReleaseMode) //开启生产模式
	router := gin.Default()
	// 静态文件
	router.Static("/files", "./files")

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.Use(middleware.Cors())
	controllers := new(api.Controller)

	api := router.Group("api")
	{
		// 无需权限校验
		v1 := api.Group("v1")
		{
			v1.POST("plugin/device/config", controllers.GetDeviceConfigForProtocolPlugin)
			v1.POST("login", controllers.Login)
			v1.GET("verification/code", controllers.GetVerificationCode)
			v1.POST("reset/password", controllers.ResetPassword)
			v1.GET("logo", controllers.GetLogoList)
			// 设备遥测（ws）
			v1.GET("telemetry/datas/current/ws", controllers.TelemetryDataApi.GetCurrentDataByWS)
			// 设备在线离线状态（ws）
			v1.GET("device/online/status/ws", controllers.TelemetryDataApi.GetDeviceStatusByWS)
			// 设备遥测keys（ws）
			v1.GET("telemetry/datas/current/keys/ws", controllers.TelemetryDataApi.GetCurrentDataByKey)
			v1.GET("ota/download/files/upgradePackage/:path/:file", controllers.OTAApi.DownloadOTAUpgradePackage)

			// 获取系统时间
			v1.GET("systime", controllers.SystemApi.GetSystime)
		}

		// 需要权限校验
		v1.Use(middleware.JWTAuth())

		// 需要权限校验
		v1.Use(middleware.CasbinRBAC())

		// 记录操作日志
		v1.Use(middleware.OperationLogs())
		{
			apps.Model.User.InitUser(v1) // 用户模块

			apps.Model.Role.Init(v1) // 角色管理

			apps.Model.Casbin.Init(v1) // 权限管理

			apps.Model.Dict.InitDict(v1) // 字典模块

			apps.Model.Product.InitProduct(v1) // 产品模块

			apps.Model.OTA.InitOTA(v1) // OTA模块

			apps.Model.UpLoad.Init(v1) // 文件上传

			apps.Model.ProtocolPlugin.InitProtocolPlugin(v1) // 协议插件模块

			apps.Model.Device.InitDevice(v1) // 设备

			apps.Model.UiElements.Init(v1) // UI元素控制

			apps.Model.Board.InitBoard(v1) // 首页

			apps.Model.EventData.InitEventData(v1) // 事件数据

			apps.Model.TelemetryData.InitTelemetryData(v1) // 遥测数据

			apps.Model.AttributeData.InitAttributeData(v1) // 属性数据

			apps.Model.CommandData.InitCommandData(v1) //命令数据

			apps.Model.OperationLog.Init(v1) // 操作日志

			apps.Model.Logo.Init(v1) // logo

			apps.Model.DataPolicy.Init(v1) // 数据清理

			apps.Model.DeviceConfig.Init(v1) // 设备配置

			apps.Model.DataScript.Init(v1) // 数据处理脚本

			apps.Model.NotificationGroup.InitNotificationGroup(v1) // 通知组

			apps.Model.NotificationHistoryGroup.InitNotificationHistory(v1) // 通知组

			apps.Model.NotificationServicesConfig.Init(v1) // 通知服务配置

			apps.Model.Alarm.Init(v1) // 告警模块

			apps.Model.Scene.Init(v1) // 场景

			apps.Model.SceneAutomations.Init(v1) // 场景联动

			apps.Model.SysFunction.Init(v1) //功能设置

			apps.Model.VisPlugin.Init(v1) // 可视化插件

			apps.Model.ServicePlugin.Init(v1) // 插件管理
		}
	}

	return router
}
