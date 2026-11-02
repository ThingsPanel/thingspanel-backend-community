package brokercontract

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
)

// Delegation is minted by normalization after loading the immutable Owner
// context. Payload selectors never populate it. Target and transport migrations
// are independent; the service actor is never substituted for either device.
type Delegation struct {
	ContextID           string    `json:"plugin_context_id"`
	Actor               Principal `json:"service_actor"`
	ConnectionID        string    `json:"connection_id"`
	BrokerCell          string    `json:"broker_cell"`
	BrokerInstance      string    `json:"broker_instance"`
	TargetDeviceID      string    `json:"target_device_id"`
	TargetRouteEpoch    uint64    `json:"target_route_epoch"`
	TransportDeviceID   string    `json:"transport_device_id"`
	TransportRouteEpoch uint64    `json:"transport_route_epoch"`
	MappingVersion      string    `json:"mapping_version"`
	ObservedAt          time.Time `json:"observed_at"`
}

func (d Delegation) Validate() error {
	if !validPluginContextID(d.ContextID) || ValidatePrincipal(d.Actor) != nil || d.Actor.Kind != PrincipalService ||
		!validIdentity(d.ConnectionID) || !validIdentity(d.BrokerCell) || !validIdentity(d.BrokerInstance) ||
		!validIdentity(d.TargetDeviceID) || !validIdentity(d.TransportDeviceID) || d.TargetRouteEpoch == 0 || d.TransportRouteEpoch == 0 ||
		(d.TargetDeviceID == d.TransportDeviceID && d.TargetRouteEpoch != d.TransportRouteEpoch) || !validIdentity(d.MappingVersion) || d.ObservedAt.IsZero() {
		return ErrInvalidMetadata
	}
	return nil
}

// ParseDelegation accepts both in-memory metadata and JSON-restored receipt
// metadata. Missing proof is distinct from malformed proof: never fall back to
// a legacy path when a delegation marker was supplied.
func ParseDelegation(value any) (*Delegation, error) {
	if value == nil {
		return nil, ErrInvalidMetadata
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 16384 {
		return nil, ErrInvalidMetadata
	}
	if ValidateUniqueJSON(raw) != nil {
		return nil, ErrInvalidMetadata
	}
	var out Delegation
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&out) != nil || dec.Decode(new(any)) != io.EOF || out.Validate() != nil {
		return nil, ErrInvalidMetadata
	}
	return &out, nil
}
