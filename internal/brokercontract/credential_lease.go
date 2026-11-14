package brokercontract

import "time"

// The authenticated broker acknowledges only the exact grant it has invalidated.
type CredentialLeaseAck struct {
	BrokerCell     string    `json:"broker_cell"`
	BrokerInstance string    `json:"broker_instance"`
	ConnectionID   string    `json:"connection_id"`
	ExpiresAt      time.Time `json:"expires_at"`
}
