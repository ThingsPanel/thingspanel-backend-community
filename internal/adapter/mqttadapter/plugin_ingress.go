package mqttadapter

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"project/internal/brokercommands"
	bc "project/internal/brokercontract"
	"project/internal/brokercontrol"
	"project/internal/brokerstate"
	"project/internal/dal"
	"project/internal/uplink"
	"project/pkg/global"
)

const pluginIngressClockSkew = 5 * time.Second

var (
	errPluginIngressDenied      = errors.New("plugin ingress denied")
	errPluginIngressUnavailable = errors.New("plugin ingress authorization unavailable")
)

type pluginWirePayload struct {
	DeviceID string
	Values   []byte
	Wrapped  bool
}

// normalizePluginIngress translates an Owner-authenticated plugin socket packet
// into the existing device/gateway ingress contract. It never grants scope from
// payload identity: payload hints only select among targets in the immutable
// Owner context.
func (a *Adapter) normalizePluginIngress(env bc.Envelope) (bc.Envelope, string, bool, error) {
	isService := env.Metadata.PrincipalKind == bc.PrincipalService
	if !isService && env.Metadata.PluginContextID == "" {
		return env, "", false, nil
	}
	if env.Metadata.PluginContextID == "" || global.DB == nil {
		return bc.Envelope{}, "", true, errPluginIngressDenied
	}
	ctx, err := brokercontrol.ReadPluginContext(a.adapterContext(), global.DB, env.Metadata.PluginContextID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return bc.Envelope{}, "", true, errPluginIngressDenied
		}
		return bc.Envelope{}, "", true, errPluginIngressUnavailable
	}
	if ctx.ID != env.Metadata.PluginContextID || ctx.Principal.TenantID != env.Metadata.TenantID ||
		ctx.Principal.DeviceID != env.DeviceID || ctx.Principal.Kind != env.Metadata.PrincipalKind ||
		ctx.BrokerCell != env.Metadata.BrokerCell || ctx.BrokerInstance != env.Metadata.BrokerInstance ||
		ctx.ConnectionID == "" || ctx.ConnectionID != env.Metadata.ConnectionID ||
		env.Metadata.ObservedAt.Before(ctx.IssuedAt.Add(-pluginIngressClockSkew)) ||
		env.Metadata.ObservedAt.After(ctx.ExpiresAt.Add(pluginIngressClockSkew)) || len(ctx.Targets) == 0 {
		return bc.Envelope{}, "", true, errPluginIngressDenied
	}
	if isService && ctx.Principal.Kind != bc.PrincipalService {
		return bc.Envelope{}, "", true, errPluginIngressDenied
	}

	wireTopicBytes, err := base64.StdEncoding.Strict().DecodeString(env.Metadata.OriginalTopicB64)
	if err != nil {
		return bc.Envelope{}, "", true, errPluginIngressDenied
	}
	wireTopic := string(wireTopicBytes)
	if bc.ValidatePublishTopic(wireTopic) != nil {
		return bc.Envelope{}, "", true, errPluginIngressDenied
	}
	canonicalCandidates := make([]pluginTopicCandidate, 0, len(ctx.Targets))
	for _, target := range ctx.Targets {
		if target.Validate() != nil || target.TenantID == "" || target.DeviceID == "" ||
			target.RouteEpoch == 0 || target.MappingVersion == "" {
			return bc.Envelope{}, "", true, errPluginIngressDenied
		}
		canonical, ok := stripPluginTopicPrefix(wireTopic, target.TopicPrefix)
		if ok {
			canonicalCandidates = append(canonicalCandidates, pluginTopicCandidate{target: target, canonical: canonical})
		}
	}
	if len(canonicalCandidates) == 0 {
		return bc.Envelope{}, "", true, errPluginIngressDenied
	}
	// A shared socket can have many target prefixes, but one wire topic must
	// resolve to one canonical topic. Overlapping prefixes that produce two
	// interpretations are ambiguous and must fail closed.
	canonicalTopic := canonicalCandidates[0].canonical
	for _, candidate := range canonicalCandidates[1:] {
		if candidate.canonical != canonicalTopic {
			return bc.Envelope{}, "", true, errPluginIngressDenied
		}
	}

	var selected bc.PluginTarget
	var decoded pluginWirePayload
	if isPluginStatusTopic(canonicalCandidates) {
		decoded = pluginWirePayload{Values: append([]byte(nil), env.Values...)}
		selected, canonicalTopic, err = selectPluginStatusTarget(canonicalCandidates, decoded)
	} else {
		decoded, err = decodePluginPrincipalPayload(env.Values, isService)
		if err != nil {
			return bc.Envelope{}, "", true, errPluginIngressDenied
		}
		kind, messageID, classifyErr := classifyIngressTopic(canonicalTopic)
		if classifyErr == nil && (kind == uplink.MessageTypeCommandResponse || kind == uplink.MessageTypeGatewayCommandResponse) && decoded.DeviceID == "" {
			selected, canonicalTopic, err = a.selectPluginCommandTarget(canonicalCandidates, messageID, ctx.BrokerCell)
		} else {
			selected, canonicalTopic, err = selectPluginBusinessTarget(canonicalCandidates, decoded)
		}
		if err == nil && decoded.Wrapped {
			ack := a.isPluginCommandAck(canonicalTopic)
			var values map[string]json.RawMessage
			if !ack && json.Unmarshal(env.Values, &values) == nil {
				if raw, ok := values["values"]; ok && len(raw) > 0 && raw[0] == '{' {
					err = errPluginIngressDenied
				}
			}
		}
	}
	if err != nil {
		return bc.Envelope{}, "", true, errPluginIngressDenied
	}
	if selected.MappingVersion != env.Metadata.MappingVersion {
		return bc.Envelope{}, "", true, errPluginIngressDenied
	}

	out := env
	out.DeviceID = selected.DeviceID
	out.Values = decoded.Values
	out.Metadata.TenantID = selected.TenantID
	out.Metadata.PrincipalKind = selected.Kind
	out.Metadata.RouteEpoch = selected.RouteEpoch
	out.Metadata.MappingVersion = selected.MappingVersion
	// Keep the service connection and original wire topic for trusted audit.
	// The caller uses canonicalTopic for classification/dispatch.
	return out, canonicalTopic, true, nil
}

