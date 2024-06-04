package model

import "time"

type GetAlarmInfoListByPageReq struct {
	PageReq
	StartTime        *time.Time `json:"start_time" form:"start_time" validate:"omitempty"`               // 告警时间
	EndTime          *time.Time `json:"end_time" form:"end_time" validate:"omitempty"`                   // 告警时间
	AlarmLevel       *string    `json:"alarm_level" form:"alarm_level" validate:"omitempty"`             // 告警级别
	ProcessingResult *string    `json:"processing_result" form:"processing_result" validate:"omitempty"` // 处理结果
	TenantID         string     `json:"tenant_id" validate:"omitempty"`
}

type UpdateAlarmInfoReq struct {
	Id               string  `json:"id" validate:"required,max=36"`
	ProcessingResult *string `json:"processing_result" validate:"required"` // 处理结果
}

type UpdateAlarmInfoBatchReq struct {
	Id                     []string `json:"id" validate:"required"`
	ProcessingResult       *string  `json:"processing_result" validate:"required"`       // 处理结果
	ProcessingInstructions *string  `json:"processing_instructions" validate:"required"` // 处理结果
}

type GetAlarmHisttoryListByPage struct {
	PageReq
	StartTime   *time.Time `json:"start_time" form:"start_time" validate:"omitempty"`     // 告警时间
	EndTime     *time.Time `json:"end_time" form:"end_time" validate:"omitempty"`         // 告警时间
	AlarmStatus *string    `json:"alarm_status" form:"alarm_status" validate:"omitempty"` // 告警状态
	DeviceId    *string    `json:"device_id" form:"device_id" validate:"omitempty"`       // 设备id
}

type AlarmHistoryDescUpdateReq struct {
	AlarmHistoryId string `json:"id"  validate:"required"`         // 告警历史id
	Description    string `json:"description" validate:"required"` // 告警描述
}
type GetDeviceAlarmStatusReq struct {
	DeviceId string `json:"device_id" form:"device_id" validate:"required"` // 设备id
}
