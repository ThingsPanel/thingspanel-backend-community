// Package northbound is an optional helper for writing a northbound connector.
//
// It contains wire types and a tiny HTTP server. It deliberately contains no
// queue, no retry policy, no credential storage and no database access: those
// belong to the platform, and a connector that re-implements them becomes a
// second, unobservable delivery system.
//
// A connector may ignore this package entirely and implement the HTTP contract
// by hand; the contract, not the SDK, is authoritative.
package northbound

import (
	"encoding/json"
	"time"
)

// ContractVersion is the wire contract this package implements.
const ContractVersion = "northbound-connector/v1"

// Outcome values a connector may report for one item.
const (
	StatusDelivered = "delivered"
	StatusRetryable = "retryable_failure"
	StatusPermanent = "permanent_failure"
	StatusUnknown   = "unknown"
)

// Ack levels describe what the connector actually proved.
const (
	AckDestinationAcked = "destination_acked"
	AckEffectConfirmed  = "effect_confirmed"
)

// Descriptor is what GET /v1/describe returns.
type Descriptor struct {
	ContractVersion string          `json:"contractVersion"`
	ID              string          `json:"id"`
	Version         string          `json:"version"`
	DisplayName     string          `json:"displayName"`
	Description     string          `json:"description,omitempty"`
	Capabilities    Capabilities    `json:"capabilities"`
	ConfigSchema    json.RawMessage `json:"configSchema"`
	SecretFields    []string        `json:"secretFields"`
	Limits          Limits          `json:"limits"`
}

// Capabilities must be honest: claiming strong idempotency where the target has
// none lets the platform retry into a duplicate write.
type Capabilities struct {
	EventTypes     []string `json:"eventTypes"`
	Batch          bool     `json:"batch"`
	AckLevel       string   `json:"ackLevel"`
	Idempotency    string   `json:"idempotency"` // none | conditional | strong
	Verify         bool     `json:"verify"`
	MaxBatchEvents int      `json:"maxBatchEvents"`
}

// Limits are enforced by the platform before a batch is sent.
type Limits struct {
	MaxBatchEvents  int `json:"maxBatchEvents"`
	MaxPayloadBytes int `json:"maxPayloadBytes"`
	MaxInFlight     int `json:"maxInFlight"`
}

// ValidateRequest is the body of POST /v1/validate.
type ValidateRequest struct {
	ContractVersion string            `json:"contractVersion"`
	Config          json.RawMessage   `json:"config"`
	Secrets         map[string]string `json:"secrets"`
}

// ValidationResult is returned by /v1/validate.
type ValidationResult struct {
	OK      bool    `json:"ok"`
	Message string  `json:"message,omitempty"`
	Errors  []Check `json:"errors,omitempty"`
}

// Check is one named validation result.
type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// OpenRequest is the body of POST /v1/open.
type OpenRequest struct {
	ContractVersion string            `json:"contractVersion"`
	ConnectionID    string            `json:"connectionId"`
	Config          json.RawMessage   `json:"config"`
	Secrets         map[string]string `json:"secrets"`
}

// OpenResponse is returned by /v1/open.
type OpenResponse struct {
	InstanceID string `json:"instanceId"`
	OK         bool   `json:"ok"`
	Message    string `json:"message,omitempty"`
}

// WriteItem is one delivery presented to the connector.
type WriteItem struct {
	DeliveryID     string            `json:"deliveryId"`
	EventID        string            `json:"eventId"`
	IdempotencyKey string            `json:"idempotencyKey"`
	EventType      string            `json:"eventType"`
	Subject        string            `json:"subject"`
	OccurredAt     time.Time         `json:"occurredAt"`
	DeviceID       string            `json:"deviceId"`
	Data           json.RawMessage   `json:"data"`
	Serialization  json.RawMessage   `json:"serialization,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
}

// WriteRequest is the body of POST /v1/write.
type WriteRequest struct {
	ContractVersion string      `json:"contractVersion"`
	ConnectionID    string      `json:"connectionId"`
	InstanceID      string      `json:"instanceId"`
	PipelineID      string      `json:"pipelineId"`
	Attempt         int         `json:"attempt"`
	Items           []WriteItem `json:"items"`
}

// WriteResult reports the outcome for exactly one item.
type WriteResult struct {
	DeliveryID   string          `json:"deliveryId"`
	Status       string          `json:"status"`
	AckLevel     string          `json:"ackLevel,omitempty"`
	Receipt      json.RawMessage `json:"receipt,omitempty"`
	ErrorCode    string          `json:"errorCode,omitempty"`
	ErrorDetail  string          `json:"errorDetail,omitempty"`
	SafeToRetry  *bool           `json:"safeToRetry,omitempty"`
	RetryAfterMS int64           `json:"retryAfterMs,omitempty"`
}

// WriteResponse is returned by /v1/write. It must contain one result per input
// item; a missing result is treated by the platform as an unknown outcome.
type WriteResponse struct {
	ContractVersion string        `json:"contractVersion"`
	Results         []WriteResult `json:"results"`
}

// CloseRequest is the body of POST /v1/close.
type CloseRequest struct {
	ContractVersion string `json:"contractVersion"`
	ConnectionID    string `json:"connectionId"`
	InstanceID      string `json:"instanceId"`
}