func decodePluginPrincipalPayload(payload []byte, service bool) (pluginWirePayload, error) {
	if service {
		return decodePluginWirePayload(payload)
	}
	// Direct device/gateway credentials carry the business payload as-is. In
	// particular, a legitimate telemetry object may itself contain a field
	// named "values" and is not the old service-plugin wrapper.
	if len(payload) == 0 {
		return pluginWirePayload{}, errPluginIngressDenied
	}
	return pluginWirePayload{Values: append([]byte(nil), payload...)}, nil
}

type pluginTopicCandidate struct {
	target    bc.PluginTarget
	canonical string
}

func stripPluginTopicPrefix(topic, prefix string) (string, bool) {
	if prefix != "" && strings.HasPrefix(topic, prefix) {
		canonical := strings.TrimPrefix(topic, prefix)
		if canonical == "" || bc.ValidatePublishTopic(canonical) != nil {
			return "", false
		}
		return canonical, true
	}
	if strings.HasPrefix(topic, "devices/") || strings.HasPrefix(topic, "gateway/") {
		return topic, true
	}
	return "", false
}

func decodePluginWirePayload(payload []byte) (pluginWirePayload, error) {
	out := pluginWirePayload{Values: payload}
	if len(payload) == 0 {
		return out, errPluginIngressDenied
	}
	if !json.Valid(payload) {
		// Non-JSON plugin payloads remain byte-for-byte business payloads. The
		// downstream canonical topic handler decides whether that family accepts
		// them; the bridge does not reinterpret or transcode them.
		return out, nil
	}
	if bc.ValidateUniqueJSON(payload) != nil {
		return out, errPluginIngressDenied
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return out, errPluginIngressDenied
	}
	rawDeviceID, hasDeviceID := fields["device_id"]
	rawValues, hasValues := fields["values"]
	if !hasDeviceID || !hasValues {
		return out, nil
	}
	if len(fields) != 2 || json.Unmarshal(rawDeviceID, &out.DeviceID) != nil || out.DeviceID == "" {
		return out, errPluginIngressDenied
	}
	var encoded string
	if json.Unmarshal(rawValues, &encoded) == nil {
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(decoded) > bc.MaxPayloadBytes {
			return out, errPluginIngressDenied
		}
		out.Values = decoded
		out.Wrapped = true
		return out, nil
	}
	// A small number of v2 plugin consumers put a JSON ACK object in values.
	// Accept that object only on an ACK topic; caller enforces that condition.
	if len(rawValues) > 0 && rawValues[0] == '{' {
		out.Values = append([]byte(nil), rawValues...)
		out.Wrapped = true
		return out, nil
	}
	return out, errPluginIngressDenied
}

