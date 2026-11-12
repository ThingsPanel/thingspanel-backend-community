package brokercontrol

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"project/internal/brokercontract"
)

type pluginTargetResolver interface {
	ResolvePluginTargets(context.Context, []string) ([]brokercontract.PluginTarget, error)
}

func pluginOwnedRecord(device DeviceRecord) bool {
	return (device.ServiceAccessID != nil && *device.ServiceAccessID != "") ||
		(device.AccessWay != nil && *device.AccessWay == "B") ||
		(device.ProtocolType != nil && *device.ProtocolType != "" && !strings.EqualFold(*device.ProtocolType, "MQTT"))
}

func (o *Owner) resolvePluginTargets(ctx context.Context, ids, allowedTenants []string, cell string) ([]brokercontract.PluginTarget, error) {
	return o.resolvePluginTargetsWithBinding(ctx, ids, allowedTenants, cell, false)
}

// Publishers use an existing active route. They must not run device admission:
// the command dispatcher already holds that route lock across bounded publish.
func (o *Owner) resolvePluginPublisherTargets(ctx context.Context, ids, allowedTenants []string, cell string) ([]brokercontract.PluginTarget, error) {
	return o.resolvePluginTargetsWithBinding(ctx, ids, allowedTenants, cell, true)
}

func (o *Owner) resolvePluginTargetsWithBinding(ctx context.Context, ids, allowedTenants []string, cell string, publisher bool) ([]brokercontract.PluginTarget, error) {
	repo, ok := o.repo.(pluginTargetResolver)
	if !ok || len(ids) == 0 || len(ids) > MaxServiceTargets {
		return nil, ErrUnavailable
	}
	rows, err := repo.ResolvePluginTargets(ctx, ids)
	if err != nil {
		return nil, ErrUnavailable
	}
	allowedID := make(map[string]bool, len(ids))
	allowedTenant := make(map[string]bool, len(allowedTenants))
	for _, id := range ids {
		allowedID[id] = true
	}
	for _, id := range allowedTenants {
		allowedTenant[id] = true
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TenantID != rows[j].TenantID {
			return rows[i].TenantID < rows[j].TenantID
		}
		return rows[i].DeviceID < rows[j].DeviceID
	})
	byScope := map[string]int{}
	for i := range rows {
		byScope[rows[i].TenantID+"\x00"+rows[i].DeviceID] = i
	}
	// A physical root is not implicit authorization. It must itself be among
	// the explicitly allowed, owner-resolved plugin targets in this grant.
	for i := range rows {
		root := rows[i].TransportDeviceID
		if root == "" {
			root = rows[i].DeviceID
		}
		if _, ok := byScope[rows[i].TenantID+"\x00"+root]; !ok {
			return nil, nil
		}
		rows[i].TransportDeviceID = root
	}
	for i := range rows {
		if !allowedID[rows[i].DeviceID] || (len(allowedTenant) > 0 && !allowedTenant[rows[i].TenantID]) || !brokercontract.ValidPluginPrefix(rows[i].TopicPrefix) {
			return nil, nil
		}
		if i > 0 && rows[i-1].DeviceID == rows[i].DeviceID {
			return nil, nil
		}
		var binding TransportBinding
		var bindErr error
		if publisher {
			reader, ok := o.repo.(interface {
				ReadActiveBinding(context.Context, string, string, string) (TransportBinding, error)
			})
			if !ok {
				return nil, ErrUnavailable
			}
			binding, bindErr = reader.ReadActiveBinding(ctx, rows[i].TenantID, rows[i].DeviceID, cell)
		} else {
			binding, bindErr = o.repo.ResolveActiveBinding(ctx, rows[i].TenantID, rows[i].DeviceID, cell)
			if admission, ok := o.repo.(interface {
				ResolveAdmissionBinding(context.Context, string, string, string) (TransportBinding, error)
			}); ok {
				binding, bindErr = admission.ResolveAdmissionBinding(ctx, rows[i].TenantID, rows[i].DeviceID, cell)
			}
		}
		if bindErr != nil {
			if errors.Is(bindErr, ErrBindingConflict) {
				return nil, nil
			}
			return nil, ErrUnavailable
		}
		rows[i].RouteEpoch = binding.RouteEpoch
		rows[i].BindingPhase = binding.Phase
		rows[i].MappingVersion = MappingVersion
	}
	for i := range rows {
		root := rows[byScope[rows[i].TenantID+"\x00"+rows[i].TransportDeviceID]]
		rows[i].TransportRouteEpoch = root.RouteEpoch
		if root.BindingPhase == "connecting" {
			rows[i].BindingPhase = "connecting"
		}
	}
	if !validPluginTargets(rows) {
		return nil, nil
	}
	return rows, nil
}

