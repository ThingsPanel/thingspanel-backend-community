package devicemetadata

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
)

// Diagnostics report what native metadata and the runtime can actually prove.
// Stage checks are derived from native state. The uplink/downlink/storage
// counters need a runtime that deduplicates events and attempts per window; the
// Community adapter does not, so they are reported with complete=false and null
// counts instead of 0 or a made-up success rate. Failures come from the native
// command records inside the window.

type NativeDiagnosticCheck struct {
	Stage   string `json:"stage"`
	State   string `json:"state"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type NativeDiagnosticCounter struct {
	Success     *int64   `json:"success"`
	Total       *int64   `json:"total"`
	SuccessRate *float64 `json:"successRate"`
	Complete    bool     `json:"complete"`
}

type NativeDiagnosticFailure struct {
	ID        string  `json:"id"`
	Timestamp string  `json:"timestamp"`
	Direction string  `json:"direction"`
	Stage     string  `json:"stage"`
	Code      string  `json:"code"`
	Message   string  `json:"message"`
	RequestID *string `json:"requestId"`
}

type NativeDiagnosticStats struct {
	Uplink   NativeDiagnosticCounter `json:"uplink"`
	Downlink NativeDiagnosticCounter `json:"downlink"`
	Storage  NativeDiagnosticCounter `json:"storage"`
}

type NativeDeviceDiagnostic struct {
	DeviceID                string                    `json:"deviceId"`
	ObservedAt              string                    `json:"observedAt"`
	Checks                  []NativeDiagnosticCheck   `json:"checks"`
	WindowFrom              string                    `json:"windowFrom"`
	WindowTo                string                    `json:"windowTo"`
	Stats                   NativeDiagnosticStats     `json:"stats"`
	RecentFailures          []NativeDiagnosticFailure `json:"recentFailures"`
	RecentFailuresTruncated bool                      `json:"recentFailuresTruncated"`
}

type NativeDeviceDiagnosticResponse struct {
	Data NativeDeviceDiagnostic `json:"data"`
	Meta RequestMeta            `json:"meta"`
}

type nativeDiagnosticsQuery struct {
	from, to time.Time
	limit    int
}

var nativeDiagnosticsKeys = []string{"from", "to", "recentFailureLimit"}

const maxDiagnosticWindow = 31 * 24 * time.Hour

func decodeNativeDiagnosticsQuery(values map[string][]string, now time.Time) (nativeDiagnosticsQuery, error) {
	out := nativeDiagnosticsQuery{limit: 20}
	get := func(k string) (string, bool) {
		v, ok := values[k]
		if !ok {
			return "", false
		}
		if len(v) != 1 {
			return "", true
		}
		return v[0], true
	}
	from, hasFrom := get("from")
	to, hasTo := get("to")
	if hasFrom != hasTo {
		return out, invalidGroupQuery("from and to must be supplied together")
	}
	if hasFrom {
		var err error
		if out.from, err = time.Parse(time.RFC3339, from); err != nil {
			return out, invalidGroupQuery("from must be an RFC 3339 timestamp")
		}
		if out.to, err = time.Parse(time.RFC3339, to); err != nil {
			return out, invalidGroupQuery("to must be an RFC 3339 timestamp")
		}
		if !out.from.Before(out.to) {
			return out, invalidGroupQuery("from must be before to")
		}
		if out.to.Sub(out.from) > maxDiagnosticWindow {
			return out, invalidGroupQuery("the diagnostic window must not exceed 31 days")
		}
	} else {
		out.to = now
		out.from = now.Add(-24 * time.Hour)
	}
	if raw, ok := get("recentFailureLimit"); ok {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return out, invalidGroupQuery("recentFailureLimit must be between 1 and 100")
		}
		out.limit = limit
	}
	return out, nil
}

func incompleteCounter() NativeDiagnosticCounter { return NativeDiagnosticCounter{} }

// nativeConnectivity returns "online" or "offline"; an error means unknown.
type nativeConnectivity func(ctx context.Context) (string, error)

func readNativeDiagnostics(ctx context.Context, identity nativeDeviceReadIdentity, query nativeDiagnosticsQuery, deviceID, requestID string, now time.Time, connectivity nativeConnectivity) (*NativeDeviceDiagnosticResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "read device diagnostics failed")
	}
	defer tx.Rollback()
	device, err := getNativeDeviceRecordTx(ctx, tx, identity.tenantID, deviceID)
	if err != nil {
		return nil, err
	}
	var active, pending int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status='active'),count(*) FILTER (WHERE status='pending')
		FROM device_credentials WHERE tenant_id=$1 AND device_id=$2 AND purpose='device_access'`, identity.tenantID, deviceID).Scan(&active, &pending); err != nil {
		return nil, wrapDBErr(err, "read credential state failed")
	}
	var configured, configPending bool
	err = tx.QueryRowContext(ctx, `SELECT (template_id IS NOT NULL OR direct_access IS NOT NULL), pending_operation_id IS NOT NULL
		FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2`, identity.tenantID, deviceID).Scan(&configured, &configPending)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, wrapDBErr(err, "read access binding failed")
	}
	var lastCommand sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT state FROM device_command_executions WHERE tenant_id=$1 AND device_id=$2
		AND accepted_at>=$3 AND accepted_at<$4 ORDER BY accepted_at DESC, id DESC LIMIT 1`, identity.tenantID, deviceID, query.from, query.to).Scan(&lastCommand); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, wrapDBErr(err, "read command state failed")
	}
	failures := []NativeDiagnosticFailure{}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,accepted_at,state,COALESCE(reason,'') FROM device_command_executions
		WHERE tenant_id=$1 AND device_id=$2 AND accepted_at>=$3 AND accepted_at<$4 AND state IN ('failed','timed_out','expired')
		ORDER BY accepted_at DESC, id DESC LIMIT $5`, identity.tenantID, deviceID, query.from, query.to, query.limit+1)
	if err != nil {
		return nil, wrapDBErr(err, "read command failures failed")
	}
	defer rows.Close()
	for rows.Next() {
		var id, state, reason string
		var at time.Time
		if err := rows.Scan(&id, &at, &state, &reason); err != nil {
			return nil, wrapDBErr(err, "read command failures failed")
		}
		message := strings.TrimSpace(reason)
		if message == "" {
			message = "command " + state
		}
		if len(message) > 2048 {
			message = message[:2048]
		}
		failures = append(failures, NativeDiagnosticFailure{
			ID: id, Timestamp: rfc3339Millis(at), Direction: "downlink", Stage: "command",
			Code: "COMMAND_" + strings.ToUpper(state), Message: message,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDBErr(err, "read command failures failed")
	}
	truncated := len(failures) > query.limit
	if truncated {
		failures = failures[:query.limit]
	}

	checks := []NativeDiagnosticCheck{{Stage: "metadata", State: "pass", Code: "DEVICE_FOUND", Message: "device metadata is readable in this tenant"}}
	switch {
	case active > 0:
		checks = append(checks, NativeDiagnosticCheck{Stage: "credential", State: "pass", Code: "CREDENTIAL_ACTIVE", Message: "an active access credential exists"})
	case pending > 0:
		checks = append(checks, NativeDiagnosticCheck{Stage: "credential", State: "unknown", Code: "CREDENTIAL_PENDING", Message: "access credentials exist but none is active yet"})
	default:
		checks = append(checks, NativeDiagnosticCheck{Stage: "credential", State: "unknown", Code: "NO_CREDENTIAL", Message: "no access credential has been created"})
	}
	switch {
	case !configured:
		checks = append(checks, NativeDiagnosticCheck{Stage: "configuration", State: "fail", Code: "CONFIGURATION_INCOMPLETE", Message: "device access configuration is not set"})
	case configPending:
		checks = append(checks, NativeDiagnosticCheck{Stage: "configuration", State: "unknown", Code: "CONFIGURATION_PENDING", Message: "a configuration change is waiting for the runtime"})
	default:
		checks = append(checks, NativeDiagnosticCheck{Stage: "configuration", State: "pass", Code: "CONFIGURATION_SET", Message: "device access configuration is set"})
	}
	if state, err := connectivity(ctx); err != nil {
		checks = append(checks, NativeDiagnosticCheck{Stage: "connection", State: "unknown", Code: "RUNTIME_UNAVAILABLE", Message: "the runtime did not report connectivity"})
	} else if state == "online" {
		checks = append(checks, NativeDiagnosticCheck{Stage: "connection", State: "pass", Code: "DEVICE_ONLINE", Message: "the runtime reports the device online"})
	} else {
		checks = append(checks, NativeDiagnosticCheck{Stage: "connection", State: "fail", Code: "DEVICE_OFFLINE", Message: "the runtime reports the device offline"})
	}
	checks = append(checks, NativeDiagnosticCheck{Stage: "ingestion", State: "unknown", Code: "NO_INGESTION_STATS", Message: "the runtime does not provide deduplicated ingestion results"})
	if device.ModelBinding != nil {
		checks = append(checks, NativeDiagnosticCheck{Stage: "model", State: "pass", Code: "MODEL_BOUND", Message: "a fixed thing model version is bound"})
	} else {
		checks = append(checks, NativeDiagnosticCheck{Stage: "model", State: "unknown", Code: "MODEL_NOT_BOUND", Message: "no fixed thing model version is bound"})
	}
	switch {
	case !lastCommand.Valid:
		checks = append(checks, NativeDiagnosticCheck{Stage: "command", State: "unknown", Code: "NO_RECENT_COMMAND", Message: "no command was sent in this window"})
	case lastCommand.String == "failed" || lastCommand.String == "timed_out" || lastCommand.String == "expired":
		checks = append(checks, NativeDiagnosticCheck{Stage: "command", State: "fail", Code: "LAST_COMMAND_" + strings.ToUpper(lastCommand.String), Message: "the most recent command in this window did not complete"})
	case lastCommand.String == "acknowledged":
		checks = append(checks, NativeDiagnosticCheck{Stage: "command", State: "pass", Code: "LAST_COMMAND_ACKNOWLEDGED", Message: "the device acknowledged the most recent command; this is a protocol answer, not proof of a physical effect"})
	default:
		checks = append(checks, NativeDiagnosticCheck{Stage: "command", State: "unknown", Code: "LAST_COMMAND_" + strings.ToUpper(lastCommand.String), Message: "the most recent command has not reached a final state"})
	}
	return &NativeDeviceDiagnosticResponse{Data: NativeDeviceDiagnostic{
		DeviceID: deviceID, ObservedAt: rfc3339Millis(now), Checks: checks,
		WindowFrom: rfc3339Millis(query.from), WindowTo: rfc3339Millis(query.to),
		Stats:          NativeDiagnosticStats{Uplink: incompleteCounter(), Downlink: incompleteCounter(), Storage: incompleteCounter()},
		RecentFailures: failures, RecentFailuresTruncated: truncated,
	}, Meta: RequestMeta{RequestID: requestID}}, nil
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/diagnostics
func (s *Service) GetNativeDeviceDiagnostics(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	values, err := parseNativeReadValues(r, nativeDiagnosticsKeys)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	now := time.Now().UTC()
	query, err := decodeNativeDiagnosticsQuery(values, now)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	connectivity := func(ctx context.Context) (string, error) {
		legacyID, err := loadRuntimeDeviceID(ctx, identity, deviceID)
		if err != nil {
			return "", err
		}
		flag, err := nativeRuntimeClient().GetOnlineFlag(ctx, legacyID)
		if err != nil {
			return "", err
		}
		if flag == 1 {
			return "online", nil
		}
		return "offline", nil
	}
	response, err := readNativeDiagnostics(r.Context(), identity, query, deviceID, requestID, now, connectivity)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}
