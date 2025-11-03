package model

// GetDeviceStatusHistoryReq 获取设备状态历史请求
type GetDeviceStatusHistoryReq struct {
	PageReq
	DeviceID  string `json:"device_id" form:"device_id" validate:"required,max=36"` // 设备ID
	StartTime *int64 `json:"start_time" form:"start_time" validate:"omitempty"`     // 开始时间戳（秒）
	EndTime   *int64 `json:"end_time" form:"end_time" validate:"omitempty"`         // 结束时间戳（秒）
	Status    *int16 `json:"status" form:"status" validate:"omitempty,oneof=0 1"`   // 状态筛选：0-离线，1-在线
}
