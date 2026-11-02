package model

import "encoding/json"

// These wire DTOs implement coordinator-provider-v1; they contain no alternate
// installation runtime. The unique coordinator lives in Encore marketinstall.
type MarketInstallIdentity struct {
	TenantID         string `json:"tenantId"`
	SubjectID        string `json:"subjectId"`
	AuthorizationRef string `json:"authorizationRef"`
}
type MarketInstallRequest struct {
	BundleKey       string               `json:"bundleKey"`
	Version         string               `json:"version"`
	OverwritePolicy string               `json:"overwritePolicy,omitempty"`
	DeviceBindings  []DeviceBindingInput `json:"deviceBindings,omitempty"`
}
type MarketInstallStep struct {
	ID          string          `json:"id"`
	Owner       string          `json:"owner"`
	Action      string          `json:"action"`
	ResourceKey string          `json:"resourceKey"`
	Payload     json.RawMessage `json:"payload"`
}
type MarketInstallPlan struct {
	ArtifactDigest string              `json:"artifactDigest"`
	Steps          []MarketInstallStep `json:"steps"`
}
type MarketInstallPrepareRequest struct {
	ProtocolVersion int `json:"protocolVersion"`
	MarketInstallIdentity
	Request MarketInstallRequest `json:"request"`
}
type MarketInstallReceipt struct {
	Owner       string          `json:"owner"`
	ResourceID  string          `json:"resourceId"`
	PayloadHash string          `json:"payloadHash"`
	Data        json.RawMessage `json:"data"`
}
type MarketInstallPriorResult struct {
	StepID  string               `json:"stepId"`
	Receipt MarketInstallReceipt `json:"receipt"`
}
type MarketInstallStepRequest struct {
	ProtocolVersion int `json:"protocolVersion"`
	MarketInstallIdentity
	OperationID    string                     `json:"operationId"`
	Generation     int64                      `json:"generation"`
	StepID         string                     `json:"stepId"`
	IdempotencyKey string                     `json:"idempotencyKey"`
	PayloadHash    string                     `json:"payloadHash"`
	Step           MarketInstallStep          `json:"step"`
	PriorResults   []MarketInstallPriorResult `json:"priorResults"`
}
type MarketInstallObservation struct {
	State     string                `json:"state"`
	Receipt   *MarketInstallReceipt `json:"receipt,omitempty"`
	ErrorCode string                `json:"errorCode,omitempty"`
}
