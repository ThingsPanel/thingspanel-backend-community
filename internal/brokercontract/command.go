package brokercontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// Command frames are private broker storage, never device wire payloads. The
// marker cannot occur at the start of valid JSON ingress envelopes.
const commandMarker = "\x00TP-COMMAND/1\n"
const MaxCommandPayload = 512 << 10

type CommandFrame struct {
	CommandID      string `json:"command_id"`
	TenantID       string `json:"tenant_id"`
	TargetDeviceID string `json:"target_device_id"`
	RouteEpoch     uint64 `json:"route_epoch"`
	MappingVersion string `json:"mapping_version"`
	WireTopic      string `json:"wire_topic"`
	ExpiresAt      int64  `json:"expires_at"`
	Payload        []byte `json:"payload"`
}

func IsCommandFrame(b []byte) bool { return bytes.HasPrefix(b, []byte(commandMarker)) }

func (f CommandFrame) Validate(now time.Time) error {
	if f.CommandID == "" || f.TenantID == "" || f.TargetDeviceID == "" || f.RouteEpoch == 0 || f.MappingVersion == "" || f.WireTopic == "" || strings.ContainsAny(f.WireTopic, "+#\x00") || IsInternalResource(f.WireTopic) || !IsCommandTopic(f.WireTopic) || len(f.Payload) == 0 || len(f.Payload) > MaxCommandPayload {
		return errors.New("invalid command storage frame")
	}
	if strings.Split(f.WireTopic, "/")[3] != f.CommandID {
		return errors.New("command topic message id does not match frame")
	}
	if f.ExpiresAt <= now.UnixMilli() || f.ExpiresAt > now.Add(24*time.Hour).UnixMilli() {
		return errors.New("command storage frame expired or exceeds maximum TTL")
	}
	return nil
}
func EncodeCommandFrame(f CommandFrame, now time.Time) ([]byte, error) {
	if err := f.Validate(now); err != nil {
		return nil, err
	}
	b, err := json.Marshal(f)
	return append([]byte(commandMarker), b...), err
}
func ParseCommandFrame(b []byte, now time.Time) (CommandFrame, error) {
	var f CommandFrame
	if !IsCommandFrame(b) || len(b) > 1<<20 {
		return f, errors.New("missing or oversized command storage frame")
	}
	if err := ValidateUniqueJSON(b[len(commandMarker):]); err != nil {
		return f, err
	}
	d := json.NewDecoder(bytes.NewReader(b[len(commandMarker):]))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return f, err
	}
	if d.Decode(new(any)) != io.EOF {
		return f, errors.New("trailing command storage frame")
	}
	return f, f.Validate(now)
}

func IsCommandTopic(topic string) bool {
	if ValidatePublishTopic(topic) != nil {
		return false
	}
	parts := strings.Split(topic, "/")
	return len(parts) == 4 && (parts[0] == "devices" || parts[0] == "gateway") && parts[1] == "command" && parts[2] != "" && parts[3] != ""
}
