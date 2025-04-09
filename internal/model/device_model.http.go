package model

const (
	DEVICE_MODEL_TELEMETRY  = "DEVICE_MODEL_TELEMETRY"
	DEVICE_MODEL_ATTRIBUTES = "DEVICE_MODEL_ATTRIBUTES"
	DEVICE_MODEL_EVENTS     = "DEVICE_MODEL_EVENTS"
	DEVICE_MODEL_COMMANDS   = "DEVICE_MODEL_COMMANDS"
)

// 物模型创建 （telemetry和attributes）
type CreateDeviceModelReq struct {
	DeviceTemplateId string  `json:"device_template_id" validate:"required,max=36"`          // 设备模版ID
	DataName         *string `json:"data_name" validate:"omitempty,max=255"`                 // 数据名称
	DataIdentifier   string  `json:"data_identifier" validate:"required,max=255"`            // 数据标识符
	ReadWriteFlag    *string `json:"read_write_flag" validate:"omitempty,max=10,oneof=R RW"` // 读写标志R-读 W-写 RW-读写
	DataType         *string `json:"data_type" validate:"omitempty,max=50"`                  // 数据类型String Number Boolean
	Unit             *string `json:"unit" validate:"omitempty,max=50"`                       // 单位
	Description      *string `json:"description" validate:"omitempty,max=500"`               // 描述
	AdditionalInfo   *string `json:"additional_info" validate:"omitempty"`                   // 附加信息
	Remark           *string `json:"remark" validate:"omitempty,max=255"`                    // 备注
}

// 物模型创建 （events和commands）
type CreateDeviceModelV2Req struct {
	DeviceTemplateId string  `json:"device_template_id" validate:"required,max=36"` // 设备模版ID
	DataName         *string `json:"data_name" validate:"omitempty,max=255"`        // 数据名称
	DataIdentifier   string  `json:"data_identifier" validate:"required,max=255"`   // 数据标识符
	Params           *string `json:"params" validate:"omitempty"`                   // 参数
	Description      *string `json:"description" validate:"omitempty,max=500"`      // 描述
	AdditionalInfo   *string `json:"additional_info" validate:"omitempty"`          // 附加信息
	Remark           *string `json:"remark" validate:"omitempty,max=255"`           // 备注
}

// 物模型更新 （telemetry和attributes）
type UpdateDeviceModelReq struct {
	ID             string  `json:"id" validate:"required,max=36"`                          // ID
	DataName       *string `json:"data_name" validate:"omitempty,max=255"`                 // 数据名称
	DataIdentifier string  `json:"data_identifier" validate:"required,max=255"`            // 数据标识符
	ReadWriteFlag  *string `json:"read_write_flag" validate:"omitempty,max=10,oneof=R RW"` // 读写标志R-读 RW-读写
	DataType       *string `json:"data_type" validate:"omitempty,max=50"`                  // 数据类型String Number Boolean
	Unit           *string `json:"unit" validate:"omitempty,max=50"`                       // 单位
	Description    *string `json:"description" validate:"omitempty,max=500"`               // 描述
	AdditionalInfo *string `json:"additional_info" validate:"omitempty"`                   // 附加信息
	Remark         *string `json:"remark" validate:"omitempty,max=255"`                    // 备注
}

// 物模型更新 （events和commands）
type UpdateDeviceModelV2Req struct {
	ID             string  `json:"id" validate:"required,max=36"`               // ID
	DataName       *string `json:"data_name" validate:"omitempty,max=255"`      // 数据名称
	DataIdentifier string  `json:"data_identifier" validate:"required,max=255"` // 数据标识符
	Params         *string `json:"params" validate:"omitempty"`                 // 参数
	Description    *string `json:"description" validate:"omitempty,max=500"`    // 描述
	AdditionalInfo *string `json:"additional_info" validate:"omitempty"`        // 附加信息
	Remark         *string `json:"remark" validate:"omitempty,max=255"`         // 备注
}

type GetDeviceModelListByPageReq struct {
	PageReq
	DeviceTemplateId string  `json:"device_template_id" form:"device_template_id"  validate:"required,max=36"` // 设备模版ID
	EnableStatus     *string `json:"enable_status"  form:"enable_status" validate:"omitempty,max=10"`          // 启用状态
}