func addPluginPublishAliases(policy brokercontract.Policy, targets []brokercontract.PluginTarget) (brokercontract.Policy, error) {
	if len(targets) == 0 || len(policy.Publish) == 0 {
		return policy, nil
	}
	byNumber := make(map[string]brokercontract.PluginTarget, len(targets))
	for _, target := range targets {
		byNumber[target.DeviceNumber] = target
	}
	for _, filter := range policy.Publish {
		index, ok := servicePublishNumberIndex(filter)
		if !ok {
			continue
		}
		parts := strings.Split(filter, "/")
		if index >= len(parts) {
			return brokercontract.Policy{}, errors.New("invalid scoped service filter")
		}
		target, exists := byNumber[parts[index]]
		if !exists {
			continue
		}
		policy.Publish = append(policy.Publish, strings.TrimSuffix(target.TopicPrefix, "/")+"/"+filter)
		if target.LegacyTelemetryID && len(parts) == 4 && parts[1] == "telemetry" && parts[2] == "control" {
			// The legacy protocol SDK always uses the devices wire family,
			// including protocol targets whose canonical family is gateway.
			policy.Publish = append(policy.Publish, target.TopicPrefix+"devices/telemetry/control/"+target.DeviceID)
		}

	}
	policy = normalizePolicy(policy)
	if brokercontract.ValidatePolicy(policy) != nil {
		return brokercontract.Policy{}, errors.New("invalid plugin publish alias")
	}
	return policy, nil
}

func buildPluginPolicy(targets []brokercontract.PluginTarget) (brokercontract.Policy, error) {
	if !validPluginTargets(targets) {
		return brokercontract.Policy{}, errors.New("invalid plugin targets")
	}
	publish := []string{}
	subscribe := []string{}
	for _, target := range targets {
		prefix := strings.TrimSuffix(target.TopicPrefix, "/")
		deviceFamily := "devices"
		if target.Kind == brokercontract.PrincipalGateway {
			deviceFamily = "gateway"
		}
		up := []string{deviceFamily + "/telemetry", deviceFamily + "/event/+", deviceFamily + "/attributes/+", deviceFamily + "/status/" + target.DeviceID,
			"gateway/status/" + target.DeviceID,
			"devices/telemetry", "devices/event/+", "devices/attributes/+", "devices/status/" + target.DeviceID,
			deviceFamily + "/command/response/+", deviceFamily + "/attributes/set/response/+"}
		up = append(up, "devices/command/response/+", "devices/attributes/set/response/+")
		down := []string{deviceFamily + "/telemetry/control/" + target.DeviceNumber, deviceFamily + "/attributes/set/" + target.DeviceNumber + "/+",
			deviceFamily + "/attributes/get/" + target.DeviceNumber, deviceFamily + "/command/" + target.DeviceNumber + "/+",
			deviceFamily + "/event/response/" + target.DeviceNumber + "/+", deviceFamily + "/attributes/response/" + target.DeviceNumber + "/+"}
		for _, topic := range up {
			publish = append(publish, topic, prefix+"/"+topic)
		}
		for _, topic := range down {
			subscribe = append(subscribe, topic, prefix+"/"+topic)
		}
		if target.LegacyTelemetryID {
			subscribe = append(subscribe, prefix+"/devices/telemetry/control/"+target.DeviceID)
		}

	}
	policy := normalizePolicy(brokercontract.Policy{Publish: publish, Subscribe: subscribe, MaxPayload: brokercontract.MaxPayloadBytes, MaxQoS: 1, RetainProfile: brokercontract.RetainedLiveIngress})
	if brokercontract.ValidatePolicy(policy) != nil {
		return brokercontract.Policy{}, errors.New("invalid plugin policy")
	}
	return policy, nil
}

func pluginContextID(principal brokercontract.Principal, cell, instance, connectionID string, targets []brokercontract.PluginTarget) (string, error) {
	return brokercontract.PluginContextID(principal, cell, instance, connectionID, targets)
}

func (o *Owner) persistPluginGrant(ctx context.Context, grant *brokercontract.Grant, cell, instance string, targets []brokercontract.PluginTarget) error {
	writer, ok := o.repo.(pluginContextWriter)
	if !ok {
		return ErrUnavailable
	}
	id, err := pluginContextID(grant.Principal, cell, instance, grant.ConnectionID, targets)
	if err != nil {
		return ErrUnavailable
	}
	grant.PluginContextID = id
	grant.PluginTargets = targets
	response, err := json.Marshal(*grant)
	if err != nil || len(response) > MaxGrantResponseBytes {
		return ErrUnavailable
	}
	if err := writer.StorePluginContext(ctx, PluginContext{ID: id, Principal: grant.Principal, BrokerCell: cell, BrokerInstance: instance,
		ConnectionID: grant.ConnectionID, Targets: targets, IssuedAt: grant.ExpiresAt.Add(-o.ttl), ExpiresAt: grant.ExpiresAt}); err != nil {
		return ErrUnavailable
	}
	return nil
}
