package model

import (
	"encoding/json"
	"time"
)

const (
	DashboardTemplateSourceMarket = "MARKET"
	DashboardTemplateSourceLocal  = "LOCAL"

	DashboardTemplateStatusReady    = "READY"
	DashboardTemplateStatusDisabled = "DISABLED"
)

// LocalDashboardTemplate is a reusable dashboard definition stored in ThingsPanel.
// It intentionally contains no real device IDs.
type LocalDashboardTemplate struct {
	ID                   string          `gorm:"column:id;primaryKey" json:"id"`
	TenantID             string          `gorm:"column:tenant_id;not null" json:"-"`
	Name                 string          `gorm:"column:name;not null" json:"name"`
	Description          string          `gorm:"column:description" json:"description,omitempty"`
	Version              string          `gorm:"column:version;not null" json:"version"`
	Source               string          `gorm:"column:source;not null" json:"source"`
	Status               string          `gorm:"column:status;not null" json:"status"`
	BundleKey            string          `gorm:"column:bundle_key" json:"bundleKey,omitempty"`
	BundleVersion        string          `gorm:"column:bundle_version" json:"bundleVersion,omitempty"`
	DashboardResourceKey string          `gorm:"column:dashboard_resource_key;not null" json:"dashboardResourceKey"`
	Thumbnail            string          `gorm:"column:thumbnail" json:"thumbnail,omitempty"`
	Snapshot             json.RawMessage `gorm:"column:snapshot;type:jsonb;not null" json:"-"`
	DownloadedAt         time.Time       `gorm:"column:downloaded_at;not null" json:"downloadedAt"`
	CreatedAt            time.Time       `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt            time.Time       `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (*LocalDashboardTemplate) TableName() string {
	return "local_dashboard_templates"
}

// LocalDashboardTemplateBinding maps a dashboard placeholder to an installed
// local device template. Runtime devices are selected only when creating an instance.
type LocalDashboardTemplateBinding struct {
	ID                      string    `gorm:"column:id;primaryKey" json:"-"`
	DashboardTemplateID     string    `gorm:"column:dashboard_template_id;not null" json:"-"`
	BindingKey              string    `gorm:"column:binding_key;not null" json:"bindingKey"`
	DisplayName             string    `gorm:"column:display_name;not null" json:"displayName"`
	Description             string    `gorm:"column:description" json:"description,omitempty"`
	Required                bool      `gorm:"column:required;not null" json:"required"`
	AllowMany               bool      `gorm:"column:allow_many;not null" json:"-"`
	DeviceTemplateKey       string    `gorm:"column:device_template_key;not null" json:"-"`
	LocalDeviceTemplateID   string    `gorm:"column:local_device_template_id;not null" json:"localDeviceTemplateId"`
	LocalDeviceTemplateName string    `gorm:"column:local_device_template_name;not null" json:"localDeviceTemplateName"`
	CreatedAt               time.Time `gorm:"column:created_at;not null" json:"-"`
	UpdatedAt               time.Time `gorm:"column:updated_at;not null" json:"-"`
}

func (*LocalDashboardTemplateBinding) TableName() string {
	return "local_dashboard_template_bindings"
}

// LocalDashboardTemplateInstance records a concrete ThingsVis dashboard created
// from a reusable local template.
type LocalDashboardTemplateInstance struct {
	ID                  string          `gorm:"column:id;primaryKey" json:"id"`
	DashboardTemplateID string          `gorm:"column:dashboard_template_id;not null" json:"dashboardTemplateId"`
	TenantID            string          `gorm:"column:tenant_id;not null" json:"-"`
	DashboardID         string          `gorm:"column:dashboard_id;not null" json:"dashboardId"`
	ProjectID           string          `gorm:"column:project_id" json:"projectId"`
	Name                string          `gorm:"column:name;not null" json:"name"`
	DeviceBindings      json.RawMessage `gorm:"column:device_bindings;type:jsonb;not null" json:"deviceBindings"`
	CreatedAt           time.Time       `gorm:"column:created_at;not null" json:"createdAt"`
}

func (*LocalDashboardTemplateInstance) TableName() string {
	return "local_dashboard_template_instances"
}

type DashboardTemplateSnapshot struct {
	Name          string          `json:"name"`
	SchemaVersion string          `json:"schemaVersion"`
	CanvasConfig  json.RawMessage `json:"canvasConfig"`
	Nodes         json.RawMessage `json:"nodes"`
	DataSources   json.RawMessage `json:"dataSources"`
	Variables     json.RawMessage `json:"variables"`
	FieldBindings []FieldBinding  `json:"fieldBindings,omitempty"`
}

type DownloadDashboardTemplateRequest struct {
	BundleKey   string `json:"bundleKey" binding:"required"`
	Version     string `json:"version" binding:"required"`
	MarketToken string `json:"marketToken" binding:"required"`
}

type DownloadDashboardTemplateResponse struct {
	TemplateIDs []string `json:"templateIds"`
	Downloaded  int      `json:"downloaded"`
	Reused      int      `json:"reused"`
}

type ListLocalDashboardTemplatesRequest struct {
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
	Keyword  string `form:"keyword"`
	Source   string `form:"source"`
	Status   string `form:"status"`
}

type LocalDashboardTemplateResponse struct {
	ID                    string                           `json:"id"`
	Name                  string                           `json:"name"`
	Description           string                           `json:"description,omitempty"`
	Version               string                           `json:"version"`
	Source                string                           `json:"source"`
	Status                string                           `json:"status"`
	BundleKey             string                           `json:"bundleKey,omitempty"`
	BundleVersion         string                           `json:"bundleVersion,omitempty"`
	DashboardResourceKey  string                           `json:"dashboardResourceKey"`
	Thumbnail             string                           `json:"thumbnail,omitempty"`
	Bindings              []*LocalDashboardTemplateBinding `json:"bindings"`
	CompatibleDeviceCount int64                            `json:"compatibleDeviceCount"`
	InstanceCount         int64                            `json:"instanceCount"`
	DownloadedAt          time.Time                        `json:"downloadedAt"`
	UpdatedAt             time.Time                        `json:"updatedAt"`
}

type ListLocalDashboardTemplatesResponse struct {
	List  []*LocalDashboardTemplateResponse `json:"list"`
	Total int64                             `json:"total"`
}

type CompatibleDevice struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	DeviceNumber       string `json:"deviceNumber,omitempty"`
	DeviceTemplateID   string `json:"deviceTemplateId"`
	DeviceTemplateName string `json:"deviceTemplateName"`
	Online             bool   `json:"online"`
}

type CompatibleDeviceBinding struct {
	BindingKey              string              `json:"bindingKey"`
	DisplayName             string              `json:"displayName"`
	Required                bool                `json:"required"`
	LocalDeviceTemplateID   string              `json:"localDeviceTemplateId"`
	LocalDeviceTemplateName string              `json:"localDeviceTemplateName"`
	Devices                 []*CompatibleDevice `json:"devices"`
}

type CompatibleDevicesResponse struct {
	Bindings []*CompatibleDeviceBinding `json:"bindings"`
}

type CreateDashboardTemplateInstanceRequest struct {
	Name           string               `json:"name" binding:"required,max=100"`
	DeviceBindings []DeviceBindingInput `json:"deviceBindings"`
}

type CreateDashboardTemplateInstanceResponse struct {
	DashboardID string `json:"dashboardId"`
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
}
