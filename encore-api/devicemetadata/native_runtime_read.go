package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"encore.app/internal/legacy"
	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
)

// Runtime reads (runtime-state, readings) are served from the Community backend
// through internal/legacy. The Community device ID is taken from the device's
// runtimeRef (namespace TP_RUNTIME_COMMUNITY_NAMESPACE, default "community");
// without one the native UUID is used only when TP_RUNTIME_DEVICE_ID_MODE=identity.
// Every request first proves the device exists in the caller's tenant natively; the
// Community call only runs afterwards. Fields the Community backend cannot supply
// (applied generation, snapshot, aggregation windows, history totals) are reported
// as null or refused; they are never invented.

const (
	runtimeIdentityModeEnv = "TP_RUNTIME_DEVICE_ID_MODE"
	// runtimeNamespaceEnv names the runtimeRef.namespace whose externalId is the
	// Community device ID. Default "community".
	runtimeNamespaceEnv = "TP_RUNTIME_COMMUNITY_NAMESPACE"
)

var (
	runtimeLegacyOnce   sync.Once
	runtimeLegacyClient *legacy.Client
)

func nativeRuntimeLegacy() *legacy.Client {
	runtimeLegacyOnce.Do(func() { runtimeLegacyClient = legacy.NewFromEnv() })
	return runtimeLegacyClient
}

// resolveRuntimeDeviceID picks the Community device ID for a native device:
//  1. a runtimeRef in the configured Community namespace is the provider-confirmed
//     identity and wins;
//  2. otherwise the native UUID is used only when the operator declared
//     TP_RUNTIME_DEVICE_ID_MODE=identity;
//  3. otherwise the device has no known Community identity and the call is refused.
func resolveRuntimeDeviceID(ref *RuntimeRef, nativeID string) (string, error) {
	namespace := strings.TrimSpace(os.Getenv(runtimeNamespaceEnv))
	if namespace == "" {
		namespace = "community"
	}
	if ref != nil && ref.Namespace == namespace && strings.TrimSpace(ref.ExternalID) != "" {
		return ref.ExternalID, nil
	}
	if strings.TrimSpace(os.Getenv(runtimeIdentityModeEnv)) == "identity" {
		return nativeID, nil
	}
	return "", &errs.Error{Code: errs.Unavailable, Message: "device has no Community runtime identity: bind a runtimeRef in namespace " + namespace + " or set " + runtimeIdentityModeEnv + "=identity"}
}

// loadRuntimeDeviceID proves the device exists in the caller's tenant (and is
// visible to the principal) before any Community call, then resolves its identity.
func loadRuntimeDeviceID(ctx context.Context, identity nativeDeviceReadIdentity, deviceID string) (string, error) {
	if !identity.permits(deviceID) {
		return "", &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return "", wrapDBErr(err, "read device runtime identity failed")
	}
	defer tx.Rollback()
	device, err := getNativeDeviceRecordTx(ctx, tx, identity.tenantID, deviceID)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", wrapDBErr(err, "read device runtime identity failed")
	}
	return resolveRuntimeDeviceID(device.RuntimeRef, deviceID)
}

func runtimeUpstreamError(err error) error {
	// The upstream body may contain internal detail; do not forward it.
	return &errs.Error{Code: errs.Unavailable, Message: "device runtime backend is unavailable or returned an unrecognized response"}
}

// ---- runtime-state ----

type NativeRuntimeState struct {
	DeviceID                       string             `json:"deviceId"`
	Connectivity                   string             `json:"connectivity"`
	ObservedAt                     *string            `json:"observedAt"`
	LastSeenAt                     *string            `json:"lastSeenAt"`
	Reason                         *string            `json:"reason"`
	DesiredConfigurationGeneration int64              `json:"desiredConfigurationGeneration"`
	AppliedConfigurationGeneration *int64             `json:"appliedConfigurationGeneration"`
	AppliedTemplateID              *string            `json:"appliedTemplateId"`
	AppliedModelBinding            *ThingModelBinding `json:"appliedModelBinding"`
}

