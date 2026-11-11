package brokercontract

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

// ProtocolTelemetry is the legacy protocol SDK wrapper. It is decoded only at
// the authenticated broker boundary; the device ID is accepted as a target
// selector only after it matches the connection's delegated scope.
type ProtocolTelemetry struct {
	DeviceID string
	Values   []byte
}

func ParseProtocolTelemetry(raw []byte) (ProtocolTelemetry, error) {
	var out ProtocolTelemetry
	if len(raw) == 0 || len(raw) > MaxEnvelopeBytes || ValidateUniqueJSON(raw) != nil {
		return out, ErrInvalidEnvelope
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 2 {
		return out, ErrInvalidEnvelope
	}
	if len(fields["device_id"]) == 0 || len(fields["values"]) == 0 {
		return out, ErrInvalidEnvelope
	}
	if err := json.Unmarshal(fields["device_id"], &out.DeviceID); err != nil || !validIdentity(out.DeviceID) {
		return out, ErrInvalidEnvelope
	}
	var encoded string
	if err := json.Unmarshal(fields["values"], &encoded); err != nil || len(encoded) > base64.StdEncoding.EncodedLen(MaxPayloadBytes) {
		return out, ErrInvalidEnvelope
	}
	values, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(values) == 0 || len(values) > MaxPayloadBytes || base64.StdEncoding.EncodeToString(values) != encoded {
		return out, ErrInvalidEnvelope
	}
	out.Values = bytes.Clone(values)
	// Reject trailing values even if a future decoder replaces the map decode.
	dec := json.NewDecoder(bytes.NewReader(raw))
	var ignored any
	if err := dec.Decode(&ignored); err != nil {
		return out, ErrInvalidEnvelope
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return out, errors.New("trailing protocol telemetry JSON")
	}
	return out, nil
}
