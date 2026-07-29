package model

import "encoding/json"

// ThingModelFieldKind defines the kind of thing model field
type ThingModelFieldKind string

const (
	ThingModelFieldKindTelemetry ThingModelFieldKind = "telemetry"
	ThingModelFieldKindAttribute ThingModelFieldKind = "attribute"
	ThingModelFieldKindCommand   ThingModelFieldKind = "command"
	ThingModelFieldKindEvent     ThingModelFieldKind = "event"
)

// ThingModelField represents a field in the thing model
type ThingModelField struct {
	Kind        ThingModelFieldKind `json:"kind"`
	Identifier  string              `json:"identifier"`
	Name        string              `json:"name"`
	DataType    string              `json:"dataType"`
	Unit        string              `json:"unit,omitempty"`
	Description string              `json:"description,omitempty"`
	AccessMode  string              `json:"accessMode,omitempty"`
}

// BundleDeviceTemplate represents a device template in the bundle
type BundleDeviceTemplate struct {
	ResourceKey string            `json:"resourceKey"`
	Version     string            `json:"version"`
	Name        string            `json:"name"`
	Protocol    *ProtocolInfo     `json:"protocol,omitempty"`
	ThingModel  []ThingModelField `json:"thingModel"`
}

// ProtocolInfo represents sanitized protocol information
type ProtocolInfo struct {
	ProtocolType   string                 `json:"protocolType"`
	PublicDefaults map[string]interface{} `json:"publicDefaults,omitempty"`
}

// DeviceBinding represents a binding from dashboard to device template
type DeviceBinding struct {
	BindingKey        string `json:"bindingKey"`
	DeviceTemplateKey string `json:"deviceTemplateKey"`
	Required          bool   `json:"required"`
	AllowMany         bool   `json:"allowMany,omitempty"`
	DisplayName       string `json:"displayName,omitempty"`
}

// FieldBinding represents a field binding from dashboard to thing model
type FieldBinding struct {
	BindingKey string              `json:"bindingKey"`
	Kind       ThingModelFieldKind `json:"kind"`
	Identifier string              `json:"identifier"`
	Required   bool                `json:"required,omitempty"`
}

// DashboardTemplate represents a dashboard template in the bundle
type DashboardTemplate struct {
	ResourceKey    string          `json:"resourceKey"`
	Version        string          `json:"version"`
	Name           string          `json:"name"`
	SchemaVersion  string          `json:"schemaVersion"`
	CanvasConfig   json.RawMessage `json:"canvasConfig"`
	Nodes          json.RawMessage `json:"nodes"`
	DataSources    json.RawMessage `json:"dataSources"`
	Variables      json.RawMessage `json:"variables"`
	DeviceBindings []DeviceBinding `json:"deviceBindings"`
	FieldBindings  []FieldBinding  `json:"fieldBindings,omitempty"`
}

// BundleResources represents the resources section of a bundle
type BundleResources struct {
	DeviceTemplates []BundleDeviceTemplate `json:"deviceTemplates"`
	Dashboards      []DashboardTemplate    `json:"dashboards"`
}

// BundleSecurity represents the security section of a bundle
type BundleSecurity struct {
	ContainsSecrets     bool   `json:"containsSecrets"`
	ContainsRuntimeData bool   `json:"containsRuntimeData"`
	ContentHash         string `json:"contentHash,omitempty"`
	Signature           string `json:"signature,omitempty"`
}

// BundlePluginDependency represents a plugin dependency in the bundle
type BundlePluginDependency struct {
	Identifier string `json:"identifier"`           // Plugin identifier, e.g. "modbus-protocol"
	MinVersion string `json:"minVersion,omitempty"` // Minimum version required
}

// CompatibilityInfo represents platform compatibility info
type CompatibilityInfo struct {
	MinThingsPanel string                   `json:"minThingsPanel,omitempty"`
	MinThingsVis   string                   `json:"minThingsVis,omitempty"`
	Plugins        []BundlePluginDependency `json:"plugins,omitempty"`
}

// BundleMetadata represents the metadata section of a bundle
type BundleMetadata struct {
	Name          string `json:"name"`
	Category      string `json:"category"`
	Description   string `json:"description"`
	Brand         string `json:"brand,omitempty"`
	Author        string `json:"author,omitempty"`
	CoverAssetKey string `json:"coverAssetKey,omitempty"`
}

