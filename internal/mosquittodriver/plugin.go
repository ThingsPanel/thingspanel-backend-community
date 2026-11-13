package mosquittodriver

import (
	"strings"
	"time"

	bc "project/internal/brokercontract"
)

func pluginServiceGrant(g bc.Grant) bool {
	return g.Principal.Kind == bc.PrincipalService && g.PluginContextID != "" && len(g.PluginTargets) > 0
}

func pluginUplinkTopicAllowed(g bc.Grant, wireTopic string) bool {
	for _, target := range g.PluginTargets {
		if _, ok := pluginCanonicalUplinkTopic(wireTopic, target); ok {
			return true
		}
	}
	return false
}

func isPluginUplinkFamily(parts []string, target bc.PluginTarget) bool {
	switch {
	case len(parts) == 2 && parts[1] == "telemetry":
		return true
	case len(parts) == 3 && parts[1] == "event" && parts[2] != "":
		return true
	case len(parts) == 3 && parts[1] == "attributes" && parts[2] != "":
		return true
	case len(parts) == 4 && parts[1] == "command" && parts[2] == "response" && parts[3] != "":
		return true
	case len(parts) == 5 && parts[1] == "attributes" && parts[2] == "set" && parts[3] == "response" && parts[4] != "":
		return true
	case len(parts) == 3 && parts[1] == "status" && parts[2] == target.DeviceID:
		return true
	default:
		return false
	}
}

func isPluginStatusWill(g bc.Grant, topic string) bool {
	for _, target := range g.PluginTargets {
		canonical, ok := pluginCanonicalUplinkTopic(topic, target)
		if !ok {
			continue
		}
		parts := strings.Split(canonical, "/")
		if len(parts) == 3 && (parts[0] == "devices" || parts[0] == "gateway") && parts[1] == "status" && parts[2] == target.DeviceID {
			return true
		}
	}
	return false
}

// pluginFrameTarget returns true only for a broker-stored frame whose actual
// wire destination is represented by one active owner-resolved target.
func (d *Driver) pluginFrameTarget(g bc.Grant, frame bc.CommandFrame) bool {
	if !pluginServiceGrant(g) || g.PluginContextID == "" || frame.TenantID == "" || frame.TargetDeviceID == "" {
		return false
	}
	prefix, canonical, ok := bc.SplitControlledDownlinkTopic(frame.WireTopic)
	if !ok || prefix == "" {
		return false
	}
	parts := strings.Split(canonical, "/")
	for _, target := range g.PluginTargets {
		if target.Validate() != nil || target.BindingPhase != "active" ||
			target.TenantID != frame.TenantID || target.DeviceID != frame.TargetDeviceID ||
			target.RouteEpoch != frame.RouteEpoch || target.MappingVersion != frame.MappingVersion ||
			target.TopicPrefix != prefix {
			continue
		}
		if !controlledTopicTargetsNumber(parts, target.DeviceNumber) {
			continue
		}
		if d.cfg.RuntimeFencing {
			d.mu.Lock()
			valid := time.Now().Before(d.runtimeValidUntil) && frame.RouteEpoch > d.runtimeFences[runtimeScope(frame.TenantID, frame.TargetDeviceID)]
			d.mu.Unlock()
			if !valid {
				continue
			}
		}
		return true
	}
	return false
}

func controlledTopicTargetsNumber(parts []string, number string) bool {
	switch {
	case len(parts) == 4 && parts[1] == "command":
		return parts[2] == number
	case len(parts) == 4 && parts[1] == "telemetry" && parts[2] == "control":
		return parts[3] == number
	case len(parts) == 5 && parts[1] == "attributes" && parts[2] == "set":
		return parts[3] == number
	case len(parts) == 4 && parts[1] == "attributes" && parts[2] == "get":
		return parts[3] == number
	default:
		return false
	}
}

func pluginSubscribeFilterAllowed(g bc.Grant, filter string) bool {
	for _, target := range g.PluginTargets {
		prefix := target.TopicPrefix
		candidate := filter
		if strings.HasPrefix(candidate, prefix) {
			candidate = strings.TrimPrefix(candidate, prefix)
		} else if !strings.HasPrefix(candidate, "devices/") && !strings.HasPrefix(candidate, "gateway/") {
			continue
		}
		if !isPluginDownlinkSubscription(candidate) {
			continue
		}
		return true
	}
	return false
}

