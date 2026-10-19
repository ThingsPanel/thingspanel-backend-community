package brokercontract

// RuntimeSync is an opt-in internal v1 extension for the community strict
// migration profile. It never changes device MQTT topics or payloads.
const MaxRuntimeFences = 256

type RuntimeFence struct {
	ChangeID    string `json:"change_id"`
	TenantID    string `json:"tenant_id"`
	DeviceID    string `json:"device_id"`
	DeniedEpoch uint64 `json:"denied_epoch"`
	NeedsAck    bool   `json:"needs_ack"`
}
type RuntimeFenceAck struct {
	RuntimeFence
	EvidenceSHA256 string `json:"evidence_sha256"`
}
type RuntimeSyncRequest struct {
	BrokerCell     string            `json:"broker_cell"`
	BrokerInstance string            `json:"broker_instance"`
	BootID         string            `json:"boot_id"`
	Applied        []RuntimeFenceAck `json:"applied"`
}
type RuntimeSyncResponse struct {
	Fences   []RuntimeFence `json:"fences"`
	Overflow bool           `json:"overflow"`
}