// PublishDraftRequest is the request to publish a draft bundle
type PublishDraftRequest struct {
	// Local resource IDs
	DeviceTemplateIDs []string `json:"deviceTemplateIds" binding:"required,min=1"`
	DashboardIDs      []string `json:"dashboardIds" binding:"required,min=1"`

	// Bundle metadata
	BundleKey   string `json:"bundleKey" binding:"required,min=3,max=63"`
	Version     string `json:"version" binding:"required"`
	MarketToken string `json:"marketToken" binding:"required"`

	// Optional overrides
	Category      string `json:"category,omitempty"`
	Brand         string `json:"brand,omitempty"`
	Description   string `json:"description,omitempty"`
	CoverAssetKey string `json:"coverAssetKey,omitempty"`

	// Compatibility constraints
	MinThingsPanel string `json:"minThingsPanel,omitempty"`
	MinThingsVis   string `json:"minThingsVis,omitempty"`
}

// AnalyzeDashboardBundleRequest requests device-role discovery for one dashboard.
type AnalyzeDashboardBundleRequest struct {
	DashboardID string `json:"dashboardId" validate:"required"`
}

// DashboardBundleDeviceReference is enriched with the local device template dependency.
type DashboardBundleDeviceReference struct {
	SourceDeviceID      string            `json:"sourceDeviceId"`
	SourceDeviceName    string            `json:"sourceDeviceName,omitempty"`
	DeviceTemplateID    string            `json:"deviceTemplateId"`
	SuggestedBindingKey string            `json:"suggestedBindingKey"`
	RequiredFields      []ThingModelField `json:"requiredFields"`
}

// AnalyzeDashboardBundleResponse is shown before the publisher confirms role names.
type AnalyzeDashboardBundleResponse struct {
	DashboardID      string                           `json:"dashboardId"`
	DashboardName    string                           `json:"dashboardName"`
	DeviceReferences []DashboardBundleDeviceReference `json:"deviceReferences"`
}

// DashboardBundleRole confirms one publisher device as a stable install-time role.
type DashboardBundleRole struct {
	SourceDeviceID string `json:"sourceDeviceId" validate:"required"`
	BindingKey     string `json:"bindingKey" validate:"required"`
	DisplayName    string `json:"displayName" validate:"required"`
}

// PublishDashboardBundleRequest publishes exactly one dashboard product.
type PublishDashboardBundleRequest struct {
	DashboardID   string                `json:"dashboardId" validate:"required"`
	BundleKey     string                `json:"bundleKey" validate:"required"`
	Version       string                `json:"version" validate:"required"`
	Name          string                `json:"name" validate:"required"`
	Category      string                `json:"category" validate:"required"`
	Description   string                `json:"description"`
	CoverAssetKey string                `json:"coverAssetKey,omitempty"`
	MarketToken   string                `json:"marketToken" validate:"required"`
	DeviceRoles   []DashboardBundleRole `json:"deviceRoles" validate:"required,min=1,dive"`
}

// PublishDraftPrecheckReport is the pre-check report returned before publishing
type PublishDraftPrecheckReport struct {
	Passed      bool                 `json:"passed"`
	Errors      []PrecheckError      `json:"errors,omitempty"`
	Warnings    []PrecheckWarning    `json:"warnings,omitempty"`
	Suggestions []PrecheckSuggestion `json:"suggestions,omitempty"`
}

// PrecheckError represents a blocking error during precheck
type PrecheckError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	Details string `json:"details,omitempty"`
}

// PrecheckWarning represents a non-blocking warning during precheck
type PrecheckWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// PrecheckSuggestion represents a suggestion for improvement
type PrecheckSuggestion struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// PublishDraftResponse is the response after publishing a draft
type PublishDraftResponse struct {
	BundleKey      string                      `json:"bundleKey"`
	Version        string                      `json:"version"`
	ContentHash    string                      `json:"contentHash"`
	Status         string                      `json:"status"`
	PrecheckReport *PublishDraftPrecheckReport `json:"precheckReport,omitempty"`
}

