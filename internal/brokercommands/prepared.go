package brokercommands

import (
	"project/internal/brokerroute"
	"project/internal/downlink"
)

// PreparedCommand freezes the resolved downlink and encoded protocol bytes at
// acceptance time so later script/cache changes cannot reinterpret the intent.
type PreparedCommand struct {
	LegacyTelemetryID   bool                   `json:"legacy_telemetry_id,omitempty"`
	TransportRoute      *brokerroute.Snapshot  `json:"transport_route,omitempty"`
	Message             downlink.Message       `json:"message"`
	Encoded             []byte                 `json:"encoded"`
	ConfigurationSHA256 string                 `json:"configuration_sha256"`
	MappingVersion      string                 `json:"mapping_version,omitempty"`
	Automation          *AutomationProof       `json:"automation,omitempty"`
	NativeAutomation    *NativeAutomationProof `json:"native_automation,omitempty"`
}

type NativeAutomationProof struct {
	DeploymentID string `json:"deployment_id"`
	Scope        string `json:"scope"`
	Generation   int64  `json:"generation"`
}

// AutomationProof is generated only by the persisted ingress effect planner.
// It is revalidated against the current policy and its action receipt at dispatch.
type AutomationProof struct {
	EffectID     string `json:"effect_id"`
	SceneID      string `json:"scene_id"`
	ActionID     string `json:"action_id"`
	PolicySHA256 string `json:"policy_sha256"`
}
