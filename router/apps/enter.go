package apps

type apps struct {
	User                       // 用户模块
	Role                       // 角色管理
	Casbin                     // 权限
	Dict                       // 字典模块
	OTA                        // OTA
	UpLoad                     // 文件上传
	ProtocolPlugin             // 协议插件
	Device                     // 设备
	UiElements                 // ui元素控制
	Board                      // 首页
	EventData                  // 属性数据
	TelemetryData              // 遥测数据
	AttributeData              // 属性数据
	CommandData                // 命令数据
	OperationLog               // 操作日志
	Logo                       // 站标
	DataPolicy                 // 数据清理
	DeviceConfig               // 设备配置
	DataScript                 // 数据处理脚本
	NotificationGroup          // 通知组
	NotificationHistoryGroup   // 通知历史组
	NotificationServicesConfig // 通知服务配置
	Alarm
	SceneAutomations
	Scene
	SysFunction
	ServicePlugin // 插件管理
	ExpectedData  // 预期数据
	OpenAPIKey    // openAPI

}

var Model = new(apps)
