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
	//日志中间件
	middleware.LogMiddle()

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
		web.NSRouter("/user/role/add", &controllers.TpRoleController{}, "*:Add"),
		web.NSRouter("/user/role/list", &controllers.TpRoleController{}, "*:List"),
		web.NSRouter("/user/role/edit", &controllers.TpRoleController{}, "*:Edit"),
		web.NSRouter("/user/role/delete", &controllers.TpRoleController{}, "*:Delete"),
		web.NSRouter("/user/function/add", &controllers.TpFunctionController{}, "*:Add"),
		web.NSRouter("/user/function/list", &controllers.TpFunctionController{}, "*:List"),
		web.NSRouter("/user/function/edit", &controllers.TpFunctionController{}, "*:Edit"),
		web.NSRouter("/user/function/delete", &controllers.TpFunctionController{}, "*:Delete"),

		// 客户管理
		web.NSRouter("/customer/index", &controllers.CustomerController{}, "*:Index"),
		web.NSRouter("/customer/add", &controllers.CustomerController{}, "*:Add"),
		web.NSRouter("/customer/edit", &controllers.CustomerController{}, "*:Edit"),
		web.NSRouter("/customer/delete", &controllers.CustomerController{}, "*:Delete"),
		// 映射
		web.NSRouter("/field/add_only", &controllers.FieldmappingController{}, "*:AddOnly"),
		web.NSRouter("/field/update_only", &controllers.FieldmappingController{}, "*:UpdateOnly"),
		// 业务
		web.NSRouter("/asset/add_only", &controllers.AssetController{}, "*:AddOnly"),
		web.NSRouter("/asset/update_only", &controllers.AssetController{}, "*:UpdateOnly"),
		web.NSRouter("/asset/index", &controllers.AssetController{}, "*:Index"),
		web.NSRouter("/asset/add", &controllers.AssetController{}, "*:Add"),
		web.NSRouter("/asset/edit", &controllers.AssetController{}, "*:Edit"),
		web.NSRouter("/asset/delete", &controllers.AssetController{}, "*:Delete"),
		web.NSRouter("/asset/widget", &controllers.AssetController{}, "*:Widget"),
		web.NSRouter("/asset/list", &controllers.AssetController{}, "*:List"),
		web.NSRouter("/asset/list/a", &controllers.AssetController{}, "*:GetAssetByBusiness"),
		web.NSRouter("/asset/list/b", &controllers.AssetController{}, "*:GetAssetByAsset"),

		web.NSRouter("/asset/simple", &controllers.AssetController{}, "*:Simple"),

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
		web.NSRouter("/device/add_only", &controllers.DeviceController{}, "*:AddOnly"),
		web.NSRouter("/device/update_only", &controllers.DeviceController{}, "*:UpdateOnly"),
		web.NSRouter("/device/token", &controllers.DeviceController{}, "*:Token"),
		web.NSRouter("/device/index", &controllers.DeviceController{}, "*:Index"),
		web.NSRouter("/device/edit", &controllers.DeviceController{}, "*:Edit"),
		web.NSRouter("/device/add", &controllers.DeviceController{}, "*:Add"),
		web.NSRouter("/device/delete", &controllers.DeviceController{}, "*:Delete"),
		web.NSRouter("/device/configure", &controllers.DeviceController{}, "*:Configure"),
		web.NSRouter("/device/operating_device", &controllers.DeviceController{}, "*:Operating"),
		web.NSRouter("/device/reset", &controllers.DeviceController{}, "*:Reset"),
		web.NSRouter("/device/data", &controllers.DeviceController{}, "*:DeviceById"),
		web.NSRouter("/device/list", &controllers.DeviceController{}, "*:PageList"),

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
		// 业务预览组件查询临时接口
		web.NSRouter("/dashboard/business/component", &controllers.DashBoardController{}, "*:BidComponent"),
		// 业务id去查资产
		web.NSRouter("/dashboard/property", &controllers.DashBoardController{}, "*:Property"),
		web.NSRouter("/dashboard/device", &controllers.DashBoardController{}, "*:Device"),
		web.NSRouter("/dashboard/inserttime", &controllers.DashBoardController{}, "*:Inserttime"),
		web.NSRouter("/dashboard/gettime", &controllers.DashBoardController{}, "*:Gettime"),
		web.NSRouter("/dashboard/dashboard", &controllers.DashBoardController{}, "*:Dashboard"),
		web.NSRouter("/dashboard/realTime", &controllers.DashBoardController{}, "*:Realtime"),
		web.NSRouter("/dashboard/updateDashboard", &controllers.DashBoardController{}, "*:Updatedashboard"),
		web.NSRouter("/dashboard/component", &controllers.DashBoardController{}, "*:Component"),
		web.NSRouter("/dashboard/pluginList", &controllers.DashBoardController{}, "*:PluginList"),

		web.NSRouter("/markets/list", &controllers.MarketsController{}, "*:List"),

		//告警策略
		web.NSRouter("/warning/index", &controllers.WarninglogController{}, "*:Index"),
		web.NSRouter("/warning/list", &controllers.WarninglogController{}, "*:List"),
		web.NSRouter("/warning/log/list", &controllers.WarninglogController{}, "*:PageList"),
		web.NSRouter("/warning/field", &controllers.WarningconfigController{}, "*:Field"),
		web.NSRouter("/warning/add", &controllers.WarningconfigController{}, "*:Add"),
		web.NSRouter("/warning/edit", &controllers.WarningconfigController{}, "*:Edit"),
		web.NSRouter("/warning/delete", &controllers.WarningconfigController{}, "*:Delete"),
		web.NSRouter("/warning/show", &controllers.WarningconfigController{}, "*:Index"),
		web.NSRouter("/warning/update", &controllers.WarningconfigController{}, "*:GetOne"),
		web.NSRouter("/warning/view", &controllers.WarninglogController{}, "*:GetDeviceWarningList"),
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
		// 更新统计点击数量，dashboard_id(chart_id)关联业务id，第一次请求会新增一条，后续为更新
		web.NSRouter("/navigation/add", &controllers.NavigationController{}, "*:Add"),
		web.NSRouter("/navigation/list", &controllers.NavigationController{}, "*:List"),

		web.NSRouter("/kv/list", &controllers.KvController{}, "*:List"),
		web.NSRouter("/kv/index", &controllers.KvController{}, "*:Index"),
		web.NSRouter("/kv/export", &controllers.KvController{}, "*:Export"),
		web.NSRouter("/kv/current", &controllers.KvController{}, "*:CurrentData"),
		web.NSRouter("/kv/current/business", &controllers.KvController{}, "*:CurrentDataByBusiness"),
		// 通过设备id查询设备历史数据
		web.NSRouter("/kv/device/history", &controllers.KvController{}, "*:DeviceHistoryData"),
		// 系统设置接口
		web.NSRouter("/system/logo/index", &controllers.LogoController{}, "*:Index"),
		web.NSRouter("/system/logo/update", &controllers.LogoController{}, "*:Edit"),
		// 图标单元小功能
		web.NSRouter("/widget/extend/update", &controllers.WidgetController{}, "*:UpdateExtend"),

		// 文件上传接口
		web.NSRouter("/file/up", &controllers.UploadController{}, "*:UpFile"),
		// 三方数据接口
		web.NSRouter("/open/data", &controllers.OpenController{}, "*:GetData"),
		// 控制日志
		web.NSRouter("/conditions/log/index", &controllers.ConditionslogController{}, "*:Index"),
	)

	// 图表推送数据
	web.Router("/ws", &controllers.WebsocketController{}, "*:WsHandler")
	web.AddNamespace(api)
}
