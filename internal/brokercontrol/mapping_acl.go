package brokercontrol

import (
	"context"
	"errors"
	"strings"

	"project/internal/brokercontract"
	"project/internal/brokermapper"
)

func instantiateMappingFilter(template, deviceID, deviceNumber string) (string, error) {
	if (!strings.Contains(template, "{device_number}") && !strings.Contains(template, "{device_id}")) || deviceNumber == "" || strings.Contains(deviceNumber, "/") || strings.Contains(deviceID, "/") {
		return "", errors.New("mapping source is not scoped to the target device identity")
	}
	filter := strings.ReplaceAll(template, "{device_number}", deviceNumber)
	filter = strings.ReplaceAll(filter, "{device_id}", deviceID)
	filter = strings.ReplaceAll(filter, "{message_id}", "+")
	if strings.ContainsAny(filter, "{}") || brokercontract.ValidateSubscribeFilter(filter) != nil || brokercontract.IsInternalResource(filter) {
		return "", errors.New("mapping source cannot be represented as a safe broker filter")
	}
	return filter, nil
}

func mappingProtectedDownlinks(snapshot *brokermapper.Snapshot, deviceID, deviceNumber string) []string {
	if snapshot == nil {
		return nil
	}
	filters := make([]string, 0)
	for _, rule := range snapshot.Rules() {
		if rule.Direction != "down" {
			continue
		}
		filter, err := instantiateMappingFilter(rule.SourceTopic, deviceID, deviceNumber)
		if err == nil {
			filters = append(filters, filter)
		}
	}
	return uniqueSorted(filters)
}

// addServiceMappingPublishAliases grants a backend service only the wire topic
// filters represented by rules for its already-scoped targets and only when
// the rule's canonical destination is already covered by that service policy.
func addServiceMappingPublishAliases(ctx context.Context, repo Repository, policy brokercontract.Policy, targets []DeviceRecord) (brokercontract.Policy, []string, error) {
	if repo == nil {
		return brokercontract.Policy{}, nil, errors.New("mapping repository is unavailable")
	}
	protected := make([]string, 0)
	for _, target := range targets {
		if target.DeviceConfigID == nil || *target.DeviceConfigID == "" {
			continue
		}
		snapshot, err := repo.CompileMappingSnapshot(ctx, target.TenantID, *target.DeviceConfigID)
		if err != nil {
			return brokercontract.Policy{}, nil, err
		}
		for _, rule := range snapshot.Rules() {
			if rule.Direction != "down" {
				continue
			}
			wireFilter, err := instantiateMappingFilter(rule.SourceTopic, target.ID, target.DeviceNumber)
			if err != nil {
				return brokercontract.Policy{}, nil, err
			}
			canonicalProbe := strings.ReplaceAll(rule.TargetTopic, "{device_number}", target.DeviceNumber)
			canonicalProbe = strings.ReplaceAll(canonicalProbe, "{device_id}", target.ID)
			canonicalProbe = strings.ReplaceAll(canonicalProbe, "{message_id}", "__mapping_probe__")
			canonicalProbe = strings.ReplaceAll(canonicalProbe, "+", "__mapping_probe__")
			if brokercontract.ValidatePublishTopic(canonicalProbe) != nil {
				return brokercontract.Policy{}, nil, errors.New("mapping target cannot be represented as a canonical command topic")
			}
			for _, allowed := range policy.Publish {
				if !brokercontract.FilterMatches(allowed, canonicalProbe) {
					continue
				}
				policy.Publish = append(policy.Publish, wireFilter)
				protected = append(protected, wireFilter)
				break
			}
		}
	}
	policy = normalizePolicy(policy)
	protected = uniqueSorted(protected)
	if err := brokercontract.ValidatePolicy(policy); err != nil {
		return brokercontract.Policy{}, nil, err
	}
	return policy, protected, nil
}
