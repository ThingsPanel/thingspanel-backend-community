package pluginmgr

import (
	"encoding/json"
	"time"
)

// DeviceConnector is the canonical resource for a connector type.
// It replaces legacy protocol_plugin + service_plugin vocabulary.
// Legacy tables are bridge projections — this struct is authoritative.
type DeviceConnector struct {
	ID string `json:"id"`

	// ConnectorKey is the stable, immutable string identity.
	// Equals legacy service_plugins.service_identifier and protocol_plugins.protocol_type.
	ConnectorKey string `json:"connectorKey"`

	// DisplayName is the human-readable name.
	// Projected to sys_dict_language.translation.
	DisplayName string `json:"displayName"`

	// MQTTTopicPrefix is prepended to all downlink topics for this connector.
	// Projected to both service_plugins.service_config.sub_topic_prefix and
	// protocol_plugins.sub_topic_prefix.
	MQTTTopicPrefix string `json:"mqttTopicPrefix"`

	// DeviceTypeSupport indicates which device categories this connector serves.
	// Values: "direct" | "gateway" | "both"
	DeviceTypeSupport string `json:"deviceTypeSupport"`

	// HTTPCallbacksEnabled means the connector process exposes an HTTP server for
	// backend callbacks (form/config, disconnect, device add/delete/update, notify).
	HTTPCallbacksEnabled bool `json:"httpCallbacksEnabled"`

	Description    string          `json:"description,omitempty"`
	AdditionalInfo json.RawMessage `json:"additionalInfo,omitempty"`

	// Bridge back-references — output only, written by legacy bridge.
	LegacyServicePluginID  string `json:"legacyServicePluginId,omitempty"`
	LegacyProtocolPluginID string `json:"legacyProtocolPluginId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ConnectorRuntime describes how a DeviceConnector runs.
type ConnectorRuntime struct {
	ID                string            `json:"id"`
	DeviceConnectorID string            `json:"deviceConnectorId"`
	Image             string            `json:"image"`
	EnvVars           map[string]string `json:"envVars,omitempty"`
	HTTPPort          int               `json:"httpPort,omitempty"`
	HealthCheckPath   string            `json:"healthCheckPath,omitempty"`
	ResourceLimits    *ResourceLimits   `json:"resourceLimits,omitempty"`
	Description       string            `json:"description,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

// ResourceLimits holds K8S-style CPU and memory constraints.
type ResourceLimits struct {
	CPURequest    string `json:"cpuRequest,omitempty"`
	CPULimit      string `json:"cpuLimit,omitempty"`
	MemoryRequest string `json:"memoryRequest,omitempty"`
	MemoryLimit   string `json:"memoryLimit,omitempty"`
}

// ConnectorInstance is a deployed, running ConnectorRuntime.
type ConnectorInstance struct {
	ID                 string `json:"id"`
	ConnectorRuntimeID string `json:"connectorRuntimeId"`
	DeviceConnectorID  string `json:"deviceConnectorId"`

	// HTTPAddress is resolved by the runner once the container starts.
	// Projected to legacy service_plugins.service_config.http_address and
	// protocol_plugins.http_address.
	HTTPAddress string    `json:"httpAddress,omitempty"`
	Namespace   string    `json:"namespace,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ConnectorStatus is the observed state of a ConnectorInstance.
// Never mix desired spec into this struct.
type ConnectorStatus struct {
	ConnectorInstanceID string `json:"connectorInstanceId"`

	// Phase is the observed lifecycle phase.
	// Values: "Pending" | "Starting" | "Running" | "Stopped" | "Error"
	Phase string `json:"phase"`

	// ProcessHealthy is true when a heartbeat has been received within 60 seconds.
	// Mirrors legacy service_plugins.service_heartbeat (1=active → true, 2=inactive → false).
	ProcessHealthy  bool       `json:"processHealthy"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	HTTPAddress     string     `json:"httpAddress,omitempty"`
	Message         string     `json:"message,omitempty"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// ConnectorEvent is an append-only lifecycle/diagnostic event.
type ConnectorEvent struct {
	ID                  string `json:"id"`
	ConnectorInstanceID string `json:"connectorInstanceId"`

	// Reason is a machine-readable code, e.g. "Started", "ImagePullFailed",
	// "BridgeProjectionFailed", "HeartbeatMissed", "DisconnectCallbackSent".
	Reason     string    `json:"reason"`
	Message    string    `json:"message,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

// ConnectorBinding associates a device with a ConnectorInstance.
type ConnectorBinding struct {
	ID                  string `json:"id"`
	ConnectorInstanceID string `json:"connectorInstanceId"`
	DeviceConnectorID   string `json:"deviceConnectorId"`
	DeviceID            string `json:"deviceId"`
	Voucher             string `json:"voucher,omitempty"`

	// LegacyServiceAccessID is the shadow service_access row written by the bridge.
	LegacyServiceAccessID string    `json:"legacyServiceAccessId,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
}

// ─── Request / Response types ────────────────────────────────────────────────

type CreateDeviceConnectorRequest struct {
	ConnectorKey         string          `json:"connectorKey"`
	DisplayName          string          `json:"displayName"`
	MQTTTopicPrefix      string          `json:"mqttTopicPrefix"`
	DeviceTypeSupport    string          `json:"deviceTypeSupport"`
	HTTPCallbacksEnabled *bool           `json:"httpCallbacksEnabled"`
	Description          string          `json:"description,omitempty"`
	AdditionalInfo       json.RawMessage `json:"additionalInfo,omitempty"`
}

type UpdateDeviceConnectorRequest struct {
	DisplayName     string          `json:"displayName,omitempty"`
	MQTTTopicPrefix string          `json:"mqttTopicPrefix,omitempty"`
	Description     string          `json:"description,omitempty"`
	AdditionalInfo  json.RawMessage `json:"additionalInfo,omitempty"`
}

type ListDeviceConnectorsRequest struct {
	Page              int    `json:"page,omitempty"`
	PageSize          int    `json:"pageSize,omitempty"`
	DeviceTypeSupport string `json:"deviceTypeSupport,omitempty"`
}

type DeviceConnectorListData struct {
	Total int               `json:"total"`
	List  []DeviceConnector `json:"list"`
}

type CreateConnectorRuntimeRequest struct {
	DeviceConnectorID string            `json:"deviceConnectorId"`
	Image             string            `json:"image"`
	EnvVars           map[string]string `json:"envVars,omitempty"`
	HTTPPort          int               `json:"httpPort,omitempty"`
	HealthCheckPath   string            `json:"healthCheckPath,omitempty"`
	ResourceLimits    *ResourceLimits   `json:"resourceLimits,omitempty"`
	Description       string            `json:"description,omitempty"`
}

type CreateConnectorInstanceRequest struct {
	ConnectorRuntimeID string `json:"connectorRuntimeId"`
	Namespace          string `json:"namespace,omitempty"`
}

type CreateConnectorBindingRequest struct {
	ConnectorInstanceID string `json:"connectorInstanceId"`
	DeviceID            string `json:"deviceId"`
	Voucher             string `json:"voucher,omitempty"`
}

type ConnectorEventListData struct {
	Total int              `json:"total"`
	List  []ConnectorEvent `json:"list"`
}

type ListConnectorEventsRequest struct {
	Page     int `json:"page,omitempty"`
	PageSize int `json:"pageSize,omitempty"`
}

// ─── Phase constants ─────────────────────────────────────────────────────────

const (
	PhasePending  = "Pending"
	PhaseStarting = "Starting"
	PhaseRunning  = "Running"
	PhaseStopped  = "Stopped"
	PhaseError    = "Error"
)

// ─── Event reason constants ───────────────────────────────────────────────────

const (
	ReasonStarted                = "Started"
	ReasonStopped                = "Stopped"
	ReasonImagePullFailed        = "ImagePullFailed"
	ReasonHeartbeatMissed        = "HeartbeatMissed"
	ReasonBridgeProjectionFailed = "BridgeProjectionFailed"
	ReasonDisconnectCallbackSent = "DisconnectCallbackSent"
	ReasonReconcileError         = "ReconcileError"
)
