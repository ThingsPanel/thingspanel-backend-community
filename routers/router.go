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
	//权限中间件
	middleware.CasbinMiddle()
	//日志中间件
	middleware.LogMiddle()

	api := web.NewNamespace("/api",
		// 登录
		web.NSRouter("/auth/login", &controllers.AuthController{}, "*:Login"),     //ty
		web.NSRouter("/auth/logout", &controllers.AuthController{}, "*:Logout"),   //ty
		web.NSRouter("/auth/refresh", &controllers.AuthController{}, "*:Refresh"), //ty
		web.NSRouter("/auth/me", &controllers.AuthController{}, "*:Me"),           //ty
		// 用户注册
		web.NSRouter("/auth/register", &controllers.AuthController{}, "*:Register"), //ty

		// 首页cpu和内存监控
		web.NSRouter("/home/list", &controllers.HomeController{}, "*:List"),   //ty
		web.NSRouter("/home/chart", &controllers.HomeController{}, "*:Chart"), //ty
		web.NSRouter("/index/show", &controllers.HomeController{}, "*:Show"),
		// 协议默认配置查看
		web.NSRouter("/index/default_setting", &controllers.HomeController{}, "*:GetDefaultSetting"), //ty

		web.NSRouter("/index/device", &controllers.HomeController{}, "*:Device"),

		// 用户管理列表分页查询
		web.NSRouter("/user/index", &controllers.UserController{}, "*:Index"), //yonghu-ck
		//新增用户
		web.NSRouter("/user/add", &controllers.UserController{}, "*:Add"),       // yonghu-w
		web.NSRouter("/user/edit", &controllers.UserController{}, "*:Edit"),     //yonghu-w
		web.NSRouter("/user/delete", &controllers.UserController{}, "*:Delete"), //yonghu-w
		//用户管理中用户密码修改
		web.NSRouter("/user/password", &controllers.UserController{}, "*:Password"), //yonghu-w
		//个人用户密码修改
		web.NSRouter("/user/update", &controllers.UserController{}, "*:Password"), //ty
		web.NSRouter("/user/permission", &controllers.UserController{}, "*:Permission"),
		web.NSRouter("/user/role/add", &controllers.TpRoleController{}, "*:Add"),
		web.NSRouter("/user/role/list", &controllers.TpRoleController{}, "*:List"),
		web.NSRouter("/user/role/edit", &controllers.TpRoleController{}, "*:Edit"),
		web.NSRouter("/user/role/delete", &controllers.TpRoleController{}, "*:Delete"),
		web.NSRouter("/user/function/add", &controllers.TpFunctionController{}, "*:Add"),
		web.NSRouter("/user/function/list", &controllers.TpFunctionController{}, "*:List"),
		web.NSRouter("/user/function/edit", &controllers.TpFunctionController{}, "*:Edit"),
		web.NSRouter("/user/function/delete", &controllers.TpFunctionController{}, "*:Delete"),
		web.NSRouter("/user/function/pull-down-list", &controllers.TpFunctionController{}, "*:FunctionPullDownList"),
		web.NSRouter("/user/function/auth", &controllers.TpFunctionController{}, "*:UserAuth"),      //获取用户权限树
		web.NSRouter("/user/function/tree", &controllers.TpFunctionController{}, "*:AuthorityList"), //获取权限树

		//菜单管理
		web.NSRouter("/menu/tree", &controllers.TpMenuController{}, "*:Tree"),
		web.NSRouter("/menu/role/index", &controllers.TpRoleMenuController{}, "*:Index"),
		web.NSRouter("/menu/role/add", &controllers.TpRoleMenuController{}, "*:Add"),
		web.NSRouter("/menu/role/edit", &controllers.TpRoleMenuController{}, "*:Edit"),
		web.NSRouter("/menu/user", &controllers.TpRoleMenuController{}, "*:UserMenus"),
		// 权限管理

		web.NSRouter("/casbin/role/function/add", &controllers.CasbinController{}, "*:AddFunctionToRole"),
		web.NSRouter("/casbin/role/function/index", &controllers.CasbinController{}, "*:GetFunctionFromRole"),
		web.NSRouter("/casbin/role/function/update", &controllers.CasbinController{}, "*:UpdateFunctionFromRole"),
		web.NSRouter("/casbin/role/function/delete", &controllers.CasbinController{}, "*:DeleteFunctionFromRole"),
		web.NSRouter("/casbin/user/role/add", &controllers.CasbinController{}, "*:AddRoleToUser"),
		web.NSRouter("/casbin/user/role/index", &controllers.CasbinController{}, "*:GetRolesFromUser"),
		web.NSRouter("/casbin/user/role/update", &controllers.CasbinController{}, "*:UpdateRolesFromUser"),
		web.NSRouter("/casbin/user/role/delete", &controllers.CasbinController{}, "*:DeleteRolesFromUser"),

		// 客户管理
		web.NSRouter("/customer/index", &controllers.CustomerController{}, "*:Index"),
		web.NSRouter("/customer/add", &controllers.CustomerController{}, "*:Add"),
		web.NSRouter("/customer/edit", &controllers.CustomerController{}, "*:Edit"),
		web.NSRouter("/customer/delete", &controllers.CustomerController{}, "*:Delete"),
		// 设备管理配置映射
		web.NSRouter("/field/add_only", &controllers.FieldmappingController{}, "*:AddOnly"), //shebei-w
		web.NSRouter("/field/update_only", &controllers.FieldmappingController{}, "*:UpdateOnly"),
		web.NSRouter("/field/device/index", &controllers.FieldmappingController{}, "*:GetByDeviceid"),
		// 设备分组添加
		web.NSRouter("/asset/add_only", &controllers.AssetController{}, "*:AddOnly"), //shebei-w
		web.NSRouter("/asset/update_only", &controllers.AssetController{}, "*:UpdateOnly"),
		//设备管理的插件选择下拉
		web.NSRouter("/asset/index", &controllers.AssetController{}, "*:Index"), //shebei-ck
		web.NSRouter("/asset/add", &controllers.AssetController{}, "*:Add"),
		web.NSRouter("/asset/edit", &controllers.AssetController{}, "*:Edit"),
		// 设备分组删除
		web.NSRouter("/asset/delete", &controllers.AssetController{}, "*:Delete"), //shebei-w
		web.NSRouter("/asset/widget", &controllers.AssetController{}, "*:Widget"),
		//可视化面板设备相关查询;数据管理下拉查询
		web.NSRouter("/asset/list", &controllers.AssetController{}, "*:List"), //ty
		// 设备分组动态加载下拉
		web.NSRouter("/asset/list/a", &controllers.AssetController{}, "*:GetAssetByBusiness"), //ty
		web.NSRouter("/asset/list/b", &controllers.AssetController{}, "*:GetAssetByAsset"),
		//设备分组列表查询
		web.NSRouter("/asset/list/c", &controllers.AssetController{}, "*:GetAssetGroupByBusinessId"), //shebei-ck
		// 设备管理的设备分组下拉
		web.NSRouter("/asset/list/d", &controllers.AssetController{}, "*:GetAssetGroupByBusinessIdX"), //shebei-ck

		web.NSRouter("/asset/simple", &controllers.AssetController{}, "*:Simple"),
		// 自动化列表查询，系统刚进入时候调了这个接口 有问题？
		web.NSRouter("asset/work_index", &controllers.BusinessController{}, "*:Index"), //ziduhua-ck
		web.NSRouter("asset/work_add", &controllers.BusinessController{}, "*:Add"),
		web.NSRouter("asset/work_edit", &controllers.BusinessController{}, "*:Edit"),
		web.NSRouter("asset/work_delete", &controllers.BusinessController{}, "*:Delete"),
		// 业务列表分页查询
		web.NSRouter("business/index", &controllers.BusinessController{}, "*:Index"),   // ty
		web.NSRouter("business/add", &controllers.BusinessController{}, "*:Add"),       //yewu-w
		web.NSRouter("business/edit", &controllers.BusinessController{}, "*:Edit"),     //yewu-w
		web.NSRouter("business/delete", &controllers.BusinessController{}, "*:Delete"), //yewu-w
		web.NSRouter("business/tree", &controllers.BusinessController{}, "*:Tree"),

		// 设备新增
		web.NSRouter("/device/add_only", &controllers.DeviceController{}, "*:AddOnly"),       //shebei-w
		web.NSRouter("/device/update_only", &controllers.DeviceController{}, "*:UpdateOnly"), //shebei-w
		web.NSRouter("/device/token", &controllers.DeviceController{}, "*:Token"),
		web.NSRouter("/device/index", &controllers.DeviceController{}, "*:Index"),
		web.NSRouter("/device/edit", &controllers.DeviceController{}, "*:Edit"),
		web.NSRouter("/device/add", &controllers.DeviceController{}, "*:Add"),
		web.NSRouter("/device/delete", &controllers.DeviceController{}, "*:Delete"),
		web.NSRouter("/device/configure", &controllers.DeviceController{}, "*:Configure"),
		web.NSRouter("/device/operating_device", &controllers.DeviceController{}, "*:Operating"),
		web.NSRouter("/device/reset", &controllers.DeviceController{}, "*:Reset"),
		web.NSRouter("/device/data", &controllers.DeviceController{}, "*:DeviceById"),
		// 设备列表分页查询
		web.NSRouter("/device/list", &controllers.DeviceController{}, "*:PageList"), //shebei-ck

		//可视化列表分页查询
		web.NSRouter("/dashboard/index", &controllers.DashBoardController{}, "*:Index"), //keshihua-ck
		//可视化中添加图表单元
		web.NSRouter("/dashboard/add", &controllers.WidgetController{}, "*:Add"), //keshhua-w
		web.NSRouter("/dashboard/edit", &controllers.WidgetController{}, "*:Edit"),
		//可视化图表单元删除
		web.NSRouter("/dashboard/delete", &controllers.WidgetController{}, "*:Delete"), //keshhua-w
		//添加一个可视化面板
		web.NSRouter("/dashboard/paneladd", &controllers.DashBoardController{}, "*:Add"),       //keshihua-w
		web.NSRouter("/dashboard/paneldelete", &controllers.DashBoardController{}, "*:Delete"), //keshihua-w
		web.NSRouter("/dashboard/paneledit", &controllers.DashBoardController{}, "*:Edit"),     //keshihua-w
		web.NSRouter("/dashboard/list", &controllers.DashBoardController{}, "*:List"),
		// 新增可视化的业务下拉选项
		web.NSRouter("/dashboard/business", &controllers.DashBoardController{}, "*:Business"), //keshihua-ck
		// 业务预览组件查询临时接口
		web.NSRouter("/dashboard/business/component", &controllers.DashBoardController{}, "*:BidComponent"), //ty
		// 可视化中业务id去查设备分组下拉
		web.NSRouter("/dashboard/property", &controllers.DashBoardController{}, "*:Property"), //ty
		//自动化告警中，根据设备分组id查设备下拉
		web.NSRouter("/dashboard/device", &controllers.DashBoardController{}, "*:Device"), //ty
		web.NSRouter("/dashboard/inserttime", &controllers.DashBoardController{}, "*:Inserttime"),
		// 可视化信息查询
		web.NSRouter("/dashboard/gettime", &controllers.DashBoardController{}, "*:Gettime"), //keshihua-ck
		// 可视化面板上的图表查询
		web.NSRouter("/dashboard/dashboard", &controllers.DashBoardController{}, "*:Dashboard"), //keshihua-ck
		web.NSRouter("/dashboard/realTime", &controllers.DashBoardController{}, "*:Realtime"),
		// 可视化图表调整
		web.NSRouter("/dashboard/updateDashboard", &controllers.DashBoardController{}, "*:Updatedashboard"), //keshih-w
		web.NSRouter("/dashboard/component", &controllers.DashBoardController{}, "*:Component"),
		// 插件列表查询
		web.NSRouter("/dashboard/pluginList", &controllers.DashBoardController{}, "*:PluginList"), //ty
		// 应用管理列表查询
		web.NSRouter("/markets/list", &controllers.MarketsController{}, "*:List"), //ty

		//告警策略
		web.NSRouter("/warning/index", &controllers.WarninglogController{}, "*:Index"),
		web.NSRouter("/warning/list", &controllers.WarninglogController{}, "*:List"),
		//告警信息分页查询
		web.NSRouter("/warning/log/list", &controllers.WarninglogController{}, "*:PageList"), //gaojinlog-ck
		web.NSRouter("/warning/field", &controllers.WarningconfigController{}, "*:Field"),
		//告警策略添加
		web.NSRouter("/warning/add", &controllers.WarningconfigController{}, "*:Add"), //zidonghua-w
		//告警策略修改
		web.NSRouter("/warning/edit", &controllers.WarningconfigController{}, "*:Edit"),     //zidonghua-w
		web.NSRouter("/warning/delete", &controllers.WarningconfigController{}, "*:Delete"), //zidonghua-w
		//自动化告警列表查询
		web.NSRouter("/warning/show", &controllers.WarningconfigController{}, "*:Index"), //zidonghua-ck
		web.NSRouter("/warning/update", &controllers.WarningconfigController{}, "*:GetOne"),
		web.NSRouter("/warning/view", &controllers.WarninglogController{}, "*:GetDeviceWarningList"),
		//自动化控制列表查询
		web.NSRouter("/automation/index", &controllers.AutomationController{}, "*:Index"), //kongzhi-ck
		//自动化控制添加
		web.NSRouter("/automation/add", &controllers.AutomationController{}, "*:Add"), //kongzhi-w
		//自动化控制编辑
		web.NSRouter("/automation/edit", &controllers.AutomationController{}, "*:Edit"),         //kongzhi-w
		web.NSRouter("/automation/details", &controllers.AutomationController{}, "*:GetDetial"), //kongzhi-w

		//自动化控制删除
		web.NSRouter("/automation/delete", &controllers.AutomationController{}, "*:Delete"), //kongzhi-w
		web.NSRouter("/automation/get_by_id", &controllers.AutomationController{}, "*:GetOne"),
		//自动化告警时间段下拉查询
		web.NSRouter("/automation/status", &controllers.AutomationController{}, "*:Status"), //ty
		//自动化逻辑条件符号查询
		web.NSRouter("/automation/symbol", &controllers.AutomationController{}, "*:Symbol"), //ty
		//自动化设备分组查询
		web.NSRouter("/automation/property", &controllers.AutomationController{}, "*:Property"), //ty
		//自动化告警中查询设备插件的所有字段、符号、类型
		web.NSRouter("/automation/show", &controllers.AutomationController{}, "*:Show"), //ty
		// 自动化控制编辑时候查询调用
		web.NSRouter("/automation/update", &controllers.AutomationController{}, "*:Update"), //kongzhi-ck
		//自动化控制中设备插件所有字段查询
		web.NSRouter("/automation/instruct", &controllers.AutomationController{}, "*:Instruct"), //ty

		// 操作日志
		web.NSRouter("/operation/index", &controllers.OperationlogController{}, "*:Index"),
		//操作日志列表分页查询
		web.NSRouter("/operation/list", &controllers.OperationlogController{}, "*:List"), //caozuolog-ck

		web.NSRouter("/structure/add", &controllers.StructureController{}, "*:Add"),
		web.NSRouter("/structure/list", &controllers.StructureController{}, "*:Index"),
		web.NSRouter("/structure/update", &controllers.StructureController{}, "*:Edit"),
		//设备字段映射删除一条
		web.NSRouter("/structure/delete", &controllers.StructureController{}, "*:Delete"), //shebei-w
		// 插件映射字段查询
		web.NSRouter("/structure/field", &controllers.StructureController{}, "*:Field"), // ty
		// 更新统计点击数量，dashboard_id(chart_id)关联业务id，第一次请求会新增一条，后续为更新
		web.NSRouter("/navigation/add", &controllers.NavigationController{}, "*:Add"), // ty
		// 首页最近访问
		web.NSRouter("/navigation/list", &controllers.NavigationController{}, "*:List"), //ty

		web.NSRouter("/kv/list", &controllers.KvController{}, "*:List"),
		// 数据管理列表分页查询
		web.NSRouter("/kv/index", &controllers.KvController{}, "*:Index"), //shuju-ck
		//数据导出功能
		web.NSRouter("/kv/export", &controllers.KvController{}, "*:Export"), //shuju-daochu
		web.NSRouter("/kv/current", &controllers.KvController{}, "*:CurrentData"),
		// 查看业务下所有设备当前值
		web.NSRouter("/kv/current/business", &controllers.KvController{}, "*:CurrentDataByBusiness"), //keshihua-ck
		web.NSRouter("/kv/current/asset", &controllers.KvController{}, "*:CurrentDataByAsset"),       //keshihua-ck
		web.NSRouter("/kv/current/asset/a", &controllers.KvController{}, "*:CurrentDataByAssetA"),    //keshihua-ck

		// 通过设备id查询设备历史数据
		web.NSRouter("/kv/device/history", &controllers.KvController{}, "*:DeviceHistoryData"),
		// 系统设置接口
		web.NSRouter("/system/logo/index", &controllers.LogoController{}, "*:Index"), //ty
		//常规设置修改
		web.NSRouter("/system/logo/update", &controllers.LogoController{}, "*:Edit"), //changguishezhi-w
		// 图标单元小功能
		web.NSRouter("/widget/extend/update", &controllers.WidgetController{}, "*:UpdateExtend"),

		// 文件上传接口
		web.NSRouter("/file/up", &controllers.UploadController{}, "*:UpFile"), //logo-w
		// 三方数据接口
		web.NSRouter("/open/data", &controllers.OpenController{}, "*:GetData"),
		// 控制日志
		web.NSRouter("/conditions/log/index", &controllers.ConditionslogController{}, "*:Index"),

		//数据转发
		web.NSRouter("/data/transpond/add", &controllers.DataTranspondController{}, "*:Add"),
		web.NSRouter("/data/transpond/list", &controllers.DataTranspondController{}, "*:List"),
		web.NSRouter("/data/transpond/edit", &controllers.DataTranspondController{}, "*:Edit"),
		web.NSRouter("/data/transpond/delete", &controllers.DataTranspondController{}, "*:Delete"),
	)

	// 图表推送数据
	web.Router("/ws", &controllers.WebsocketController{}, "*:WsHandler")
	web.AddNamespace(api)
}
