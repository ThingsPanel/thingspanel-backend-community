package brokercontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	for i := range rows {
		if !allowedID[rows[i].DeviceID] || (len(allowedTenant) > 0 && !allowedTenant[rows[i].TenantID]) || !brokercontract.ValidPluginPrefix(rows[i].TopicPrefix) {
			return nil, nil
		}
		if i > 0 && rows[i-1].DeviceID == rows[i].DeviceID {
			return nil, nil
		}
		binding, bindErr := o.repo.ResolveActiveBinding(ctx, rows[i].TenantID, rows[i].DeviceID, cell)
		if admission, ok := o.repo.(interface {
			ResolveAdmissionBinding(context.Context, string, string, string) (TransportBinding, error)
		}); ok {
			binding, bindErr = admission.ResolveAdmissionBinding(ctx, rows[i].TenantID, rows[i].DeviceID, cell)
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
	}
	policy := normalizePolicy(brokercontract.Policy{Publish: publish, Subscribe: subscribe, MaxPayload: brokercontract.MaxPayloadBytes, MaxQoS: 1, RetainProfile: brokercontract.RetainedLiveIngress})
	if brokercontract.ValidatePolicy(policy) != nil {
		return brokercontract.Policy{}, errors.New("invalid plugin policy")
	}
	return policy, nil
}

func pluginContextID(principal brokercontract.Principal, cell, instance, connectionID string, targets []brokercontract.PluginTarget) (string, error) {
	targets = append([]brokercontract.PluginTarget(nil), targets...)
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].TenantID != targets[j].TenantID {
			return targets[i].TenantID < targets[j].TenantID
		}
		return targets[i].DeviceID < targets[j].DeviceID
	})
	data, err := json.Marshal(struct {
		Principal  brokercontract.Principal      `json:"principal"`
		Cell       string                        `json:"cell"`
		Instance   string                        `json:"instance"`
		Connection string                        `json:"connection"`
		Targets    []brokercontract.PluginTarget `json:"targets"`
	}{principal, cell, instance, connectionID, targets})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
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
