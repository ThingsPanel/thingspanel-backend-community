package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type Device struct{}

func (d *Device) InitDevice(Router *gin.RouterGroup) {

	// 设备路由
	deviceapi := Router.Group("device")
	{
		// 增
		deviceapi.POST("", api.Controllers.DeviceApi.CreateDevice)

		// 删
		deviceapi.DELETE(":id", api.Controllers.DeviceApi.DeleteDevice)

		// 改
		deviceapi.PUT("", api.Controllers.DeviceApi.UpdateDevice)

		// 激活
		deviceapi.PUT("active", api.Controllers.DeviceApi.ActiveDevice)

		// 详情查询
		deviceapi.GET("/detail/:id", api.Controllers.DeviceApi.GetDeviceByID)
		// 查
		deviceapi.GET("", api.Controllers.DeviceApi.GetDeviceListByPage)

		// 编号校验
		deviceapi.GET("check/:deviceNumber", api.Controllers.DeviceApi.CheckDeviceNumber)

		// 租户下设备列表
		deviceapi.GET("tenant/list", api.Controllers.DeviceApi.GetTenantDeviceList)

		// 设备列表
		deviceapi.GET("list", api.Controllers.DeviceApi.GetDeviceList)

		// 添加子设备
		deviceapi.POST("son/add", api.Controllers.DeviceApi.CreateSonDevice)

		// 连接-凭证表单
		deviceapi.GET("connect/form", api.Controllers.DeviceApi.DeviceConnectForm)

		// 连接信息
		deviceapi.GET("connect/info", api.Controllers.DeviceApi.DeviceConnect)

		// 更新 voucher
		deviceapi.POST("update/voucher", api.Controllers.DeviceApi.UpdateDeviceVoucher)

		// 获取子设备列表
		deviceapi.GET("sub-list/:id", api.Controllers.DeviceApi.GetSubList)

		// 移除子设备
		deviceapi.PUT("sub-remove", api.Controllers.DeviceApi.RemoveSubDevice)

		// 选择指标下拉菜单
		deviceapi.GET("metrics/:id", api.Controllers.DeviceApi.GetMetrics)

		// 自动化-单设备动作选择下拉菜单
		deviceapi.GET("metrics/menu", api.Controllers.DeviceApi.GetActionByDeviceID)

		// 自动化-单设备条件选择下拉菜单
		deviceapi.GET("metrics/condition/menu", api.Controllers.DeviceApi.GetConditionByDeviceID)

		// 设备地图-遥测信息
		deviceapi.GET("map/telemetry/:id", api.Controllers.DeviceApi.GetMapTelemetry)

		//更换设备配置
		deviceapi.PUT("update/config", api.Controllers.DeviceApi.UpdateDeviceConfig)

		// 设备在线状态查询
		deviceapi.GET("online/status/:id", api.Controllers.DeviceApi.GetDeviceOnlineStatus)
	}

	// 设备模版路由
	deviceTemplateapi := deviceapi.Group("template")
	{
		// 增
		deviceTemplateapi.POST("", api.Controllers.DeviceApi.CreateDeviceTemplate)

		// 删
		deviceTemplateapi.DELETE(":id", api.Controllers.DeviceApi.DeleteDeviceTemplate)

		// 改
		deviceTemplateapi.PUT("", api.Controllers.DeviceApi.UpdateDeviceTemplate)

		// 详情查询
		deviceTemplateapi.GET("/detail/:id", api.Controllers.DeviceApi.GetDeviceTemplateById)

		// 分页查询
		deviceTemplateapi.GET("", api.Controllers.DeviceApi.GetDeviceTemplateListByPage)

		// 模板下拉菜单
		deviceTemplateapi.GET("/menu", api.Controllers.DeviceApi.GetDeviceTemplateMenu)

		// 根据设备ID获取模板
		deviceTemplateapi.GET("/chart", api.Controllers.DeviceApi.GetDeviceTemplateByDeviceId)

		// 根据分组ID获取设备下拉（带模板信息）
		deviceTemplateapi.GET("/chart/select", api.Controllers.DeviceApi.GetDeviceTemplateChartSelect)
	}

	// 设备分组
	deviceGroupapi := deviceapi.Group("group")
	{
		// 新增
		deviceGroupapi.POST("", api.Controllers.DeviceApi.CreateDeviceGroup)

		// 删除分组
		deviceGroupapi.DELETE(":id", api.Controllers.DeviceApi.DeleteDeviceGroup)

		// 修改分组
		deviceGroupapi.PUT("", api.Controllers.DeviceApi.UpdateDeviceGroup)

		// 分页列表查询
		deviceGroupapi.GET("", api.Controllers.DeviceApi.GetDeviceGroupByPage)

		// 分组树查询
		deviceGroupapi.GET("tree", api.Controllers.DeviceApi.GetDeviceGroupByTree)

		// 详情查询
		deviceGroupapi.GET("detail/:id", api.Controllers.DeviceApi.GetDeviceGroupByDetail)
	}

	// 设备分组管理
	deviceGroupRapi := deviceGroupapi.Group("relation")
	{
		// 创建分组关系
		deviceGroupRapi.POST("", api.Controllers.DeviceApi.CreateDeviceGroupRelation)

		deviceGroupRapi.DELETE("", api.Controllers.DeviceApi.DeleteDeviceGroupRelation)

		deviceGroupRapi.GET("list", api.Controllers.DeviceApi.GetDeviceGroupRelation)

		deviceGroupRapi.GET("", api.Controllers.DeviceApi.GetDeviceGroupListByDeviceId)

	}

	deviceModelApi := deviceapi.Group("model")
	{
		// 模板数据源指标查询（遥测、属性）
		deviceModelApi.GET("source/at/list", api.Controllers.DeviceModelApi.GetModelSourceAT)
		deviceModelTelemetryApi := deviceModelApi.Group("telemetry")
		{
			deviceModelTelemetryApi.POST("", api.Controllers.DeviceModelApi.CreateDeviceModelTelemetry)
			deviceModelTelemetryApi.DELETE(":id", api.Controllers.DeviceModelApi.DeleteDeviceModelGeneral)
			deviceModelTelemetryApi.PUT("", api.Controllers.DeviceModelApi.UpdateDeviceModelGeneral)
			deviceModelTelemetryApi.GET("", api.Controllers.DeviceModelApi.GetDeviceModelGeneral)
		}

		deviceModelAttributesApi := deviceModelApi.Group("attributes")
		{
			deviceModelAttributesApi.POST("", api.Controllers.DeviceModelApi.CreateDeviceModelAttributes)
			deviceModelAttributesApi.DELETE(":id", api.Controllers.DeviceModelApi.DeleteDeviceModelGeneral)
			deviceModelAttributesApi.PUT("", api.Controllers.DeviceModelApi.UpdateDeviceModelGeneral)
			deviceModelAttributesApi.GET("", api.Controllers.DeviceModelApi.GetDeviceModelGeneral)
		}

		deviceModelEventsApi := deviceModelApi.Group("events")
		{
			deviceModelEventsApi.POST("", api.Controllers.DeviceModelApi.CreateDeviceModelEvents)
			deviceModelEventsApi.DELETE(":id", api.Controllers.DeviceModelApi.DeleteDeviceModelGeneral)
			deviceModelEventsApi.PUT("", api.Controllers.DeviceModelApi.UpdateDeviceModelGeneralV2)
			deviceModelEventsApi.GET("", api.Controllers.DeviceModelApi.GetDeviceModelGeneral)
		}

		deviceModelCommandsApi := deviceModelApi.Group("commands")
		{
			deviceModelCommandsApi.POST("", api.Controllers.DeviceModelApi.CreateDeviceModelCommands)
			deviceModelCommandsApi.DELETE(":id", api.Controllers.DeviceModelApi.DeleteDeviceModelGeneral)
			deviceModelCommandsApi.PUT("", api.Controllers.DeviceModelApi.UpdateDeviceModelGeneralV2)
			deviceModelCommandsApi.GET("", api.Controllers.DeviceModelApi.GetDeviceModelGeneral)

		}

		deviceModelCustomCommandsApi := deviceModelApi.Group("custom/commands")
		{
			deviceModelCustomCommandsApi.POST("", api.Controllers.DeviceModelApi.CreateDeviceModelCustomCommands)
			deviceModelCustomCommandsApi.DELETE(":id", api.Controllers.DeviceModelApi.DeleteDeviceModelCustomCommands)
			deviceModelCustomCommandsApi.PUT("", api.Controllers.DeviceModelApi.UpdateDeviceModelCustomCommands)
			deviceModelCustomCommandsApi.GET("", api.Controllers.DeviceModelApi.GetDeviceModelCustomCommandsByPage)
			deviceModelCustomCommandsApi.GET(":deviceId", api.Controllers.DeviceModelApi.GetDeviceModelCustomCommandsByDeviceId)
		}
	}

}