func isPluginStatusTopic(candidates []pluginTopicCandidate) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		parts := strings.Split(candidate.canonical, "/")
		if len(parts) != 3 || (parts[0] != "devices" && parts[0] != "gateway") || parts[1] != "status" {
			return false
		}
	}
	return true
}

func selectPluginStatusTarget(candidates []pluginTopicCandidate, payload pluginWirePayload) (bc.PluginTarget, string, error) {
	var matches []pluginTopicCandidate
	for _, candidate := range candidates {
		parts := strings.Split(candidate.canonical, "/")
		if parts[2] != candidate.target.DeviceID && parts[2] != candidate.target.DeviceNumber {
			continue
		}
		if payload.DeviceID != "" && payload.DeviceID != candidate.target.DeviceID && payload.DeviceID != candidate.target.DeviceNumber {
			continue
		}
		matches = append(matches, candidate)
	}
	if len(matches) != 1 || (string(payload.Values) != "1" && string(payload.Values) != "0") || payload.Wrapped {
		return bc.PluginTarget{}, "", errPluginIngressDenied
	}
	return matches[0].target, matches[0].canonical, nil
}

func selectPluginBusinessTarget(candidates []pluginTopicCandidate, payload pluginWirePayload) (bc.PluginTarget, string, error) {
	var matches []pluginTopicCandidate
	for _, candidate := range candidates {
		if payload.DeviceID != "" && payload.DeviceID != candidate.target.DeviceID && payload.DeviceID != candidate.target.DeviceNumber {
			continue
		}
		// Standard telemetry/event/attribute topics do not always encode a
		// device identifier. Without an explicit wrapper identity, only a
		// single-target context can select the destination.
		if payload.DeviceID == "" && len(candidates) != 1 {
			continue
		}
		matches = append(matches, candidate)
	}
	if len(matches) != 1 {
		return bc.PluginTarget{}, "", errPluginIngressDenied
	}
	_, _, err := classifyIngressTopic(matches[0].canonical)
	if err != nil {
		return bc.PluginTarget{}, "", errPluginIngressDenied
	}
	if payload.Wrapped && payload.DeviceID != matches[0].target.DeviceID && payload.DeviceID != matches[0].target.DeviceNumber {
		return bc.PluginTarget{}, "", errPluginIngressDenied
	}
	return matches[0].target, matches[0].canonical, nil
}

func (a *Adapter) selectPluginCommandTarget(candidates []pluginTopicCandidate, messageID, cell string) (bc.PluginTarget, string, error) {
	if messageID == "" || global.DB == nil {
		return bc.PluginTarget{}, "", errPluginIngressDenied
	}
	var matches []pluginTopicCandidate
	for _, candidate := range candidates {
		row, err := brokercommands.New(global.DB).GetScoped(a.adapterContext(), candidate.target.TenantID, messageID)
		if err == nil {
			if row.TargetDeviceID == candidate.target.DeviceID && row.RouteCell == cell && row.RouteEpoch == int64(candidate.target.RouteEpoch) {
				matches = append(matches, candidate)
			}
			continue
		}
		if !errors.Is(err, brokercommands.ErrNotFound) {
			return bc.PluginTarget{}, "", errPluginIngressUnavailable
		}
		// Pre-durable in-flight commands have no route epoch. Preserve only the
		// existing scoped legacy path: the row must match this target device and
		// tenant; message_id alone never selects a target. HandleIngress then
		// routes it through its legacy command-log completion path.
		legacy, legacyErr := dal.GetCommandSetLogByMessageIDAndTenant(a.adapterContext(), messageID, candidate.target.TenantID)
		if legacyErr == nil && legacy != nil && legacy.DeviceID == candidate.target.DeviceID {
			matches = append(matches, candidate)
			continue
		}
		if legacyErr != nil && !errors.Is(legacyErr, gorm.ErrRecordNotFound) {
			return bc.PluginTarget{}, "", errPluginIngressUnavailable
		}
	}
	if len(matches) != 1 {
		return bc.PluginTarget{}, "", errPluginIngressDenied
	}
	return matches[0].target, matches[0].canonical, nil
}

