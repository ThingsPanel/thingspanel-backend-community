package brokercontract

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"time"
)

const StatePrefix = "tp-internal/state/v1"

type StateObservation struct {
	MappingVersion  string    `json:"mapping_version"`
	PolicyVersion   string    `json:"policy_version"`
	ContractVersion string    `json:"contract_version"`
	Principal       Principal `json:"principal"`
	BrokerCell      string    `json:"broker_cell"`
	BrokerInstance  string    `json:"broker_instance"`
	NodeBootID      string    `json:"node_boot_id"`
	ConnectionID    string    `json:"connection_id"`
	RouteEpoch      uint64    `json:"route_epoch"`
	EventSeq        uint64    `json:"event_seq"`
	Online          bool      `json:"online"`
	ObservedAt      time.Time `json:"observed_at"`
}

func StateTopic(p Principal) (string, error) {
	if ValidatePrincipal(p) != nil {
		return "", ErrInvalidPrincipal
	}
	return StatePrefix + "/" + base64.RawURLEncoding.EncodeToString([]byte(p.TenantID)) + "/" + base64.RawURLEncoding.EncodeToString([]byte(p.DeviceID)), nil
}
func EncodeState(s StateObservation) (string, []byte, error) {
	t, e := StateTopic(s.Principal)
	if e != nil {
		return "", nil, e
	}
	if s.ContractVersion != ContractVersion || !validIdentity(s.BrokerCell) || !validIdentity(s.BrokerInstance) || !validIdentity(s.NodeBootID) || !validIdentity(s.ConnectionID) || s.MappingVersion == "" || s.PolicyVersion == "" || s.RouteEpoch == 0 || s.EventSeq == 0 || s.ObservedAt.IsZero() {
		return "", nil, ErrInvalidMetadata
	}
	b, e := json.Marshal(s)
	return t, b, e
}
func ParseState(topic string, body []byte, trust TrustedMetadata) (StateObservation, error) {
	var s StateObservation
	if len(body) > 16384 {
		return s, ErrTooLarge
	}
	if ValidateUniqueJSON(body) != nil {
		return s, ErrInvalidEnvelope
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil {
		return s, ErrInvalidEnvelope
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return s, ErrInvalidEnvelope
	}
	t, _, e := EncodeState(s)
	if e != nil {
		return s, e
	}
	if t != topic || trust.BrokerCell == "" || trust.BrokerInstance == "" || s.BrokerCell != trust.BrokerCell || s.BrokerInstance != trust.BrokerInstance {
		return s, ErrUnauthorized
	}
	if s.Principal.Kind == PrincipalService {
		return s, ErrUnauthorized
	}
	return s, nil
}
