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
