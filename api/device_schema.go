package api

import "time"

type DeviceTemplateReadSchema struct {
	ID                string    `json:"id"`          // Id
	Name              string    `json:"name"`        // 模板名称
	Author            *string   `json:"author"`      // 作者
	Version           *string   `json:"version"`     // 版本号
	Description       *string   `json:"description"` // 描述
	TenantID          string    `json:"tenant_id"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Flag              *int16    `json:"flag" example:"1"`    // 标志 默认1
	Label             *string   `json:"label"`               // 标签
	DeviceModelConfig *string   `json:"device_model_config"` // 物模型配置
	WebChartConfig    *string   `json:"web_chart_config"`    // web图表配置
	AppChartConfig    *string   `json:"app_chart_config"`    // app图表配置
}

type GetDeviceTemplateListResponse struct {
	Code    int                       `json:"code" example:"200"`
	Message string                    `json:"message" example:"success"`
	Data    GetDeviceTemplateListData `json:"data"`
}

type GetDeviceTemplateListData struct {
	Total int64                      `json:"total"`
	List  []DeviceTemplateReadSchema `json:"list"`
}

type GetDeviceTemplateResponse struct {
	Code    int                      `json:"code" example:"200"`
	Message string                   `json:"message" example:"success"`
	Data    DeviceTemplateReadSchema `json:"data"`
}
