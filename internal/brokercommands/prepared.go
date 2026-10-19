package brokercommands

import "project/internal/downlink"

// PreparedCommand freezes the resolved downlink and encoded protocol bytes at
// acceptance time so later script/cache changes cannot reinterpret the intent.
type PreparedCommand struct {
	Message             downlink.Message `json:"message"`
	Encoded             []byte           `json:"encoded"`
	ConfigurationSHA256 string           `json:"configuration_sha256"`
	Automation          *AutomationProof `json:"automation,omitempty"`
}

// AutomationProof is generated only by the persisted ingress effect planner.
// It is revalidated against the current policy and its action receipt at dispatch.
type AutomationProof struct {
	EffectID     string `json:"effect_id"`
	SceneID      string `json:"scene_id"`
	ActionID     string `json:"action_id"`
	PolicySHA256 string `json:"policy_sha256"`
}