// HorizonPublishRequest is the request sent to Horizon's market API
type HorizonPublishRequest struct {
	ContractVersion string          `json:"contractVersion"`
	BundleKind      string          `json:"bundleKind"`
	BundleKey       string          `json:"bundleKey"`
	Version         string          `json:"version"`
	Metadata        json.RawMessage `json:"metadata"`
	Compatibility   json.RawMessage `json:"compatibility,omitempty"`
	Resources       json.RawMessage `json:"resources"`
	Security        json.RawMessage `json:"security"`
}

// HorizonPublishResponse is the response from Horizon's market API
type HorizonPublishResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// LocalDeviceTemplate represents a local device template with tenant ownership
type LocalDeviceTemplate struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Brand       string `json:"brand,omitempty"`
	Description string `json:"description,omitempty"`
	Label       string `json:"label,omitempty"`
}

// LocalDashboard represents a local dashboard with tenant ownership
type LocalDashboard struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Name        string `json:"name"`
	JsonData    string `json:"jsonData,omitempty"`
	Description string `json:"description,omitempty"`
}

// PrecheckErrorCode constants
const (
	ErrCodeResourceNotFound       = "RESOURCE_NOT_FOUND"
	ErrCodeResourceForbidden      = "RESOURCE_FORBIDDEN"
	ErrCodeCrossTenantAccess      = "CROSS_TENANT_ACCESS"
	ErrCodeInvalidFieldBinding    = "INVALID_FIELD_BINDING"
	ErrCodeSecretDetected         = "SECRET_DETECTED"
	ErrCodeRealDeviceIDDetected   = "REAL_DEVICE_ID_DETECTED"
	ErrCodeRealTenantIDDetected   = "REAL_TENANT_ID_DETECTED"
	ErrCodeRealUserIDDetected     = "REAL_USER_ID_DETECTED"
	ErrCodeThingModelReadFailed   = "THING_MODEL_READ_FAILED"
	ErrCodeDashboardExportFailed  = "DASHBOARD_EXPORT_FAILED"
	ErrCodeHorizonPublishFailed   = "HORIZON_PUBLISH_FAILED"
	ErrCodeVersionConflict        = "VERSION_CONFLICT"
	ErrCodeIdempotencyConflict    = "IDEMPOTENCY_CONFLICT"
	ErrCodeInvalidBundleKey       = "INVALID_BUNDLE_KEY"
	ErrCodeInvalidVersion         = "INVALID_VERSION"
	ErrCodeEmptyDeviceTemplates   = "EMPTY_DEVICE_TEMPLATES"
	ErrCodeEmptyDashboards        = "EMPTY_DASHBOARDS"
	ErrCodeDeviceTemplateNotFound = "DEVICE_TEMPLATE_NOT_FOUND"
	ErrCodeDashboardNotFound      = "DASHBOARD_NOT_FOUND"
)

// PrecheckWarningCode constants
const (
	WarnCodeMissingDescription   = "MISSING_DESCRIPTION"
	WarnCodeMissingCategory      = "MISSING_CATEGORY"
	WarnCodeMissingBrand         = "MISSING_BRAND"
	WarnCodeNoTelemetryFields    = "NO_TELEMETRY_FIELDS"
	WarnCodeNoAttributeFields    = "NO_ATTRIBUTE_FIELDS"
	WarnCodeOptionalFieldBinding = "OPTIONAL_FIELD_BINDING"
)

// ProtocolConfigAllowlist defines allowed fields in protocol config (no secrets)
var ProtocolConfigAllowlist = map[string]bool{
	"protocolType": true,
	"transport":    true,
	"qos":          true,
	"keepAlive":    true,
	"timeout":      true,
	"retryCount":   true,
	"encoding":     true,
	"compress":     true,
}

// SecretFieldPatterns defines patterns that indicate secrets
var SecretFieldPatterns = []string{
	"password", "secret", "token", "key", "auth",
	"credential", "private", "apikey", "api_key",
	"accesskey", "access_key", "pwd", "passwd",
}

// SecretURLParams defines URL parameters that may contain secrets
var SecretURLParams = []string{
	"token", "key", "sig", "signature", "credential",
	"password", "passwd", "pwd", "secret", "auth",
}
