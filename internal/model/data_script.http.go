package model

import "time"

type CreateDataScriptReq struct {
	Name            string  `json:"name" validate:"required,max=99"`
	DeviceConfigId  string  `json:"device_config_id"  validate:"required,max=36"`
	Content         *string `json:"content" validate:"omitempty"`
	ScriptType      string  `json:"script_type" validate:"omitempty"`
	LastAnalogInput *string `json:"last_analog_input" validate:"omitempty"`
	Description     *string `json:"description" validate:"omitempty,max=255"`
	Remark          *string `json:"remark" validate:"omitempty,max=255"`
}

type UpdateDataScriptReq struct {
	Id              string     `json:"id" validate:"required,max=36"` // Id
	Name            string     `json:"name" validate:"required,max=99"`
	DeviceConfigId  string     `json:"device_config_id"  validate:"required,max=36"`
	Content         *string    `json:"content" validate:"omitempty"`
	ScriptType      string     `json:"script_type" validate:"required,oneof=A B C D E F H"` //  A-遥测上报预处理B-遥测下发预处理C-属性上报预处理D-属性下发预处理 E-命令下发预处理 F-事件上报预处理 H-事件下发预处理
	LastAnalogInput *string    `json:"last_analog_input" validate:"omitempty"`
	Description     *string    `json:"description" validate:"omitempty,max=255"`
	Remark          *string    `json:"remark" validate:"omitempty,max=255"`
	UpdatedAt       *time.Time `json:"updated_at" validate:"omitempty"`
}

type GetDataScriptListByPageReq struct {
	PageReq
	DeviceConfigId *string `json:"device_config_id" form:"device_config_id" validate:"required,max=36"`
	ScriptType     *string `json:"script_type" form:"script_type" validate:"omitempty"`
}

type QuizDataScriptReq struct {
	Content     string `json:"content" validate:"omitempty"`
	AnalogInput string `json:"last_analog_input" validate:"omitempty"`
	Topic       string `json:"topic" validate:"omitempty"`
}

type EnableDataScriptReq struct {
	Id         string `json:"id" validate:"required,max=36"`
	EnableFlag string `json:"enable_flag" validate:"required,oneof=Y N"`
}
