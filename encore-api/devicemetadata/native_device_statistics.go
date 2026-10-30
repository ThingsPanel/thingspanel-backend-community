package devicemetadata

import (
	"context"
	"database/sql"
	"net/http"

	"encore.dev/beta/errs"
)

type NativeDeviceStatistics struct {
	Total     int64                    `json:"total"`
	Enabled   int64                    `json:"enabled"`
	Disabled  int64                    `json:"disabled"`
	Active    int64                    `json:"active"`
	Inactive  int64                    `json:"inactive"`
	Ungrouped int64                    `json:"ungrouped"`
	Runtime   NativeDeviceRuntimeStats `json:"runtime"`
	Alarms    NativeDeviceAlarmStats   `json:"alarms"`
}

type NativeDeviceRuntimeStats struct {
	Online     *int64  `json:"online"`
	Offline    *int64  `json:"offline"`
	Unknown    int64   `json:"unknown"`
	ObservedAt *string `json:"observedAt"`
}

type NativeDeviceAlarmStats struct {
	AffectedDevices *int64  `json:"affectedDevices"`
	ActiveAlarms    *int64  `json:"activeAlarms"`
	ObservedAt      *string `json:"observedAt"`
}

type NativeDeviceStatisticsResponse struct {
	Data NativeDeviceStatistics `json:"data"`
	Meta RequestMeta            `json:"meta"`
	ETag string                 `json:"-" header:"ETag"`
}

var nativeDeviceStatisticsQueryKeys = func() []string {
	keys := make([]string, 0, len(nativeDeviceReadQueryKeys)-2)
	for _, key := range nativeDeviceReadQueryKeys {
		if key != "page" && key != "pageSize" {
			keys = append(keys, key)
		}
	}
	return keys
}()

func parseNativeDeviceStatisticsQuery(r *http.Request) (nativeDeviceListQuery, error) {
	values, err := parseNativeReadValues(r, nativeDeviceStatisticsQueryKeys)
	if err != nil {
		return nativeDeviceListQuery{}, err
	}
	query, err := decodeNativeDeviceListQuery(values)
	if err != nil {
		return nativeDeviceListQuery{}, err
	}
	return query, nil
}

func readNativeDeviceStatistics(ctx context.Context, identity nativeDeviceReadIdentity, query nativeDeviceListQuery, requestID string) (*NativeDeviceStatisticsResponse, error) {
	// These filters require authoritative owner snapshots. Until those owners
	// are connected, reject rather than turning unavailable observations into 0.
	if query.values.Has("connectivity") || query.values.Has("firmwareVersion") || (query.values.Has("alarmState") && query.values.Get("alarmState") != "any") {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "authoritative runtime filter snapshot is unavailable"}
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "read device statistics failed")
	}
	defer tx.Rollback()
	if group := query.values.Get("groupId"); group != "" {
		if err := requireNativeGroupRead(ctx, tx, identity.tenantID, group); err != nil {
			return nil, err
		}
	}
	if err := requireNativeDeviceReadiness(ctx, tx, identity); err != nil {
		return nil, err
	}
	filter := nativeDeviceBaseFilter(identity)
	addNativeDeviceFilters(&filter, query)
	ungrouped := "NOT EXISTS(SELECT 1 FROM device_group_members m JOIN device_groups g ON g.tenant_id=m.tenant_id AND g.id=m.group_id WHERE m.tenant_id=d.tenant_id AND m.device_id=d.id AND g.deleted_at IS NULL)"
	sqlQuery := filter.cte + `SELECT
	COUNT(*)::bigint,
	COUNT(*) FILTER (WHERE d.administrative_state='enabled')::bigint,
	COUNT(*) FILTER (WHERE d.administrative_state='disabled')::bigint,
	COUNT(*) FILTER (WHERE d.activation_state='active')::bigint,
	COUNT(*) FILTER (WHERE d.activation_state='inactive')::bigint,
	COUNT(*) FILTER (WHERE ` + ungrouped + `)::bigint
	` + nativeDeviceFromSQL + filter.predicate()
	result := NativeDeviceStatistics{
		Runtime: NativeDeviceRuntimeStats{Unknown: 0},
	}
	if err := tx.QueryRowContext(ctx, sqlQuery, filter.args...).Scan(&result.Total, &result.Enabled, &result.Disabled, &result.Active, &result.Inactive, &result.Ungrouped); err != nil {
		return nil, wrapDBErr(err, "aggregate device statistics failed")
	}
	// Runtime and alarm observations have not been connected to their owners.
	// Unknown is therefore the full filtered denominator; null means unavailable.
	result.Runtime.Unknown = result.Total
	response := &NativeDeviceStatisticsResponse{Data: result, Meta: RequestMeta{RequestID: requestID}}
	response.ETag = groupJSONETag(result)
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "read device statistics failed")
	}
	return response, nil
}

//encore:api auth raw method=GET path=/api/v2/device-statistics
func (s *Service) GetNativeDeviceStatistics(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	query, err := parseNativeDeviceStatisticsQuery(r)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	result, err := readNativeDeviceStatistics(r.Context(), identity, query, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, result.ETag, result)
}
