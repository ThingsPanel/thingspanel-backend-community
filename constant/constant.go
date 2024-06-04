package constant

const (
	SYS_ADMIN    string = "SYS_ADMIN"    // 系统管理员
	TENANT_USER  string = "TENANT_USER"  // 租户用户
	TENANT_ADMIN string = "TENANT_ADMIN" // 租户系统管理员
)

const EMPTY string = ""

const (
	DIRECT_CONNECTION  int = iota + 1 //1-直连设备
	GATEWAY_DEVICE                    //2-网关设备
	GATEWAY_SON_DEVICE                // 3-网关子设备
)

const (
	Manual int = iota + 1 // 手动
	Auto                  // 自动
)

const (
	StatusOK     int = iota + 1 // 成功
	StatusFailed                // 失败
)

// DeviceModelSource 模型数据源类型
type DeviceModelSource string

const (
	TelemetrySource DeviceModelSource = "telemetry"
	AttributeSource DeviceModelSource = "attributes"
	EventSource     DeviceModelSource = "event"
	CommandSource   DeviceModelSource = "command"
)

//表单类型
type FormType string

const (
	CONFIG_FORM       FormType = "CFG"  // 配置表单
	VOUCHER_FORM      FormType = "VCR"  // 凭证表单
	VOUCHER_TYPE_FORM FormType = "VCRT" // 凭证类型表单
)

// 设备类型
const (
	DEVICE_TYPE_1 string = "1" // 直连设备
	DEVICE_TYPE_2 string = "2" // 网关设备
	DEVICE_TYPE_3 string = "3" // 网关子设备
)
