package model

type GetCommandSetLogsListByPageReq struct {
	PageReq
	DeviceId      string  `json:"device_id" form:"device_id" validate:"required,max=36"`               // 设备ID
	Identify      *string `json:"identify" form:"identify" validate:"omitempty,max=36"`                //数据标识符
	Status        *string `json:"status" form:"status" validate:"omitempty,oneof=1 2"`                 //状态 1-发送成功 2-失败
	OperationType *string `json:"operation_type" form:"operation_type" validate:"omitempty,oneof=1 2"` //操作类型 1-手动操作 2-自动触发
}
