package model

import "time"

type CreateSceneReq struct {
	Name        string            `json:"name" validate:"required,max=36"`
	Description string            `json:"description"`
	Actions     []SceneActionsReq `json:"actions" validate:"required"`
}

type SceneActionsReq struct {
	ActionType      string  `json:"action_type" validate:"required,oneof=10 11 30"`
	ActionTarget    string  `json:"action_target" validate:"required"`
	ActionParamType *string `json:"action_param_type" validate:"omitempty"`
	ActionParam     *string `json:"action_param" validate:"omitempty"`
	ActionValue     *string `json:"action_value" validate:"omitempty"`
	Remark          *string `json:"remark" validate:"omitempty"`
}

type UpdateSceneReq struct {
	ID          string            `json:"id" validate:"required,max=36"`
	Name        string            `json:"name" validate:"required,max=36"`
	Description string            `json:"description"`
	Actions     []SceneActionsReq `json:"actions" validate:"required"`
}

type GetSceneListByPageReq struct {
	PageReq
	Name *string `json:"name" form:"name" validate:"omitempty"`
}

type GetSceneLogListByPageReq struct {
	PageReq
	ID                 string     `json:"id" form:"id" validate:"required,max=36"`
	ExecutionResult    *string    `json:"execution_result" form:"execution_result" validate:"omitempty"`
	ExecutionStartTime *time.Time `json:"execution_start_time" form:"execution_start_time" validate:"omitempty"`
	ExecutionEndTime   *time.Time `json:"execution_end_time" form:"execution_end_time" validate:"omitempty"`
}
