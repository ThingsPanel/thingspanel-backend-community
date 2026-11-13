package automation

import (
	"context"
	"strings"

	"encore.app/automation/runtime"
	"encore.dev/beta/errs"
)

// Browser-authenticated diagnostic ingress does not carry the trusted owner's
// durable device receipt. Verify its device scope before it can update state.
// The owner-only path retains its existing authenticated ingress contract.
func requireUserIngressDevices(ctx context.Context, rt *runtime.Runtime, tenantID string, ids ...string) error {
	directory := rt.Directory()
	if directory == nil {
		return &errs.Error{Code: errs.Unavailable, Message: "设备归属暂时无法验证，请稍后重试"}
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) {
			return &errs.Error{Code: errs.InvalidArgument, Message: "设备 ID 无效"}
		}
		wanted[id] = true
	}
	devices, err := directory.ByIDs(ctx, tenantID, ids)
	if err != nil {
		return &errs.Error{Code: errs.Unavailable, Message: "设备归属暂时无法验证，请稍后重试"}
	}
	for _, device := range devices {
		if device.TenantID == tenantID {
			delete(wanted, device.ID)
		}
	}
	if len(wanted) != 0 {
		return &errs.Error{Code: errs.NotFound, Message: "设备不存在或不可访问"}
	}
	return nil
}
