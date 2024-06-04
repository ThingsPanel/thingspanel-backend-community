package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type DeviceConfig struct {
}

func (p *DeviceConfig) Init(Router *gin.RouterGroup) {
	url := Router.Group("device_config")
	{
		// 增
		url.POST("", api.Controllers.DeviceConfigApi.CreateDeviceConfig)

		// 删
		url.DELETE(":id", api.Controllers.DeviceConfigApi.DeleteDeviceConfig)

		// 改
		url.PUT("", api.Controllers.DeviceConfigApi.UpdateDeviceConfig)

		// 查
		url.GET("", api.Controllers.DeviceConfigApi.GetDeviceConfigListByPage)

		// 查设备配置下拉菜单
		url.GET("menu", api.Controllers.DeviceConfigApi.GetDeviceConfigListMenu)

		// 查
		url.GET("/:id", api.Controllers.DeviceConfigApi.GetDeviceConfigById)

		// 批量修改设备配置
		url.PUT("batch", api.Controllers.DeviceConfigApi.BatchUpdateDeviceConfig)

		// 连接与认证下拉
		url.GET("connect", api.Controllers.DeviceConfigApi.GetDeviceConfigConnect)

		// 设备配置-连接与认证下拉
		url.GET("voucher_type", api.Controllers.DeviceConfigApi.GetVoucherType)

		// 单类设备自动化动作下拉菜单
		url.GET("metrics/menu", api.Controllers.DeviceConfigApi.GetActionByDeviceConfigID)

		// 单类设备自动化条件下拉菜单
		url.GET("metrics/condition/menu", api.Controllers.DeviceConfigApi.GetConditionByDeviceConfigID)

	}
}
