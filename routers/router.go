// @APIVersion 1.0.0
// @Title beego Test API
// @Description beego has a very cool tools to autogenerate documents for your API
// @Contact astaxie@gmail.com
// @TermsOfServiceUrl http://beego.me/
// @License Apache 2.0
// @LicenseUrl http://www.apache.org/licenses/LICENSE-2.0.html
package routers

import (
	"ThingsPanel-Go/controllers"
	"ThingsPanel-Go/middleware"

	"github.com/beego/beego/v2/server/web"
)

func init() {
	//跨域
	middleware.CorsMiddle()
	//授权登录中间件
	middleware.AuthMiddle()
	api := web.NewNamespace("/api",
		// 登录
		web.NSRouter("/auth/login", &controllers.AuthController{}, "*:Login"),
		web.NSRouter("/auth/logout", &controllers.AuthController{}, "*:Logout"),
		web.NSRouter("/auth/refresh", &controllers.AuthController{}, "*:Refresh"),
		web.NSRouter("/auth/me", &controllers.AuthController{}, "*:Me"),
		web.NSRouter("/auth/register", &controllers.AuthController{}, "*:Register"),

		// 首页
		web.NSRouter("/home/list", &controllers.HomeController{}, "*:List"),
		web.NSRouter("/home/chart", &controllers.HomeController{}, "*:Chart"),
		web.NSRouter("/index/show", &controllers.HomeController{}, "*:Show"),
		web.NSRouter("/index/device", &controllers.HomeController{}, "*:Device"),

		// 用户
		web.NSRouter("/user/index", &controllers.UserController{}, "*:Index"),
		web.NSRouter("/user/add", &controllers.UserController{}, "*:Add"),
		web.NSRouter("/user/edit", &controllers.UserController{}, "*:Edit"),
		web.NSRouter("/user/delete", &controllers.UserController{}, "*:Delete"),
		web.NSRouter("/user/password", &controllers.UserController{}, "*:Password"),
		web.NSRouter("/user/update", &controllers.UserController{}, "*:Password"),
		web.NSRouter("/user/permission", &controllers.UserController{}, "*:Permission"),

		// 客户管理
		web.NSRouter("/customer/index", &controllers.CustomerController{}, "*:Index"),
		web.NSRouter("/customer/add", &controllers.CustomerController{}, "*:Add"),
		web.NSRouter("/customer/edit", &controllers.CustomerController{}, "*:Edit"),
		web.NSRouter("/customer/delete", &controllers.CustomerController{}, "*:Delete"),

		// 业务
		web.NSRouter("/asset/index", &controllers.AssetController{}, "*:Index"),
		web.NSRouter("/asset/add", &controllers.AssetController{}, "*:Add"),
		web.NSRouter("/asset/edit", &controllers.AssetController{}, "*:Edit"),
		web.NSRouter("/asset/delete", &controllers.AssetController{}, "*:Delete"),
		web.NSRouter("/asset/widget", &controllers.AssetController{}, "*:Widget"),
		web.NSRouter("/asset/list", &controllers.AssetController{}, "*:List"),

		web.NSRouter("asset/work_index", &controllers.BusinessController{}, "*:Index"),
		web.NSRouter("asset/work_add", &controllers.BusinessController{}, "*:Add"),
		web.NSRouter("asset/work_edit", &controllers.BusinessController{}, "*:Edit"),
		web.NSRouter("asset/work_delete", &controllers.BusinessController{}, "*:Delete"),

		web.NSRouter("business/index", &controllers.BusinessController{}, "*:Index"),
		web.NSRouter("business/add", &controllers.BusinessController{}, "*:Add"),
		web.NSRouter("business/edit", &controllers.BusinessController{}, "*:Edit"),
		web.NSRouter("business/delete", &controllers.BusinessController{}, "*:Delete"),
		web.NSRouter("business/tree", &controllers.BusinessController{}, "*:Tree"),

		// 设备
		web.NSRouter("/device/token", &controllers.DeviceController{}, "*:Token"),
		web.NSRouter("/device/index", &controllers.DeviceController{}, "*:Index"),
		web.NSRouter("/device/edit", &controllers.DeviceController{}, "*:Edit"),
		web.NSRouter("/device/add", &controllers.DeviceController{}, "*:Add"),
		web.NSRouter("/device/delete", &controllers.DeviceController{}, "*:Delete"),

		//可视化
		web.NSRouter("/dashboard/index", &controllers.DashBoardController{}, "*:Index"),
		web.NSRouter("/dashboard/add", &controllers.WidgetController{}, "*:Add"),
		web.NSRouter("/dashboard/edit", &controllers.WidgetController{}, "*:Edit"),
		web.NSRouter("/dashboard/delete", &controllers.WidgetController{}, "*:Delete"),
		web.NSRouter("/dashboard/paneladd", &controllers.DashBoardController{}, "*:Add"),
		web.NSRouter("/dashboard/paneldelete", &controllers.DashBoardController{}, "*:Delete"),
		web.NSRouter("/dashboard/paneledit", &controllers.DashBoardController{}, "*:Edit"),
		web.NSRouter("/dashboard/list", &controllers.DashBoardController{}, "*:List"),
		web.NSRouter("/dashboard/business", &controllers.DashBoardController{}, "*:Business"),
		web.NSRouter("/dashboard/property", &controllers.DashBoardController{}, "*:Property"),
		web.NSRouter("/dashboard/device", &controllers.DashBoardController{}, "*:Device"),
		web.NSRouter("/dashboard/inserttime", &controllers.DashBoardController{}, "*:Inserttime"),
		web.NSRouter("/dashboard/gettime", &controllers.DashBoardController{}, "*:Gettime"),
		web.NSRouter("/dashboard/dashboard", &controllers.DashBoardController{}, "*:Dashboard"),
		web.NSRouter("/dashboard/realTime", &controllers.DashBoardController{}, "*:Realtime"),
		web.NSRouter("/dashboard/updateDashboard", &controllers.DashBoardController{}, "*:Updatedashboard"),
		web.NSRouter("/dashboard/component", &controllers.DashBoardController{}, "*:Component"),

		web.NSRouter("/markets/list", &controllers.MarketsController{}, "*:List"),

		//告警策略
		web.NSRouter("/warning/index", &controllers.WarninglogController{}, "*:Index"),
		web.NSRouter("/warning/list", &controllers.WarninglogController{}, "*:List"),
		web.NSRouter("/warning/field", &controllers.WarningconfigController{}, "*:Field"),
		web.NSRouter("/warning/add", &controllers.WarningconfigController{}, "*:Add"),
		web.NSRouter("/warning/edit", &controllers.WarningconfigController{}, "*:Edit"),
		web.NSRouter("/warning/delete", &controllers.WarningconfigController{}, "*:Delete"),
		web.NSRouter("/warning/show", &controllers.WarningconfigController{}, "*:Index"),
		web.NSRouter("/warning/update", &controllers.WarningconfigController{}, "*:GetOne"),

		//控制策略
		web.NSRouter("/automation/index", &controllers.AutomationController{}, "*:Index"),
		web.NSRouter("/automation/add", &controllers.AutomationController{}, "*:Add"),
		web.NSRouter("/automation/edit", &controllers.AutomationController{}, "*:Edit"),
		web.NSRouter("/automation/delete", &controllers.AutomationController{}, "*:Delete"),
		web.NSRouter("/automation/get_by_id", &controllers.AutomationController{}, "*:GetOne"),
		web.NSRouter("/automation/status", &controllers.AutomationController{}, "*:Status"),
		web.NSRouter("/automation/symbol", &controllers.AutomationController{}, "*:Symbol"),
		web.NSRouter("/automation/property", &controllers.AutomationController{}, "*:Property"),
		web.NSRouter("/automation/show", &controllers.AutomationController{}, "*:Show"),
		web.NSRouter("/automation/update", &controllers.AutomationController{}, "*:Update"),
		web.NSRouter("/automation/instruct", &controllers.AutomationController{}, "*:Instruct"),

		// 操作日志
		web.NSRouter("/operation/index", &controllers.OperationlogController{}, "*:Index"),
		web.NSRouter("/operation/list", &controllers.OperationlogController{}, "*:List"),

		web.NSRouter("/structure/add", &controllers.StructureController{}, "*:Add"),
		web.NSRouter("/structure/list", &controllers.StructureController{}, "*:Index"),
		web.NSRouter("/structure/update", &controllers.StructureController{}, "*:Edit"),
		web.NSRouter("/structure/delete", &controllers.StructureController{}, "*:Delete"),
		web.NSRouter("/structure/field", &controllers.StructureController{}, "*:Field"),

		web.NSRouter("/navigation/add", &controllers.NavigationController{}, "*:Add"),
		web.NSRouter("/navigation/list", &controllers.NavigationController{}, "*:List"),

		web.NSRouter("/kv/list", &controllers.KvController{}, "*:List"),
		web.NSRouter("/kv/index", &controllers.KvController{}, "*:Index"),
		web.NSRouter("/kv/export", &controllers.KvController{}, "*:Export"),
	)
	// 图表推送数据
	web.Router("/ws", &controllers.WebsocketController{}, "*:WsHandler")
	web.AddNamespace(api)
}
