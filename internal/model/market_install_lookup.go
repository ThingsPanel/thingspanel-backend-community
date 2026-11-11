package model

// Confirmed means that the installation owner observed the original record,
// not that installation completed. Result.Status is the actual progress state.
// NotObserved does not establish that a concurrent submit has no side effects.
type MarketInstallationLookup struct {
	State        string                 `json:"state"`
	OperationKey string                 `json:"operationKey"`
	Result       *InstallBundleResponse `json:"result,omitempty"`
}

// This identity assertion is accepted only over the existing trusted provider
// transport after native session authentication. It is not a user API body.
// Resource is intentionally not an input: the server fixes it to Install.
type MarketInstallationReadIdentity struct {
	ProtocolVersion int    `json:"protocolVersion"`
	TenantID        string `json:"tenantId"`
	SubjectID       string `json:"subjectId"`
}

type MarketInstallationReadAuthorization struct {
	ProtocolVersion int    `json:"protocolVersion"`
	Authorized      bool   `json:"authorized"`
	TenantID        string `json:"tenantId"`
	SubjectID       string `json:"subjectId"`
	Resource        string `json:"resource"`
}
