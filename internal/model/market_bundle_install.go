package model

import (
	"encoding/json"
	"time"
)

// Install status constants (state machine states)
const (
	InstallStateDownloading         = "DOWNLOADING"
	InstallStateDownloaded          = "DOWNLOADED"
	InstallStateVerified            = "VERIFIED"
	InstallStateModelsInstalled     = "MODELS_INSTALLED"
	InstallStateDashboardsCreated   = "DASHBOARDS_CREATED"
	InstallStateWaitingForBindings  = "WAITING_FOR_BINDINGS"
	InstallStateCompleted           = "COMPLETED"
	InstallStateFailed              = "FAILED"
	InstallStateCompensationRequired = "COMPENSATION_REQUIRED"
)

// Resource type constants
const (
	ResourceTypeDeviceTemplate = "device_template"
	ResourceTypeDashboard     = "dashboard"
	ResourceTypeDeviceConfig  = "device_config"
)

// Binding status constants
const (
	BindingStatusPending = "pending"
	BindingStatusBound   = "bound"
	BindingStatusUnbound = "unbound"
	BindingStatusFailed  = "failed"
)

// MarketBundleInstallation represents local installation state
type MarketBundleInstallation struct {
	ID               string          `json:"id"`
	IdempotencyKey   string          `json:"idempotencyKey"`
	BundleKey        string          `json:"bundleKey"`
	BundleVersion    string          `json:"bundleVersion"`
	TenantID         string          `json:"tenantId"`
	Status           string          `json:"status"`
	ErrorCode        string          `json:"errorCode,omitempty"`
	ErrorMessage     string          `json:"errorMessage,omitempty"`
	Warnings         json.RawMessage `json:"warnings,omitempty"`
	DownloadedAt     *time.Time      `json:"downloadedAt,omitempty"`
	VerifiedAt       *time.Time      `json:"verifiedAt,omitempty"`
	ModelsInstalledAt *time.Time     `json:"modelsInstalledAt,omitempty"`
	DashboardsCreatedAt *time.Time   `json:"dashboardsCreatedAt,omitempty"`
	CompletedAt      *time.Time      `json:"completedAt,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

// MarketResourceMapping represents a mapping from market resource key to local ID
type MarketResourceMapping struct {
	ID               string          `json:"id"`
	InstallationID   string          `json:"installationId"`
	TenantID         string          `json:"tenantId"`
	ResourceType     string          `json:"resourceType"`
	MarketResourceKey string         `json:"marketResourceKey"`
	MarketVersion    string          `json:"marketVersion"`
	LocalID          string          `json:"localId"`
	LocalName        string          `json:"localName,omitempty"`
	Status           string          `json:"status"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

// MarketBundleBindingStatus tracks device binding status for dashboards
type MarketBundleBindingStatus struct {
	ID               string     `json:"id"`
	InstallationID   string     `json:"installationId"`
	BindingKey       string     `json:"bindingKey"`
	DeviceTemplateKey string    `json:"deviceTemplateKey"`
	Required         bool       `json:"required"`
	LocalDeviceID    string     `json:"localDeviceId,omitempty"`
	BoundAt          *time.Time `json:"boundAt,omitempty"`
	Status           string     `json:"status"`
	ErrorMessage     string     `json:"errorMessage,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

// MarketInstallationAudit represents audit trail entry
type MarketInstallationAudit struct {
	ID            string          `json:"id"`
	InstallationID string         `json:"installationId"`
	TenantID      string          `json:"tenantId"`
	Action        string          `json:"action"`
	PrevState     string          `json:"prevState,omitempty"`
	NewState      string          `json:"newState,omitempty"`
	ResourceType  string          `json:"resourceType,omitempty"`
	ResourceKey   string          `json:"resourceKey,omitempty"`
	LocalID       string          `json:"localId,omitempty"`
	Details       json.RawMessage `json:"details,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
}

// InstallBundleRequest is the request to install a market bundle locally
type InstallBundleRequest struct {
	BundleKey    string                          `json:"bundleKey" binding:"required"`
	Version      string                          `json:"version" binding:"required"`
	IdempotencyKey string                        `json:"idempotencyKey,omitempty"`
	DeviceBindings []DeviceBindingInput          `json:"deviceBindings,omitempty"`
	MarketToken  string                          `json:"marketToken" binding:"required"`
	OverwritePolicy string                       `json:"overwritePolicy,omitempty"` // "default" (no overwrite), "upgrade"
}

// DeviceBindingInput represents a device binding selection from the user
type DeviceBindingInput struct {
	BindingKey    string `json:"bindingKey" binding:"required"`
	LocalDeviceID string `json:"localDeviceId" binding:"required"`
}

// InstallBundleResponse is the response after initiating an installation
type InstallBundleResponse struct {
	InstallationID   string                     `json:"installationId"`
	BundleKey        string                    `json:"bundleKey"`
	Version          string                    `json:"version"`
	Status           string                    `json:"status"`
	ResourceMappings []*ResourceMappingResponse `json:"resourceMappings,omitempty"`
	BindingStatus    []*BindingStatusResponse  `json:"bindingStatus,omitempty"`
	Warnings         []string                  `json:"warnings,omitempty"`
	Errors           []string                  `json:"errors,omitempty"`
	IsIdempotent     bool                      `json:"isIdempotent"`
	ExistingInstallID string                   `json:"existingInstallId,omitempty"`
}

// ResourceMappingResponse represents a resource mapping in the response
type ResourceMappingResponse struct {
	ResourceType     string `json:"resourceType"`
	MarketResourceKey string `json:"marketResourceKey"`
	LocalID           string `json:"localId"`
	LocalName         string `json:"localName,omitempty"`
	Status            string `json:"status"`
}

// BindingStatusResponse represents a binding status in the response
type BindingStatusResponse struct {
	BindingKey      string `json:"bindingKey"`
	DeviceTemplateKey string `json:"deviceTemplateKey"`
	Required        bool   `json:"required"`
	LocalDeviceID   string `json:"localDeviceId,omitempty"`
	Status          string `json:"status"`
	ErrorMessage    string `json:"errorMessage,omitempty"`
}

// GetInstallationStatusRequest for querying installation status
type GetInstallationStatusRequest struct {
	InstallationID string `json:"installationId" form:"installationId" binding:"required"`
}

// ListInstallationsRequest for listing tenant installations
type ListInstallationsRequest struct {
	BundleKey string `json:"bundleKey" form:"bundleKey,omitempty"`
	Status    string `json:"status" form:"status,omitempty"`
	Page      int    `json:"page" form:"page,default=1"`
	PageSize  int    `json:"pageSize" form:"pageSize,default=20"`
}

// ListInstallationsResponse
type ListInstallationsResponse struct {
	Data     []*MarketBundleInstallation `json:"data"`
	Total    int                        `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"pageSize"`
}

// UpdateBindingRequest for updating device bindings
type UpdateBindingRequest struct {
	BindingKey    string `json:"bindingKey" binding:"required"`
	LocalDeviceID string `json:"localDeviceId" binding:"required"`
}

// RetryInstallationRequest for retrying a failed installation
type RetryInstallationRequest struct {
	InstallationID string `json:"installationId" binding:"required"`
	DeviceBindings []DeviceBindingInput `json:"deviceBindings,omitempty"`
}

// CompensateRequest for triggering compensation on failed installation
type CompensateRequest struct {
	InstallationID string `json:"installationId" binding:"required"`
}

// HorizonBundleDownload represents the downloaded bundle from Horizon
type HorizonBundleDownload struct {
	BundleFileBytes []byte          `json:"bundleFileBytes"`
	ContentHash     string          `json:"contentHash"`
	ContractVersion string          `json:"contractVersion"`
	BundleKind      string          `json:"bundleKind"`
	Metadata        json.RawMessage `json:"metadata"`
	Compatibility   json.RawMessage `json:"compatibility"`
	Resources       json.RawMessage `json:"resources"`
	Security        json.RawMessage `json:"security"`
}

// HorizonDownloadResponse is the response from Horizon download API
type HorizonDownloadResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// InstallBundleMetadata holds metadata for installation tracking
type InstallBundleMetadata struct {
	SourceIP       string   `json:"sourceIp,omitempty"`
	UserAgent      string   `json:"userAgent,omitempty"`
	OverwritePolicy string  `json:"overwritePolicy,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}
