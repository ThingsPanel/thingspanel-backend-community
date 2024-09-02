package model

import "time"

type GetSceneAutomationLogReq struct {
	PageReq
	SceneAutomationId  string     `json:"scene_automation_id" form:"scene_automation_id" validate:"required,max=36"`
	ExecutionResult    *string    `json:"execution_result" form:"execution_result" validate:"omitempty"`
	ExecutionStartTime *time.Time `json:"execution_start_time" form:"execution_start_time" validate:"omitempty"`
	ExecutionEndTime   *time.Time `json:"execution_end_time" form:"execution_end_time" validate:"omitempty"`
}

type GetSceneByDeviceIdWhitDeviceConfigIdReq struct {
	PageReq
	DeviceId       string `json:"device_id" form:"device_id"`
	DeviceConfigId string `json:"device_config_id" form:"device_config_id"`
}
