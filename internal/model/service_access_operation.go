package model

import "time"

// ServiceAccessCreateObservation contains no credentials or configuration.
// A confirmed creation receipt does not grant activation or deletion authority.
type ServiceAccessCreateObservation struct {
	ProtocolVersion string     `json:"protocol_version"`
	State           string     `json:"state"`
	OperationKey    string     `json:"operation_key"`
	RequestHash     string     `json:"request_hash,omitempty"`
	ID              string     `json:"id,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
	Replayed        bool       `json:"replayed,omitempty"`
}