type GetModelSourceATRes struct {
	DataSourceTypeRes string     `json:"data_source_type"`
	Options           []*Options `json:"options"`
}

type Options struct {
	Key      string     `json:"key"`
	Label    *string    `json:"label"`
	DataType *string    `json:"data_type"`
	Enum     []EnumItem `json:"enum"`
}

type EnumItem struct {
	ValueType   string `json:"value_type"`
	Value       int    `json:"value"`
	Description string `json:"description"`
}

type CreateDeviceModelCustomCommandReq struct {
	DeviceTemplateId string  `json:"device_template_id" validate:"required,max=36"` // 设备模版ID
	ButtomName       string  `json:"buttom_name" validate:"required,max=36"`        // 按钮名称
	DataIdentifier   string  `json:"data_identifier" validate:"required,max=255"`   // 数据标识符
	Description      *string `json:"description" validate:"omitempty,max=500"`      // 描述
	Instruct         *string `json:"instruct" validate:"omitempty"`                 // 指令内容
	EnableStatus     string  `json:"enable_status" validate:"required,max=10"`      // 启用状态
	Remark           *string `json:"remark" validate:"omitempty,max=255"`           // 备注
}

type UpdateDeviceModelCustomCommandReq struct {
	ID             string  `json:"id" validate:"required,max=36"`               // ID
	ButtomName     string  `json:"buttom_name" validate:"required,max=36"`      // 按钮名称
	DataIdentifier string  `json:"data_identifier" validate:"required,max=255"` // 数据标识符
	Description    *string `json:"description" validate:"omitempty,max=500"`    // 描述
	Instruct       *string `json:"instruct" validate:"omitempty"`               // 指令内容
	EnableStatus   string  `json:"enable_status" validate:"required,max=10"`    // 启用状态
	Remark         *string `json:"remark" validate:"omitempty,max=255"`         // 备注
}

type GetDeviceMetricsChartReq struct {
	DeviceID          string  `json:"device_id" form:"device_id" validate:"required"`                                                                                                                                                  // 设备ID
	DataType          string  `json:"data_type" form:"data_type" validate:"required,oneof=telemetry attribute command event"`                                                                                                          // 设备数据类型
	DataMode          string  `json:"data_mode" form:"data_mode" validate:"required,oneof=latest history"`                                                                                                                             // 数据模式
	Key               string  `json:"key" form:"key" validate:"required"`                                                                                                                                                              // 数据标识符
	TimeRange         *string `json:"time_range" form:"time_range" validate:"omitempty,oneof=last_5m last_15m last_30m last_1h last_3h last_6h last_12h last_24h last_3d last_7d last_15d last_30d last_60d last_90d last_6m last_1y"` // 时间范围
	AggregateWindow   *string `json:"aggregate_window" form:"aggregate_window" validate:"omitempty,oneof=no_aggregate 30s 1m 2m 5m 10m 30m 1h 3h 6h 1d 7d 1mo"`                                                                        // 聚合间隔
	AggregateFunction *string `json:"aggregate_function" form:"aggregate_function" validate:"omitempty,oneof=avg max min sum diff"`                                                                                                    // 聚合方法
}

type DeviceMetricsChartData struct {
	DeviceID          string       `json:"device_id"`          // 设备ID
	DataType          string       `json:"data_type"`          // 设备数据类型
	Key               string       `json:"key"`                // 数据标识符
	AggregateWindow   *string      `json:"aggregate_window"`   // 聚合间隔
	AggregateFunction *string      `json:"aggregate_function"` // 聚合方法
	TimeRange         *string      `json:"time_range"`         // 时间范围
	Value             *interface{} `json:"value"`              // 最新值
	Timestamp         *int64       `json:"timestamp"`          // 最新值时间戳
	Points            *[]DataPoint `json:"points"`             // 数据点列表
}

type DataPoint struct {
	T int64   `json:"t"` // 时间戳
	V float64 `json:"v"` // 值
}

// 设备选择器请求
type DeviceSelectorReq struct {
	PageReq
	// 是否有设备模板
	HasDeviceConfig *bool `json:"has_device_config" form:"has_device_config" validate:"omitempty"`
}

// 设备选择器响应
type DeviceSelectorRes struct {
	DeviceID   string `json:"device_id"`   // 设备ID
	DeviceName string `json:"device_name"` // 设备名称
}
