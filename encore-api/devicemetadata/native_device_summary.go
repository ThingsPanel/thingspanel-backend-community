package devicemetadata

import (
	"context"
	"encore.app/internal/communityclient"
	"encore.dev"
	"encore.dev/beta/errs"
	"time"
)

type NativeRuntimeSummary struct {
	Connectivity    string     `json:"connectivity"`
	ObservedAt      *time.Time `json:"observedAt"`
	LastTelemetryAt *time.Time `json:"lastTelemetryAt"`
	Available       bool       `json:"available"`
}
type NativeAlarmSummary struct {
	State       string     `json:"state"`
	ActiveCount *int64     `json:"activeCount"`
	ObservedAt  *time.Time `json:"observedAt"`
	Available   bool       `json:"available"`
}
type deviceSummaryReader interface {
	GetRuntimeSummaries(context.Context, []string) ([]communityclient.RuntimeSummary, error)
	GetAlarmSummaries(context.Context, string, []string) ([]communityclient.AlarmSummary, error)
}

func hasDeviceSummaryFilters(q nativeDeviceListQuery) bool {
	return q.values.Has("connectivity") || (q.values.Get("alarmState") != "" && q.values.Get("alarmState") != "any")
}
func attachDeviceSummaries(ctx context.Context, items []NativeDevice, q nativeDeviceListQuery) error {
	base := ""
	if meta := encore.Meta(); meta != nil {
		base = meta.APIBaseURL.String()
	}
	return attachDeviceSummariesWithReader(ctx, items, q, nativeRuntimeClient(), base)
}
func attachDeviceSummariesWithReader(ctx context.Context, items []NativeDevice, q nativeDeviceListQuery, reader deviceSummaryReader, alarmBase string) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	runtimeIDs := make([]string, 0, len(items))
	runtimeMap := map[string][]int{}
	for i := range items {
		items[i].RuntimeSummary = &NativeRuntimeSummary{Connectivity: "unknown"}
		items[i].AlarmSummary = &NativeAlarmSummary{State: "unknown"}
		ids = append(ids, items[i].ID)
		if id, err := resolveRuntimeDeviceID(items[i].RuntimeRef, items[i].ID); err == nil {
			if len(runtimeMap[id]) == 0 {
				runtimeIDs = append(runtimeIDs, id)
			}
			runtimeMap[id] = append(runtimeMap[id], i)
		}
	}
	type runtimeResult struct {
		rows []communityclient.RuntimeSummary
		err  error
	}
	type alarmResult struct {
		rows []communityclient.AlarmSummary
		err  error
	}
	rc := make(chan runtimeResult, 1)
	ac := make(chan alarmResult, 1)
	go func() {
		if len(runtimeIDs) == 0 {
			rc <- runtimeResult{}
			return
		}
		rows, err := reader.GetRuntimeSummaries(ctx, runtimeIDs)
		rc <- runtimeResult{rows, err}
	}()
	go func() { rows, err := reader.GetAlarmSummaries(ctx, alarmBase, ids); ac <- alarmResult{rows, err} }()
	var rr runtimeResult
	var ar alarmResult
	select {
	case rr = <-rc:
	case <-ctx.Done():
		return &errs.Error{Code: errs.Unavailable, Message: "device summary deadline exceeded"}
	}
	select {
	case ar = <-ac:
	case <-ctx.Done():
		return &errs.Error{Code: errs.Unavailable, Message: "device summary deadline exceeded"}
	}
	runtimeValid := rr.err == nil
	seen := map[string]bool{}
	for _, row := range rr.rows {
		if len(runtimeMap[row.DeviceID]) == 0 || seen[row.DeviceID] || !row.Available || row.ObservedAt == nil || (row.Connectivity != "online" && row.Connectivity != "offline" && row.Connectivity != "unknown") {
			runtimeValid = false
			break
		}
		seen[row.DeviceID] = true
	}
	if len(seen) != len(runtimeIDs) {
		runtimeValid = false
	}
	if runtimeValid {
		for _, row := range rr.rows {
			for _, i := range runtimeMap[row.DeviceID] {
				items[i].RuntimeSummary = &NativeRuntimeSummary{Connectivity: row.Connectivity, ObservedAt: row.ObservedAt, LastTelemetryAt: row.LastTelemetryAt, Available: true}
			}
		}
	}
	indexes := map[string]int{}
	for i, id := range ids {
		indexes[id] = i
	}
	seen = map[string]bool{}
	alarmValid := ar.err == nil
	for _, row := range ar.rows {
		_, ok := indexes[row.DeviceID]
		if !ok || seen[row.DeviceID] || !row.Available || row.ObservedAt == nil || row.ActiveCount == nil || *row.ActiveCount < 0 || (row.State != "active" && row.State != "none") || (row.State == "active") != (*row.ActiveCount > 0) {
			alarmValid = false
			break
		}
		seen[row.DeviceID] = true
	}
	if len(seen) != len(ids) {
		alarmValid = false
	}
	if alarmValid {
		for _, row := range ar.rows {
			items[indexes[row.DeviceID]].AlarmSummary = &NativeAlarmSummary{State: row.State, ActiveCount: row.ActiveCount, ObservedAt: row.ObservedAt, Available: true}
		}
	}
	if (q.values.Has("connectivity") && !runtimeValid) || (q.values.Get("alarmState") != "" && q.values.Get("alarmState") != "any" && !alarmValid) {
		return &errs.Error{Code: errs.Unavailable, Message: "device status filter owner is unavailable"}
	}
	return nil
}
func matchesDeviceSummary(d NativeDevice, q nativeDeviceListQuery) bool {
	if wanted := q.values.Get("connectivity"); wanted != "" && (d.RuntimeSummary == nil || d.RuntimeSummary.Connectivity != wanted) {
		return false
	}
	if wanted := q.values.Get("alarmState"); wanted != "" && wanted != "any" && (d.AlarmSummary == nil || d.AlarmSummary.State != wanted) {
		return false
	}
	return true
}