func isPluginDownlinkSubscription(filter string) bool {
	parts := strings.Split(filter, "/")
	if len(parts) < 2 || (parts[0] != "devices" && parts[0] != "gateway") {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	switch {
	case len(parts) == 4 && parts[1] == "command" && parts[2] == "+" && parts[3] == "+":
		return true
	case len(parts) == 5 && parts[1] == "attributes" && parts[2] == "set" && parts[3] == "+" && parts[4] == "+":
		return true
	case len(parts) == 4 && parts[1] == "telemetry" && parts[2] == "control" && parts[3] == "+":
		return true
	case len(parts) == 4 && parts[1] == "attributes" && parts[2] == "get" && parts[3] == "+":
		return true
	case len(parts) == 5 && (parts[1] == "event" || parts[1] == "attributes") && parts[2] == "response" && parts[3] == "+" && parts[4] == "+":
		return true
	default:
		return false
	}
}

func (d *Driver) encodePluginIngress(g bc.Grant, topic string, payload []byte, qos byte, retain bool) (string, []byte, bool, error) {
	ingestionID, err := d.nextIngestionID()
	if err != nil {
		return "", nil, false, err
	}
	return d.encodePluginIngressWithID(g, topic, payload, qos, retain, ingestionID, time.Now().UTC())
}

func (d *Driver) encodePluginIngressWithID(g bc.Grant, topic string, payload []byte, qos byte, retain bool, ingestionID string, now time.Time) (string, []byte, bool, error) {
	internalTopic, err := bc.IngressTopic(g.Principal.TenantID, g.Principal.DeviceID, []byte(topic))
	if err != nil {
		return "", nil, false, err
	}
	metadata := bc.Metadata{
		PluginContextID: g.PluginContextID, ContractVersion: bc.ContractVersion,
		TenantID: g.Principal.TenantID, PrincipalKind: g.Principal.Kind,
		BrokerCell: d.cfg.Cell, BrokerInstance: d.cfg.Instance,
		ConnectionID: g.ConnectionID, IngestionID: ingestionID,
		RouteEpoch: g.RouteEpoch, PolicyVersion: g.PolicyVersion, MappingVersion: g.MappingVersion,
		OriginalTopicB64: bc.EncodeOriginalTopic([]byte(topic)), OriginalQoS: qos,
		OriginalRetain: retain, ObservedAt: now,
	}
	body, err := bc.EncodeEnvelope(g.Principal, metadata, payload)
	return internalTopic, body, retain, err
}

func pluginStatusTargetID(g bc.Grant, topic string) (string, bool) {
	for _, target := range g.PluginTargets {
		canonical, ok := pluginCanonicalUplinkTopic(topic, target)
		if !ok {
			continue
		}
		parts := strings.Split(canonical, "/")
		if len(parts) == 3 && (parts[0] == "devices" || parts[0] == "gateway") && parts[1] == "status" && parts[2] == target.DeviceID {
			return target.DeviceID, true
		}
	}
	return "", false
}

func pluginCanonicalUplinkTopic(topic string, target bc.PluginTarget) (string, bool) {
	if bc.ValidatePublishTopic(topic) == nil && isPluginUplinkFamily(strings.Split(topic, "/"), target) {
		return topic, true
	}
	if strings.HasPrefix(topic, target.TopicPrefix) {
		canonical := strings.TrimPrefix(topic, target.TopicPrefix)
		if bc.ValidatePublishTopic(canonical) == nil && isPluginUplinkFamily(strings.Split(canonical, "/"), target) {
			return canonical, true
		}
	}
	return "", false
}

func (d *Driver) rememberPluginStatus(ptr uintptr, g bc.Grant, topic string, payload []byte, qos byte, retain bool) {
	if g.PluginContextID == "" || retain || (string(payload) != "0" && string(payload) != "1") {
		return
	}
	id, ok := pluginStatusTargetID(g, topic)
	if !ok {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.sessions[ptr]
	if s == nil || s.grant.ConnectionID != g.ConnectionID || s.grant.PluginContextID != g.PluginContextID {
		return
	}
	if s.pluginStatuses == nil {
		s.pluginStatuses = make(map[string]pluginStatus, len(g.PluginTargets))
	}
	status, exists := s.pluginStatuses[id]
	if !exists {
		s.pluginStatusOrder = append(s.pluginStatusOrder, id)
	}
	status.topic = topic
	status.payload = append(status.payload[:0], payload...)
	status.qos = qos
	status.lastEmitted = time.Now()
	status.disconnectPending = false
	s.pluginStatuses[id] = status
	if s.closed && !hasPendingPluginStatus(s) {
		s.retireAt = time.Now().Add(5 * time.Second)
	}
}

func hasPendingPluginStatus(s *session) bool {
	for _, status := range s.pluginStatuses {
		if status.disconnectPending {
			return true
		}
	}
	return false
}

// nextPluginStatusLocked emits one already-reported status per call. A closed
// SDK session can only emit its final offline value; it never infers online.
func (d *Driver) nextPluginStatusLocked(s *session, now time.Time) (topic string, payload []byte, qos byte, ingestionID string, ok bool) {
	if len(s.pluginStatusOrder) == 0 || !d.runtimeGrantAllowedLocked(s.grant) {
		return "", nil, 0, "", false
	}
	n := len(s.pluginStatusOrder)
	for i := 0; i < n; i++ {
		index := (s.pluginStatusCursor + i) % n
		deviceID := s.pluginStatusOrder[index]
		status := s.pluginStatuses[deviceID]
		if s.closed {
			if !status.disconnectPending {
				continue
			}
			status.disconnectPending = false
			status.payload = []byte("0")
		} else if !s.connected || now.Sub(status.lastEmitted) < 30*time.Second {
			continue
		}
		if d.cfg.RuntimeFencing {
			var target *bc.PluginTarget
			for j := range s.grant.PluginTargets {
				if s.grant.PluginTargets[j].DeviceID == deviceID {
					target = &s.grant.PluginTargets[j]
					break
				}
			}
			if target == nil || target.RouteEpoch <= d.runtimeFences[runtimeScope(target.TenantID, target.DeviceID)] {
				status.disconnectPending = false
				s.pluginStatuses[deviceID] = status
				continue
			}
		}
		status.lastEmitted = now
		s.pluginStatuses[deviceID] = status
		s.pluginStatusCursor = (index + 1) % n
		if s.closed && !hasPendingPluginStatus(s) {
			s.retireAt = now.Add(5 * time.Second)
		}
		id, err := d.nextIngestionIDLocked()
		if err != nil {
			return "", nil, 0, "", false
		}
		return status.topic, append([]byte(nil), status.payload...), status.qos, id, true
	}
	return "", nil, 0, "", false
}
