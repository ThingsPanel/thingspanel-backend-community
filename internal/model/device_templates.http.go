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
	Brand       *string `json:"brand" validate:"omitempty,max=255"`
	ModelNumber *string `json:"model_number" validate:"omitempty,max=255"`
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
	Brand          *string    `json:"brand" validate:"omitempty,max=255"`
	ModelNumber    *string    `json:"model_number" validate:"omitempty,max=255"`
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

// GetDeviceTemplateStatsReq 获取设备物模型统计请求
type GetDeviceTemplateStatsReq struct {
	DeviceTemplateID string `json:"device_template_id" form:"device_template_id" validate:"required,max=36"` // 物模型ID
}

// GetDeviceTemplateStatsRsp 获取设备物模型统计响应
type GetDeviceTemplateStatsRsp struct {
	DeviceTemplateID string `json:"device_template_id"` // 物模型ID
	Name             string `json:"name"`               // 物模型名称
	Label            string `json:"label"`              // 标签
	TotalDevices     int64  `json:"total_devices"`      // 关联设备总数
	OnlineDevices    int64  `json:"online_devices"`     // 在线设备数
}

// GetDeviceTemplateSelectorReq 获取设备物模型选择器请求
type GetDeviceTemplateSelectorReq struct {
	Name             *string `json:"name" form:"name" validate:"omitempty,max=255"`                            // 物模型名称（模糊匹配）
	Label            *string `json:"label" form:"label" validate:"omitempty,max=255"`                          // 标签（模糊匹配）
	DeviceTemplateID *string `json:"device_template_id" form:"device_template_id" validate:"omitempty,max=36"` // 物模型ID（精确匹配）
}

// GetDeviceTemplateSelectorRsp 获取设备物模型选择器响应
type GetDeviceTemplateSelectorRsp struct {
	ID    string  `json:"id"`    // 物模型ID
	Name  string  `json:"name"`  // 物模型名称
	Label *string `json:"label"` // 标签
}

// MarketLoginReq 市场登录请求
type MarketLoginReq struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

// MarketLoginRsp 市场登录响应
type MarketLoginRsp struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"` // 视市场服务返回而定
}

// PublishToMarketReq 发布到市场请求 (本地接口接收)
type PublishToMarketReq struct {
	DeviceTemplateID string `json:"device_template_id" validate:"required,max=36"`
	MarketToken      string `json:"market_token" validate:"required"` // 用户在市场的登录 token
	MarketName       string `json:"market_name"`
	Brand            string `json:"brand"`
	Model            string `json:"model"`
	Category         string `json:"category"`
	Version          string `json:"version"`
	Author           string `json:"author"`
	Description      string `json:"description"`
}

// PublishTemplateReq 发布模板到市场的业务契约对象 (对应 Task-01 契约)
type PublishTemplateReq struct {
	Name               string                 `json:"name"`
	Brand              string                 `json:"brand"`
	Model              string                 `json:"model"`
	Category           string                 `json:"category"`
	Author             string                 `json:"author"`
	Version            string                 `json:"version"`
	Description        string                 `json:"description"`
	TemplateDefinition map[string]interface{} `json:"template_definition"`
	DeviceModel        map[string]interface{} `json:"device_model"`
	PluginDependencies []PluginDependency     `json:"plugin_dependencies"`
}

// MarketPublishApiResponse 市场发布的标准响应
type MarketPublishApiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// PluginDependency 协议插件依赖声明
type PluginDependency struct {
	PluginName string `json:"plugin_name"`           // 插件名称，如 "modbus-protocol"
	PluginType string `json:"plugin_type"`           // 插件类型，如 "protocol"
	MinVersion string `json:"min_version,omitempty"` // 最低版本要求
	Required   bool   `json:"required"`              // 是否必须
}

// InstallFromMarketReq 从市场安装模板请求
type InstallFromMarketReq struct {
	MarketTemplateID string `json:"market_template_id" validate:"required"`
	Version          string `json:"version" validate:"omitempty"` // 不传则安装最新版
	MarketToken      string `json:"market_token" validate:"required"`
}

// MarketTemplateListReq 市场模板列表请求（代理转发用）
type MarketTemplateListReq struct {
	Keyword  *string `json:"keyword" form:"keyword"`
	Category *string `json:"category" form:"category"`
	SortBy   *string `json:"sort_by" form:"sort_by"` // latest | hottest
	Page     int     `json:"page" form:"page"`
	PageSize int     `json:"page_size" form:"page_size"`
}

// MarketTemplateFullData 市场模板完整数据（从市场下载的完整定义）
type MarketTemplateFullData struct {
	Name               string                 `json:"name"`
	Brand              string                 `json:"brand"`
	ModelNumber        string                 `json:"model_number"`
	Category           string                 `json:"category"`
	Author             string                 `json:"author"`
	VersionID          string                 `json:"version_id"`
	Version            string                 `json:"version"`
	Description        string                 `json:"description"`
	Telemetry          []DeviceModelTelemetry `json:"telemetry"`
	Attributes         []DeviceModelAttribute `json:"attributes"`
	Events             []DeviceModelEvent     `json:"events"`
	Commands           []DeviceModelCommand   `json:"commands"`
	PluginDependencies []PluginDependency     `json:"plugin_dependencies"`
}

// InstallFromMarketRsp 安装响应（含插件缺失警告）
type InstallFromMarketRsp struct {
	DeviceTemplate *DeviceTemplate    `json:"device_template"`
	MissingPlugins []PluginDependency `json:"missing_plugins,omitempty"`
}
