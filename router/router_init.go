package router

import (
	"time"

	middleware "project/internal/middleware"
	"project/internal/middleware/response"
	"project/pkg/global"
	"project/pkg/metrics"
	"project/router/apps"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	// gin-swagger middleware
	_ "project/docs"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	api "project/internal/api"
	service "project/internal/service"
)

// swagger embed files

func RouterInit() *gin.Engine {
	// gin.SetMode(gin.ReleaseMode) //开启生产模式
	router := gin.Default()
	// Swagger文档路由
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 创建 metrics 收集器
	m := metrics.NewMetrics("ThingsPanel")
	// 创建内存存储实现
	memStorage := metrics.NewMemoryStorage()
	// 设置存储实现
	m.SetHistoryStorage(memStorage)
	// 开始定期收集系统指标(每15秒)
	m.StartMetricsCollection(15 * time.Second)
	// 注册 metrics 中间件
	router.Use(middleware.MetricsMiddleware(m))
	// 注册 prometheus metrics 接口
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// 设置metrics管理器到系统监控服务
	service.SetMetricsManager(m)

	// 添加静态文件路由
	router.StaticFile("/metrics-viewer", "./static/metrics-viewer.html")

	// 处理文件访问请求
	router.GET("/files/*filepath", func(c *gin.Context) {
		filepath := c.Param("filepath")
		c.File("./files" + filepath)
	})

	// router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	router.Use(middleware.Cors())
	// 初始化响应处理器
	handler, err := response.NewHandler("configs/messages.yaml", "configs/messages_str.yaml")
	if err != nil {
		logrus.Fatalf("初始化响应处理器失败: %v", err)
	}

	// 记录操作日志
	router.Use(middleware.OperationLogs())
	// 全局使用
	global.ResponseHandler = handler
	// 使用中间件
	router.Use(handler.Middleware())

	controllers := new(api.Controller)
	// 健康检查
	router.GET("/health", controllers.SystemApi.HealthCheck)

	api := router.Group("api")
	{
		// 无需权限校验
		v1 := api.Group("v1")
		{
			// v1.GET("notice/test", controllers.NoticeTest)
			v1.POST("plugin/heartbeat", controllers.Heartbeat)
			v1.POST("plugin/device/config", controllers.HandleDeviceConfigForProtocolPlugin)
			v1.POST("plugin/devices", controllers.HandleDeviceConfigForProtocolPluginByProtocolType)
			v1.POST("plugin/service/access/list", controllers.HandlePluginServiceAccessList)
			v1.POST("plugin/service/access", controllers.HandlePluginServiceAccess)
			v1.POST("login", controllers.Login)
			v1.GET("verification/code", controllers.HandleVerificationCode)
			v1.POST("reset/password", controllers.ResetPassword)
			v1.GET("logo", controllers.HandleLogoList)
			// 设备遥测（ws）
			v1.GET("telemetry/datas/current/ws", controllers.TelemetryDataApi.ServeCurrentDataByWS)
			// 设备在线离线状态（ws） - 兼容旧实现
			v1.GET("device/online/status/ws", controllers.TelemetryDataApi.ServeDeviceStatusByWS)
			// 设备在线离线状态（ws） - 新批量订阅实现（首次消息鉴权，支持 device_ids）
			v1.GET("device/online/status/ws/batch", controllers.TelemetryDataApi.ServeDeviceOnlineStatusWS)
			// 设备遥测keys（ws）
			v1.GET("telemetry/datas/current/keys/ws", controllers.TelemetryDataApi.ServeCurrentDataByKey)
			v1.GET("ota/download/files/upgradePackage/:path/:file", controllers.OTAApi.DownloadOTAUpgradePackage)
			// 获取系统时间
			v1.GET("systime", controllers.SystemApi.HandleSystime)
			// 查询系统功能设置
			v1.GET("sys_function", controllers.SysFunctionApi.HandleSysFcuntion)
			// 租户邮箱注册
			v1.POST("/tenant/email/register", controllers.UserApi.EmailRegister)
			// 网关自动注册
			v1.POST("/device/gateway-register", controllers.DeviceApi.GatewayRegister)
			// 网关子设备注册
			v1.POST("/device/gateway-sub-register", controllers.DeviceApi.GatewaySubRegister)
			// 获取系统版本
			v1.GET("sys_version", controllers.SystemApi.HandleSysVersion)
			// 设备动态认证（一型一密）
			v1.POST("/device/auth", controllers.DeviceAuthApi.DeviceAuth)
			// 设备诊断（不校验权限）
			v1.GET("/devices/:device_id/diagnostics", controllers.DeviceApi.GetDeviceDiagnostics)
		}

		// 需要权限校验
		v1.Use(middleware.JWTAuth())

		// 需要权限校验
		v1.Use(middleware.CasbinRBAC())
		// SSE服务
		SSERouter(v1)

		{
			apps.Model.User.InitUser(v1) // 用户模块

			apps.Model.Role.Init(v1) // 角色管理

			apps.Model.Casbin.Init(v1) // 权限管理

			apps.Model.Dict.InitDict(v1) // 字典模块

			apps.Model.OTA.InitOTA(v1) // OTA模块

			apps.Model.UpLoad.Init(v1) // 文件上传

			apps.Model.ProtocolPlugin.InitProtocolPlugin(v1) // 协议插件模块

			apps.Model.Device.InitDevice(v1) // 设备

			apps.Model.UiElements.Init(v1) // UI元素控制

			apps.Model.Board.InitBoard(v1) // 首页

			apps.Model.EventData.InitEventData(v1) // 事件数据

			apps.Model.TelemetryData.InitTelemetryData(v1) // 遥测数据

			apps.Model.AttributeData.InitAttributeData(v1) // 属性数据

			apps.Model.CommandData.InitCommandData(v1) // 命令数据

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

			apps.Model.SysFunction.Init(v1) // 功能设置

			apps.Model.ServicePlugin.Init(v1) // 插件管理

			apps.Model.ExpectedData.InitExpectedData(v1)

			apps.Model.OpenAPIKey.InitOpenAPIKey(v1)

			apps.Model.MessagePush.Init(v1)

			// 初始化系统监控路由
			apps.Model.SystemMonitor.InitSystemMonitor(v1, m)
		}
	}

	return router
}
