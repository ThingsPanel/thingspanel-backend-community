package automation

import (
	"context"
	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"time"
)

type DeviceAlarmSummaryRequest struct {
	DeviceIDs []string `json:"deviceIds"`
}
type DeviceAlarmSummary struct {
	DeviceID    string    `json:"deviceId"`
	State       string    `json:"state"`
	ActiveCount int64     `json:"activeCount"`
	ObservedAt  time.Time `json:"observedAt"`
	Available   bool      `json:"available"`
}
type DeviceAlarmSummaryResponse struct {
	Items []DeviceAlarmSummary `json:"items"`
}

// DeviceAlarmSummaries reads the alarm owner's tenant-scoped snapshot.
//
//encore:api auth method=POST path=/v1/device-alarm-summaries
func (s *Service) DeviceAlarmSummaries(ctx context.Context, req *DeviceAlarmSummaryRequest) (*DeviceAlarmSummaryResponse, error) {
	tenant, err := mustTenant()
	if err != nil {
		return nil, err
	}
	identity := authn.CurrentData()
	if !authz.Can(identity, "device", "read") {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "device read permission required"}
	}
	if req == nil || len(req.DeviceIDs) == 0 || len(req.DeviceIDs) > 200 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid batch"}
	}
	allowed := map[string]bool{}
	for _, id := range identity.AuthorizedDeviceIDs {
		allowed[id] = true
	}
	seen := map[string]bool{}
	for _, id := range req.DeviceIDs {
		if id == "" || len(id) > 128 || seen[id] {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid batch"}
		}
		seen[id] = true
		if len(allowed) > 0 && !allowed[id] {
			return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
		}
	}
	// Resolve through the metadata owner before exposing even zero counts.
	devices, err := (nativeDirectory{}).ByIDs(ctx, tenant, req.DeviceIDs)
	if err != nil {
		return nil, err
	}
	found := make(map[string]bool, len(devices))
	for _, device := range devices {
		found[device.ID] = true
	}
	for _, id := range req.DeviceIDs {
		if !found[id] {
			return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
		}
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	counts, err := rt.Store().ActiveAlarmCounts(ctx, tenant, req.DeviceIDs)
	if err != nil {
		return nil, internal(err)
	}
	out := &DeviceAlarmSummaryResponse{Items: make([]DeviceAlarmSummary, 0, len(req.DeviceIDs))}
	now := time.Now().UTC()
	for _, id := range req.DeviceIDs {
		state := "none"
		if counts[id] > 0 {
			state = "active"
		}
		out.Items = append(out.Items, DeviceAlarmSummary{DeviceID: id, State: state, ActiveCount: counts[id], ObservedAt: now, Available: true})
	}
	return out, nil
}
