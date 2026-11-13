package brokercontract

import "strings"

const MaxPluginTargets = 256

// PluginTarget is an owner-derived socket route, never a plugin-supplied claim.
// The legacy topic prefix and credentials remain external SDK contracts.
type PluginTarget struct {
	TenantID       string        `json:"tenant_id"`
	DeviceID       string        `json:"device_id"`
	DeviceNumber   string        `json:"device_number"`
	TopicPrefix    string        `json:"topic_prefix"`
	MappingVersion string        `json:"mapping_version"`
	Kind           PrincipalKind `json:"principal_kind"`
	RouteEpoch     uint64        `json:"route_epoch"`
	BindingPhase   string        `json:"binding_phase"`
}

func ValidPluginPrefix(prefix string) bool {
	if prefix == "" || len(prefix) > 512 || !strings.HasSuffix(prefix, "/") {
		return false
	}
	base := strings.TrimSuffix(prefix, "/")
	if ValidatePublishTopic(base) != nil || IsInternalResource(base) {
		return false
	}
	for _, part := range strings.Split(base, "/") {
		if part == "" {
			return false
		}
	}
	return true
}
func (p PluginTarget) Validate() error {
	if ValidatePrincipal(Principal{TenantID: p.TenantID, DeviceID: p.DeviceID, Kind: p.Kind}) != nil || p.Kind == PrincipalService ||
		!validIdentity(p.DeviceNumber) || strings.ContainsAny(p.DeviceNumber, "/+#") || !ValidPluginPrefix(p.TopicPrefix) ||
		!validIdentity(p.MappingVersion) || p.RouteEpoch == 0 || (p.BindingPhase != "active" && p.BindingPhase != "connecting") {
		return ErrInvalidMetadata
	}
	return nil
}
func validPluginContextID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func validatePluginGrant(g Grant) error {
	if g.PluginContextID == "" && len(g.PluginTargets) == 0 {
		return nil
	}
	if !validPluginContextID(g.PluginContextID) || len(g.PluginTargets) == 0 || len(g.PluginTargets) > MaxPluginTargets {
		return ErrInvalidMetadata
	}
	seen := map[string]bool{}
	for _, p := range g.PluginTargets {
		key := p.TenantID + "\x00" + p.DeviceID
		if p.Validate() != nil || seen[key] {
			return ErrInvalidMetadata
		}
		seen[key] = true
		if g.Principal.Kind != PrincipalService && (p.TenantID != g.Principal.TenantID || p.DeviceID != g.Principal.DeviceID) {
			return ErrUnauthorized
		}
	}
	return nil
}
