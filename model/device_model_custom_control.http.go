package model

type CreateDeviceModelCustomControlReq struct {
	DeviceTemplateId string  `json:"device_template_id" validate:"required,max=36"` // 设备模版ID
	Name             string  `json:"name" validate:"required,max=36"`               // 名称
	ControlType      string  `json:"control_type" validate:"required,max=50"`       // 控制类型
	Description      *string `json:"description" validate:"omitempty,max=500"`      // 描述
	Content          *string `json:"content" validate:"omitempty"`                  // 指令内容
	EnableStatus     string  `json:"enable_status" validate:"required,max=10"`      // 启用状态
	Remark           *string `json:"remark" validate:"omitempty,max=255"`           // 备注
}

type UpdateDeviceModelCustomControlReq struct {
	ID               string  `json:"id" validate:"required,max=36"`                  // ID
	DeviceTemplateId *string `json:"device_template_id" validate:"omitempty,max=36"` // 设备模版ID
	Name             *string `json:"name" validate:"omitempty,max=36"`               // 名称
	ControlType      *string `json:"control_type" validate:"omitempty,max=50"`       // 控制类型
	Description      *string `json:"description" validate:"omitempty,max=500"`       // 描述
	Content          *string `json:"content" validate:"omitempty"`                   // 指令内容
	EnableStatus     *string `json:"enable_status" validate:"omitempty,max=10"`      // 启用状态
	Remark           *string `json:"remark" validate:"omitempty,max=255"`            // 备注
}