func (a *Adapter) handleNormalizedPluginStatus(env bc.Envelope, canonicalTopic string) error {
	if global.DB == nil {
		return errPluginIngressUnavailable
	}
	kind := env.Metadata.PrincipalKind
	if kind != bc.PrincipalDevice && kind != bc.PrincipalGateway {
		return errPluginIngressDenied
	}
	online := string(env.Values) == "1"
	if !online && string(env.Values) != "0" {
		return errPluginIngressDenied
	}
	bootID, eventSeq, err := parsePluginIngestionID(env.Metadata.IngestionID)
	if err != nil {
		return errPluginIngressDenied
	}
	observation := bc.StateObservation{ContractVersion: bc.ContractVersion,
		Principal:  bc.Principal{TenantID: env.Metadata.TenantID, DeviceID: env.DeviceID, Kind: kind},
		BrokerCell: env.Metadata.BrokerCell, BrokerInstance: env.Metadata.BrokerInstance, NodeBootID: bootID,
		ConnectionID: env.Metadata.ConnectionID, RouteEpoch: env.Metadata.RouteEpoch, EventSeq: eventSeq,
		MappingVersion: env.Metadata.MappingVersion, PolicyVersion: env.Metadata.PolicyVersion,
		Online: online, ObservedAt: env.Metadata.ObservedAt}
	decision, err := brokerstate.New(global.DB).Apply(a.adapterContext(), observation)
	if err != nil {
		return err
	}
	if !decision.Fresh {
		return nil
	}
	if decision.Status == "online" {
		if err = a.RememberMapping(env.DeviceID, env.Metadata.MappingVersion); err != nil {
			return err
		}
	}
	value := []byte("0")
	if decision.Status == "online" {
		value = []byte("1")
	}
	if a.bus == nil {
		return errors.New("status projection bus is unavailable")
	}
	return a.bus.Publish(&uplink.DeviceMessage{Type: uplink.MessageTypeStatus, DeviceID: env.DeviceID,
		TenantID: env.Metadata.TenantID, Timestamp: env.Metadata.ObservedAt.UnixMilli(), Payload: value,
		Metadata: map[string]interface{}{"device_id": env.DeviceID, "source": "broker_connection", "source_protocol": "mqtt",
			"transport_profile": "ingress_v1", "connection_id": env.Metadata.ConnectionID, "node_boot_id": bootID,
			"event_seq": eventSeq, "route_epoch": env.Metadata.RouteEpoch, "plugin_context_id": env.Metadata.PluginContextID,
			"topic": canonicalTopic}})
}

func parsePluginIngestionID(value string) (string, uint64, error) {
	if !strings.HasPrefix(value, "mosq-") {
		return "", 0, errPluginIngressDenied
	}
	at := strings.LastIndexByte(value, '-')
	if at <= len("mosq-") || at == len(value)-1 {
		return "", 0, errPluginIngressDenied
	}
	boot := value[len("mosq-"):at]
	seq, err := strconv.ParseUint(value[at+1:], 16, 64)
	if err != nil || seq == 0 || strings.ContainsAny(boot, "\x00/+#") {
		return "", 0, errPluginIngressDenied
	}
	return boot, seq, nil
}

func (a *Adapter) isPluginCommandAck(canonicalTopic string) bool {
	kind, _, err := classifyIngressTopic(canonicalTopic)
	return err == nil && (kind == uplink.MessageTypeCommandResponse || kind == uplink.MessageTypeGatewayCommandResponse)
}
