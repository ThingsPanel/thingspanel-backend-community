package routers

import (
	"github.com/ThingsPanel/ThingsPanel-Go/controllers"
	"github.com/ThingsPanel/ThingsPanel-Go/middleware"
	"github.com/beego/beego/v2/server/web"
)

func init() {
	//授权登录中间件
	middleware.AuthMiddle()
	//admin模块路由
	api := web.NewNamespace("/api",
		// 小程序登录
		web.NSRouter("/auth/login", &controllers.AuthController{}, "*:Login"),
		// 退出登录
		web.NSRouter("/auth/logout", &controllers.AuthController{}, "*:Logout"),
		// 个人信息
		web.NSRouter("/auth/me", &controllers.AuthController{}, "*:Me"),
		//试图列表
		web.NSRouter("/dashboard/index", &controllers.DashboardController{}, "*:Index"),
		//添加视图
		web.NSRouter("/dashboard/paneladd", &controllers.DashboardController{}, "*:Paneladd"),
		//获取图表
		web.NSRouter("/dashboard/dashboard", &controllers.ChartController{}, "*:Dashboard"),
		// 遥测数据
		web.NSRouter("/please_using_websocket", &controllers.ChartController{}, "*:Ws"),
		// 设备列表
		web.NSRouter("/device/index", &controllers.DeviceController{}, "*:Index"),
		// 扫码激活设备
		web.NSRouter("/device/scan", &controllers.DeviceController{}, "*:Scan"),
		// 新增告警策略
		web.NSRouter("/warning/add", &controllers.WarningController{}, "*:Add"),
		// 获取所有告警策略
		web.NSRouter("/warning/show", &controllers.WarningController{}, "*:Show"),
		// 获取具体某一条告警策略
		web.NSRouter("/warning/get_by_id", &controllers.WarningController{}, "*:GetById"),
		// 修改告警策略
		web.NSRouter("/warning/edit", &controllers.WarningController{}, "*:Edit"),
		// 删除具体某一条告警策略
		web.NSRouter("/warning/delete_by_id", &controllers.WarningController{}, "*:DeleteById"),
		// 获取告警日志
		web.NSRouter("/warning/index", &controllers.WarningController{}, "*:Index"),
		// 分页获取告警日志
		web.NSRouter("/warning/list", &controllers.WarningController{}, "*:List"),
		// 新增控制策略
		web.NSRouter("/automation/add", &controllers.AutomationController{}, "*:Add"),
		// 获取某个控制策略
		web.NSRouter("/automation/get_by_id", &controllers.AutomationController{}, "*:GetById"),
		// 修改某个控制策略
		web.NSRouter("/automation/edit", &controllers.AutomationController{}, "*:Edit"),
		// 删除某个控制策略
		web.NSRouter("/automation/delete_by_id", &controllers.AutomationController{}, "*:DeleteById"),
		// 获取定时执行参数
		web.NSRouter("/automation/status", &controllers.AutomationController{}, "*:Status"),
		// 【触发条件】获取设备属性及单位
		web.NSRouter("/automation/show", &controllers.AutomationController{}, "*:Show"),
		// 【控制指令】获取设备属性及单位
		web.NSRouter("/automation/instruct", &controllers.AutomationController{}, "*:Instruct"),
		// 获取资产信息
		web.NSRouter("/automation/property", &controllers.AutomationController{}, "*:Property"),
		// 获取所有控制策略
		web.NSRouter("/automation/index", &controllers.AutomationController{}, "*:Index"),
	)

	web.AddNamespace(api)
}
