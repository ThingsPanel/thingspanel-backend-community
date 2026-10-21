package northbound

// Wire contract identifiers shared by the Community runtime and the northbound
// service. The authoritative definition is
// docs/northbound/contracts/northbound-source-v1.md; this file is the local
// implementation of that contract, not a second source of truth.
const (
	// SourceContractVersion is the handoff envelope version.
	SourceContractVersion = "northbound-source/v1"
	// ConnectorContractVersion is the plugin wire contract version.
	ConnectorContractVersion = "northbound-connector/v1"
)

// SubscriptionResponse is the relay's view of which tenants have active
// forwarding tasks. It carries no payload and no credential.
type SubscriptionResponse struct {
	Tenants []TenantSubscription `json:"tenants"`
}

// TenantSubscription is one tenant entry in the subscription refresh.
type TenantSubscription struct {
	TenantID        string `json:"tenantId"`
	ActivePipelines int64  `json:"activePipelines"`
}
