package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type Board struct {
}

func (*Board) InitBoard(Router *gin.RouterGroup) {
	url := Router.Group("board")
	{
		// 增
		url.POST("", api.Controllers.BoardApi.CreateBoard)

		// 删
		url.DELETE(":id", api.Controllers.BoardApi.DeleteBoard)

		// 改
		url.PUT("", api.Controllers.BoardApi.UpdateBoard)

		// 查
		url.GET("", api.Controllers.BoardApi.GetBoardListByPage)

		// 单条详情
		url.GET(":id", api.Controllers.BoardApi.GetBoard)

		// 首页看板
		url.GET("home", api.Controllers.BoardApi.GetBoardListByTenantId)

	}
	// 设备数据
	devices(url)
	// 租客数据
	tenant(url)
	// 用户数据
	user(url)
}

func devices(Router *gin.RouterGroup) {
	url := Router.Group("device")
	// 设备总数
	url.GET("total", api.Controllers.BoardApi.GetDeviceTotal)
	// 设备总数/激活数
	url.GET("", api.Controllers.BoardApi.GetDevice)
}

func tenant(Router *gin.RouterGroup) {
	url := Router.Group("tenant")
	// 租户总数
	url.GET("", api.Controllers.BoardApi.GetTenant)
	// 租户下用户数据
	url.GET("user/info", api.Controllers.BoardApi.GetTenantUserInfo)
	// 租户下设备数据
	url.GET("device/info", api.Controllers.BoardApi.GetTenantDeviceInfo)
}

func user(Router *gin.RouterGroup) {
	url := Router.Group("user")
	// 个人信息
	url.GET("info", api.Controllers.BoardApi.GetUserInfo)
	// 个人信息修改
	url.POST("update", api.Controllers.BoardApi.UpdateUserInfo)
	// 个人密码修改
	url.POST("update/password", api.Controllers.BoardApi.UpdateUserInfoPassword)
}
