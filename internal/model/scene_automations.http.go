package model

import "time"

type CreateSceneAutomationReq struct {
	Name                   string        `json:"name" validate:"required,max=36"`
	Description            string        `json:"description"`
	Enabled                string        `json:"enabled" validate:"omitempty,oneof=Y N"`
	TriggerConditionGroups [][]Condition `json:"trigger_condition_groups" validate:"required"`
	Actions                []Action      `json:"actions" validate:"required"`
	Remark                 string        `json:"remark" `
}

type UpdateSceneAutomationReq struct {
	ID                     string        `json:"id" validate:"required,max=36"`
	Name                   string        `json:"name" validate:"required,max=36"`
	Description            string        `json:"description"`
	Enabled                string        `json:"enabled" validate:"required,oneof=Y N"`
	TriggerConditionGroups [][]Condition `json:"trigger_condition_groups" validate:"required"`
	Actions                []Action      `json:"actions" validate:"required"`
	Remark                 string        `json:"remark" `
}

type Condition struct {
	TriggerConditionsType string     `json:"trigger_conditions_type" validate:"required"`
	TriggerSource         *string    `json:"trigger_source" validate:"omitempty"`
	TriggerParamType      *string    `json:"trigger_param_type" validate:"omitempty"`
	TriggerParam          *string    `json:"trigger_param" validate:"omitempty"`
	TriggerOperator       *string    `json:"trigger_operator" validate:"omitempty"`
	TriggerValue          *string    `json:"trigger_value" validate:"omitempty"`
	ExecutionTime         *time.Time `json:"execution_time" validate:"omitempty"`
	ExpirationTime        *int       `json:"expiration_time" validate:"omitempty"`
	TaskType              *string    `json:"task_type" validate:"omitempty"`
	Params                *string    `json:"params" validate:"omitempty"`
}

type Action struct {
	ActionType      string `json:"action_type" validate:"omitempty"`
	ActionTarget    string `json:"action_target" validate:"omitempty"`
	ActionParamType string `json:"action_param_type" validate:"omitempty"`
	ActionParam     string `json:"action_param" validate:"omitempty"`
	ActionValue     string `json:"action_value" validate:"omitempty"`
}

type GetSceneAutomationByPageReq struct {
	Name           *string `json:"name" form:"name" validate:"omitempty"`
	DeviceId       *string `json:"device_id"  form:"device_id"  validate:"omitempty"`
	DeviceConfigId *string `json:"device_config_id"  form:"device_config_id"  validate:"omitempty`
	PageReq
}

type GetSceneAutomationsWithAlarmByPageReq struct {
	PageReq
	DeviceId       *string `json:"device_id"  form:"device_id"  validate:"omitempty"`
	DeviceConfigId *string `json:"device_config_id" form:"device_config_id" validata:"omitempty"`
}
