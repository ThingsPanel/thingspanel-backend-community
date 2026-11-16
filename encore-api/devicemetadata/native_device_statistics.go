package devicemetadata

import (
	"context"
	"database/sql"
	"net/http"
	"time"

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
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if query.values.Has("firmwareVersion") {
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

	rows, err := tx.QueryContext(ctx, filter.cte+`SELECT d.id::text,d.administrative_state,d.activation_state,b.runtime_namespace,b.runtime_external_id,`+ungrouped+nativeDeviceFromSQL+filter.predicate(), filter.args...)
	if err != nil {
		return nil, wrapDBErr(err, "read statistics candidates failed")
	}
	defer rows.Close()
	result := NativeDeviceStatistics{}
	var online, offline, affected, alarms int64
	runtimeComplete, alarmComplete := true, true
	batch := make([]NativeDevice, 0, 200)
	groups := map[string]bool{}
	flush := func() error {
		if err := attachDeviceSummaries(ctx, batch, query); err != nil {
			return err
		}
		for _, d := range batch {
			if !matchesDeviceSummary(d, query) {
				continue
			}
			result.Total++
			if d.AdministrativeState == "enabled" {
				result.Enabled++
			} else {
				result.Disabled++
			}
			if d.ActivationState == "active" {
				result.Active++
			} else {
				result.Inactive++
			}
			if groups[d.ID] {
				result.Ungrouped++
			}
			r, a := d.RuntimeSummary, d.AlarmSummary
			if r == nil || !r.Available {
				runtimeComplete = false
				result.Runtime.Unknown++
			} else {
				switch r.Connectivity {
				case "online":
					online++
				case "offline":
					offline++
				default:
					result.Runtime.Unknown++
				}
				if r.ObservedAt != nil {
					v := r.ObservedAt.UTC().Format(time.RFC3339Nano)
					result.Runtime.ObservedAt = &v
				}
			}
			if a == nil || !a.Available || a.ActiveCount == nil {
				alarmComplete = false
			} else {
				alarms += *a.ActiveCount
				if *a.ActiveCount > 0 {
					affected++
				}
				if a.ObservedAt != nil {
					v := a.ObservedAt.UTC().Format(time.RFC3339Nano)
					result.Alarms.ObservedAt = &v
				}
			}
		}
		batch = batch[:0]
		clear(groups)
		return nil
	}
	for rows.Next() {
		var d NativeDevice
		var ns, external sql.NullString
		var ungrouped bool
		if err := rows.Scan(&d.ID, &d.AdministrativeState, &d.ActivationState, &ns, &external, &ungrouped); err != nil {
			return nil, err
		}
		if ns.Valid && external.Valid {
			d.RuntimeRef = &RuntimeRef{Namespace: ns.String, ExternalID: external.String}
		}
		batch = append(batch, d)
		groups[d.ID] = ungrouped
		if len(batch) == 200 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDBErr(err, "read statistics failed")
	}
	if err := flush(); err != nil {
		return nil, err
	}
	rows.Close()
	if runtimeComplete {
		result.Runtime.Online = &online
		result.Runtime.Offline = &offline
	} else {
		result.Runtime.ObservedAt = nil
	}
	if alarmComplete {
		result.Alarms.AffectedDevices = &affected
		result.Alarms.ActiveAlarms = &alarms
	} else {
		result.Alarms.ObservedAt = nil
	}
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
	result, err := readNativeDeviceStatistics(nativeRuntimeReadContext(r), identity, query, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, result.ETag, result)
}
