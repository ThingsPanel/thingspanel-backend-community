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
	CommandID          string `json:"command_id"`
	TenantID           string `json:"tenant_id"`
	TargetDeviceID     string `json:"target_device_id"`
	TargetDeviceNumber string `json:"target_device_number,omitempty"`
	RouteEpoch         uint64 `json:"route_epoch"`
	MappingVersion     string `json:"mapping_version"`
	// CanonicalTopic is populated for custom wire mappings. Empty preserves the
	// original standard/plugin frame encoding, where WireTopic is canonical.
	CanonicalTopic string `json:"canonical_topic,omitempty"`
	WireTopic      string `json:"wire_topic"`
	ExpiresAt      int64  `json:"expires_at"`
	Payload        []byte `json:"payload"`
}

func IsCommandFrame(b []byte) bool { return bytes.HasPrefix(b, []byte(commandMarker)) }

func (f CommandFrame) Validate(now time.Time) error {
	if f.CommandID == "" || f.TenantID == "" || f.TargetDeviceID == "" || f.RouteEpoch == 0 || f.MappingVersion == "" || f.WireTopic == "" || strings.ContainsAny(f.WireTopic, "+#\x00") || IsInternalResource(f.WireTopic) || ValidatePublishTopic(f.WireTopic) != nil || len(f.Payload) == 0 || len(f.Payload) > MaxCommandPayload {
		return errors.New("invalid command storage frame")
	}
	canonical := f.CanonicalTopic
	if canonical == "" {
		canonical = f.WireTopic
	} else if strings.ContainsAny(canonical, "+#\x00") || IsInternalResource(canonical) || !IsControlledDownlinkTopic(canonical) || f.TargetDeviceNumber == "" {
		return errors.New("invalid canonical command topic")
	}
	_, canonical, controlled := SplitControlledDownlinkTopic(canonical)
	if !controlled {
		return errors.New("invalid controlled downlink topic")
	}
	parts := strings.Split(canonical, "/")
	if f.CanonicalTopic != "" {
		deviceNumberIndex := 2
		if len(parts) > 1 && parts[1] == "telemetry" {
			deviceNumberIndex = 3
		} else if len(parts) > 2 && parts[1] == "attributes" {
			deviceNumberIndex = 3
		}
		if deviceNumberIndex >= len(parts) || parts[deviceNumberIndex] != f.TargetDeviceNumber {
			return errors.New("canonical command topic does not match target device number")
		}
	}
	if (IsCommandTopic(canonical) && parts[3] != f.CommandID) || (len(parts) == 5 && parts[1] == "attributes" && parts[2] == "set" && parts[4] != f.CommandID) {
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
	return len(parts) == 4 && (parts[0] == "devices" || parts[0] == "gateway") && parts[1] == "command" && parts[2] != "" && parts[2] != "response" && parts[3] != ""
}

// SplitControlledDownlinkTopic recognizes a controlled device-wire topic either
// directly or beneath a validated legacy plugin prefix. The returned canonical
// topic excludes the plugin prefix; ordinary report/response topics are false.
func SplitControlledDownlinkTopic(topic string) (prefix, canonical string, ok bool) {
	if ValidatePublishTopic(topic) != nil {
		return "", "", false
	}
	if isCanonicalControlledDownlink(topic) {
		return "", topic, true
	}
	parts := strings.Split(topic, "/")
	for i := 1; i < len(parts); i++ {
		candidatePrefix := strings.Join(parts[:i], "/") + "/"
		candidate := strings.Join(parts[i:], "/")
		if ValidPluginPrefix(candidatePrefix) && isCanonicalControlledDownlink(candidate) {
			return candidatePrefix, candidate, true
		}
	}
	return "", "", false
}

// IsControlledDownlinkTopic is a finite set of device-wire control families,
// optionally beneath a registered service topic prefix. Attribute report/event
// replies are deliberately outside it.
func IsControlledDownlinkTopic(topic string) bool {
	_, _, ok := SplitControlledDownlinkTopic(topic)
	return ok
}

func isCanonicalControlledDownlink(topic string) bool {
	if IsCommandTopic(topic) {
		return true
	}
	p := strings.Split(topic, "/")
	if len(p) < 4 || (p[0] != "devices" && p[0] != "gateway") {
		return false
	}
	for _, level := range p {
		if level == "" {
			return false
		}
	}
	return (len(p) == 4 && p[1] == "telemetry" && p[2] == "control") ||
		(len(p) == 5 && p[1] == "attributes" && p[2] == "set" && p[3] != "response") ||
		(len(p) == 4 && p[1] == "attributes" && p[2] == "get")
}
