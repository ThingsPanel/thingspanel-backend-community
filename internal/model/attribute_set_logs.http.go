package model

type GetAttributeSetLogsListByPageReq struct {
	PageReq
	DeviceId      string  `json:"device_id" form:"device_id" validate:"required,max=36"`               // 设备ID
	Status        *string `json:"status" form:"status" validate:"omitempty,oneof=1 2 3 4"`             //状态 1-发送成功 2- 发送失败3-返回成功 4-返回失败
	OperationType *string `json:"operation_type" form:"operation_type" validate:"omitempty,oneof=1 2"` //操作类型 1-手动操作 2-自动触发
}

type AttributePutMessage struct {
	DeviceID string `json:"device_id" form:"device_id" validate:"required,max=36"`
	Value    string `json:"value" form:"value" validate:"required"`
}

// 发送
type AttributeGetMessageReq struct {
	DeviceID string   `json:"device_id" form:"device_id" validate:"required,max=36"`
	Keys     []string `json:"keys" form:"keys" validate:"max=9999"`
}