type NativeRuntimeStateResponse struct {
	Data NativeRuntimeState `json:"data"`
	Meta RequestMeta        `json:"meta"`
}

func readNativeRuntimeState(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, requestID string) (*NativeRuntimeStateResponse, error) {
	binding, err := readNativeDeviceTemplateBinding(ctx, identity, deviceID, requestID)
	if err != nil {
		return nil, err
	}
	legacyID, err := loadRuntimeDeviceID(ctx, identity, deviceID)
	if err != nil {
		return nil, err
	}
	flag, err := nativeRuntimeLegacy().GetOnlineFlag(ctx, legacyID)
	if err != nil {
		return nil, runtimeUpstreamError(err)
	}
	state := NativeRuntimeState{
		DeviceID:                       deviceID,
		Connectivity:                   "offline",
		DesiredConfigurationGeneration: binding.Data.ConfigurationGeneration,
		Reason:                         stringPointer("Community is_online flag; no observation time or applied generation is available"),
	}
	if flag == 1 {
		state.Connectivity = "online"
	}
	return &NativeRuntimeStateResponse{Data: state, Meta: RequestMeta{RequestID: requestID}}, nil
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/runtime-state
func (s *Service) GetNativeDeviceRuntimeState(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	response, err := readNativeRuntimeState(r.Context(), identity, deviceID, requestID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}

// ---- readings ----

type NativeReading struct {
	DeviceID     string          `json:"deviceId"`
	Identifier   string          `json:"identifier"`
	Kind         string          `json:"kind"`
	Value        json.RawMessage `json:"value"`
	ObservedAt   string          `json:"observedAt"`
	SnapshotID   *string         `json:"snapshotId"`
	SampleID     string          `json:"sampleId"`
	CacheVersion *string         `json:"cacheVersion"`
	Aggregation  string          `json:"aggregation"`
	WindowStart  *string         `json:"windowStart"`
	WindowEnd    *string         `json:"windowEnd"`
	SampleCount  *int64          `json:"sampleCount"`
}

type NativeReadingPage struct {
	Items    []NativeReading `json:"items"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Total    int64           `json:"total"`
}

type NativeReadingPageResponse struct {
	Data NativeReadingPage `json:"data"`
	Meta RequestMeta       `json:"meta"`
}

type nativeReadingQuery struct {
	kind       string
	identifier string
	latestOnly bool
	// from/to are set for raw history reads; the range is [from, to).
	from, to    *time.Time
	aggregation string // "raw" unless an aggregated history is asked for
	window      string
	page        int
	pageSize    int
}

var nativeReadingQueryKeys = []string{"page", "pageSize", "kind", "identifier", "from", "to", "latestOnly", "aggregation", "window"}

func decodeNativeReadingQuery(values map[string][]string) (nativeReadingQuery, error) {
	get := func(k string) string {
		if v, ok := values[k]; ok && len(v) == 1 {
			return v[0]
		}
		return ""
	}
	has := func(k string) bool { _, ok := values[k]; return ok }
	out := nativeReadingQuery{kind: "telemetry"}
	var err error
	out.page, out.pageSize, err = normalizeDeviceGroupPagination(get("page"), get("pageSize"))
	if err != nil {
		return out, err
	}
	if has("kind") {
		out.kind = get("kind")
	}
	switch out.kind {
	case "telemetry", "attribute", "event":
	default:
		return out, invalidGroupQuery("kind must be telemetry, attribute or event")
	}
	out.identifier = get("identifier")
	if has("identifier") && (out.identifier == "" || utf8.RuneCountInString(out.identifier) > 255) {
		return out, invalidGroupQuery("invalid identifier")
	}
	switch get("latestOnly") {
	case "":
	case "true":
		out.latestOnly = true
	case "false":
	default:
		return out, invalidGroupQuery("latestOnly must be true or false")
	}
	out.aggregation = "raw"
	if has("aggregation") {
		out.aggregation = get("aggregation")
		if !validAggregation(out.aggregation) {
			return out, invalidGroupQuery("aggregation must be raw, min, max, avg, sum, diff or last")
		}
	}
	if has("window") {
		out.window = get("window")
		if _, ok := readingWindows[out.window]; !ok {
			return out, invalidGroupQuery("window must be one of 30s, 1m, 2m, 5m, 10m, 30m, 1h, 3h, 6h, 1d, 7d, 30d")
		}
	}
	aggregated := out.aggregation != "raw"
	if aggregated != (out.window != "") {
		return out, invalidGroupQuery("window is required for aggregated readings and not allowed for raw ones")
	}
	if has("from") || has("to") || aggregated {
		if out.latestOnly {
			return out, invalidGroupQuery("latestOnly cannot be combined with from, to or aggregation")
		}
		if !has("from") || !has("to") {
			return out, invalidGroupQuery("from and to must be provided together")
		}
		if out.kind != "telemetry" {
			return out, &errs.Error{Code: errs.InvalidArgument, Message: "history is only available for telemetry readings"}
		}
		if out.identifier == "" {
			return out, invalidGroupQuery("telemetry history requires identifier")
		}
		from, err := time.Parse(time.RFC3339Nano, get("from"))
		if err != nil {
			return out, invalidGroupQuery("from must be an RFC 3339 timestamp")
		}
		to, err := time.Parse(time.RFC3339Nano, get("to"))
		if err != nil {
			return out, invalidGroupQuery("to must be an RFC 3339 timestamp")
		}
		from, to = from.UTC().Truncate(time.Millisecond), to.UTC().Truncate(time.Millisecond)
		if !from.Before(to) {
			return out, invalidGroupQuery("from must be before to")
		}
		if aggregated {
			if to.Sub(from) > aggregateMaxRange {
				return out, invalidGroupQuery("aggregated ranges are limited to 366 days")
			}
		} else if to.Sub(from) > 24*time.Hour {
			return out, invalidGroupQuery("raw history ranges are limited to 24 hours")
		}
		out.from, out.to = &from, &to
		return out, nil
	}
	if (out.kind == "telemetry" || out.kind == "attribute") && !out.latestOnly {
		return out, &errs.Error{Code: errs.InvalidArgument, Message: "pass latestOnly=true for the latest values, or from and to with an identifier for telemetry history"}
	}
	return out, nil
}

func rfc3339Millis(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func readNativeReadings(ctx context.Context, identity nativeDeviceReadIdentity, query nativeReadingQuery, deviceID, requestID string) (*NativeReadingPageResponse, error) {
	legacyID, err := loadRuntimeDeviceID(ctx, identity, deviceID)
	if err != nil {
		return nil, err
	}
	cli := nativeRuntimeLegacy()
	page := NativeReadingPage{Items: []NativeReading{}, Page: query.page, PageSize: query.pageSize}
	switch query.kind {
	case "telemetry", "attribute":
		if query.from != nil && query.aggregation != "raw" {
			rows, err := fetchAllTelemetryHistory(ctx, cli, legacyID, query.identifier, *query.from, *query.to)
			if err != nil {
				return nil, err
			}
			buckets, err := aggregateHistory(deviceID, query.identifier, query.aggregation, query.window, rows)
			if err != nil {
				return nil, err
			}
			page.Total = int64(len(buckets))
			start := (query.page - 1) * query.pageSize
			if start < len(buckets) {
				end := start + query.pageSize
				if end > len(buckets) {
					end = len(buckets)
				}
				page.Items = append(page.Items, buckets[start:end]...)
			}
			break
		}
		if query.from != nil {
			// Community's range is inclusive on whole milliseconds, so to-1ms
			// yields the contract's half-open [from, to).
			rows, total, err := cli.ListTelemetryHistory(ctx, legacyID, query.identifier,
				query.from.UnixMilli(), query.to.UnixMilli()-1, query.page, query.pageSize)
			if err != nil {
				return nil, runtimeUpstreamError(err)
			}
			page.Total = total
			for _, row := range rows {
				observed := rfc3339Millis(row.Ts)
				page.Items = append(page.Items, NativeReading{
					DeviceID: deviceID, Identifier: row.Key, Kind: "telemetry", Value: row.Value,
					ObservedAt: observed, SampleID: "legacy-history:" + row.Key + ":" + observed, Aggregation: "raw",
				})
			}
			break
		}
		fetch := cli.GetTelemetryCurrentReadings
		if query.kind == "attribute" {
			fetch = cli.GetAttributeCurrentReadings
		}
		rows, err := fetch(ctx, legacyID)
		if err != nil {
			return nil, runtimeUpstreamError(err)
		}
		filtered := rows[:0:0]
		for _, row := range rows {
			if query.identifier == "" || row.Key == query.identifier {
				filtered = append(filtered, row)
			}
		}
		page.Total = int64(len(filtered))
		start := (query.page - 1) * query.pageSize
		for i := start; i < len(filtered) && i < start+query.pageSize; i++ {
			row := filtered[i]
			observed := rfc3339Millis(row.Ts)
			page.Items = append(page.Items, NativeReading{
				DeviceID: deviceID, Identifier: row.Key, Kind: query.kind, Value: row.Value,
				ObservedAt: observed, SampleID: "legacy-current:" + row.Key + ":" + observed, Aggregation: "last",
			})
		}
	case "event":
		rows, total, err := cli.ListEventDatas(ctx, legacyID, query.identifier, query.page, query.pageSize)
		if err != nil {
			return nil, runtimeUpstreamError(err)
		}
		page.Total = total
		for _, row := range rows {
			value := json.RawMessage("null")
			if row.Datum != nil {
				if json.Valid([]byte(*row.Datum)) {
					value = json.RawMessage(*row.Datum)
				} else if encoded, err := json.Marshal(*row.Datum); err == nil {
					value = encoded
				}
			}
			page.Items = append(page.Items, NativeReading{
				DeviceID: deviceID, Identifier: row.Identify, Kind: "event", Value: value,
				ObservedAt: rfc3339Millis(row.T), SampleID: row.ID, Aggregation: "raw",
			})
		}
	}
	return &NativeReadingPageResponse{Data: page, Meta: RequestMeta{RequestID: requestID}}, nil
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/readings
func (s *Service) ListNativeDeviceReadings(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	values, err := parseNativeReadValues(r, nativeReadingQueryKeys)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	query, err := decodeNativeReadingQuery(values)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	response, err := readNativeReadings(r.Context(), identity, query, deviceID, requestID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}

// fetchAllTelemetryHistory reads every raw sample in [from, to) page by page.
// A range holding more than aggregateMaxRows samples is refused.
func fetchAllTelemetryHistory(ctx context.Context, cli *legacy.Client, legacyID, identifier string, from, to time.Time) ([]legacy.HistoryReading, error) {
	const pageSize = 1000
	var all []legacy.HistoryReading
	for page := 1; ; page++ {
		rows, total, err := cli.ListTelemetryHistory(ctx, legacyID, identifier, from.UnixMilli(), to.UnixMilli()-1, page, pageSize)
		if err != nil {
			return nil, runtimeUpstreamError(err)
		}
		if total > aggregateMaxRows {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "the range holds more than " + strconv.Itoa(aggregateMaxRows) + " samples; narrow the time range"}
		}
		all = append(all, rows...)
		if len(rows) == 0 || int64(page)*pageSize >= total {
			return all, nil
		}
	}
}
