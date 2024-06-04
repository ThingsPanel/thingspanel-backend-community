package model

import (
	"time"
)

type CreateDeviceTemplateReq struct {
	Name        string  `json:"name" validate:"required,max=255"`
	Author      *string `json:"author" validate:"omitempty,max=99"`
	Version     *string `json:"version" validate:"omitempty,max=36"`
	Description *string `json:"description" validate:"omitempty,max=500"`
	Remark      *string `json:"remark" validate:"omitempty,max=255"`
	Path        *string `json:"path" validate:"omitempty,max=255"`
	Label       *string `json:"label" validate:"omitempty,max=255"`
}

type UpdateDeviceTemplateReq struct {
	Id             string     `json:"id" validate:"required,max=36"`
	Name           *string    `json:"name" validate:"omitempty,max=255"`
	Author         *string    `json:"author" validate:"omitempty,max=99"`
	Version        *string    `json:"version" validate:"omitempty,max=36"`
	Description    *string    `json:"description" validate:"omitempty,max=500"`
	Remark         *string    `json:"remark" validate:"omitempty,max=255"`
	Path           *string    `json:"path" validate:"omitempty,max=255"`
	Label          *string    `json:"label" validate:"omitempty,max=255"`
	WebChartConfig *string    `json:"web_chart_config" validate:"omitempty"`
	AppChartConfig *string    `json:"app_chart_config" validate:"omitempty"`
	UpdatedAt      *time.Time `json:"updated_at" validate:"omitempty"`
}

type GetDeviceTemplateListByPageReq struct {
	PageReq
	Name *string `json:"name" form:"name" validate:"omitempty,max=255"`
}
type GetDeviceTemplateMenuReq struct {
	Name *string `json:"name" form:"name" validate:"omitempty,max=255"`
}

type GetDeviceTemplateRsp struct {
	Id           string                 `json:"id"`
	Name         string                 `json:"name"`
	TemplateType int16                  `json:"template_type"`
	Author       string                 `json:"author"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description"`
	TenantId     string                 `json:"tenant_id"`
	Data         map[string]interface{} `json:"data"`
	PublicFlag   int16                  `json:"public_flag"`
	Remark       string                 `json:"remark"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}
