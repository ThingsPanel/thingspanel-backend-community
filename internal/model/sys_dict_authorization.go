package model

// DictionaryAuthorizationRequest is accepted only over the dedicated service
// transport from the native owner after its live session has been validated.
// A caller cannot choose an arbitrary Casbin resource or supply a role grant.
type DictionaryAuthorizationRequest struct {
	ProtocolVersion int    `json:"protocolVersion"`
	TenantID        string `json:"tenantId"`
	SubjectID       string `json:"subjectId"`
	Operation       string `json:"operation"`
}

type DictionaryAuthorizationResponse struct {
	ProtocolVersion int    `json:"protocolVersion"`
	Authorized      bool   `json:"authorized"`
	TenantID        string `json:"tenantId"`
	SubjectID       string `json:"subjectId"`
	Operation       string `json:"operation"`
	Resource        string `json:"resource"`
}
