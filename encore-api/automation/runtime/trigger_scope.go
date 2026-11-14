package runtime

import (
	"context"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// TriggerScopeResult is shared by ingress, timers, simulation and the effective
// rule view. Nil means allowed. It intentionally never uses action Targets.
// Resolve the current native directory membership, rather than a binding cache:
// moving a device to another template must stop new triggers immediately.
func (r *Runtime) TriggerScopeResult(ctx context.Context, rule *store.Rule, deviceID string) *domain.Result {
	scope := rule.Definition.TriggerScope
	if scope == nil {
		return nil
	}
	blocked := func(code, message string) *domain.Result {
		return &domain.Result{Verdict: domain.VerdictUnknown, Reasons: []domain.Reason{{
			Code: code, Message: message, DeviceID: deviceID,
		}}}
	}
	if deviceID == "" {
		return blocked("trigger_scope_mismatch", "需要指定触发范围内的设备")
	}
	if r.dir == nil {
		return blocked("trigger_scope_unavailable", "设备目录不可用，无法确认触发范围")
	}
	devices, err := r.dir.ByIDs(ctx, rule.TenantID, []string{deviceID})
	if err != nil {
		return blocked("trigger_scope_unavailable", "设备目录暂时不可用，无法确认触发范围")
	}
	for _, device := range devices {
		if device.ID == deviceID && device.TenantID == rule.TenantID && scope.Matches(deviceID, device.DeviceConfigID) {
			return nil
		}
	}
	return blocked("trigger_scope_mismatch", "设备不在规则的触发范围内")
}
