package devicemetadata

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"strings"
	"time"

	"encore.dev/beta/errs"
)

type nativeDeviceSummaryItem struct {
	ID             string                `json:"id"`
	RuntimeSummary *NativeRuntimeSummary `json:"runtimeSummary"`
	AlarmSummary   *NativeAlarmSummary   `json:"alarmSummary"`
}

func decodeNativeDeviceSummaryIDs(body []byte, identity nativeDeviceReadIdentity) ([]string, error) {
	var req struct {
		DeviceIDs []string `json:"deviceIds"`
	}
	if err := decodeStrict(body, &req); err != nil || len(req.DeviceIDs) == 0 || len(req.DeviceIDs) > 200 {
		return nil, invalidGroupQuery("deviceIds must contain 1 to 200 UUIDs")
	}
	seen := make(map[string]bool, len(req.DeviceIDs))
	for i, raw := range req.DeviceIDs {
		id, err := nativeReadUUID(raw)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, invalidGroupQuery("deviceIds must be unique")
		}
		if !identity.permits(id) {
			return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
		}
		seen[id] = true
		req.DeviceIDs[i] = id
	}
	return req.DeviceIDs, nil
}

func readNativeDeviceSummaryItems(ctx context.Context, identity nativeDeviceReadIdentity, ids []string) ([]nativeDeviceSummaryItem, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, wrapDBErr(err, "read device summaries failed")
	}
	defer tx.Rollback()
	filter := nativeDeviceReadFilter{args: []any{identity.tenantID}}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, filter.argument(id))
	}
	rows, err := tx.QueryContext(ctx, `
SELECT d.id::text,b.runtime_namespace,b.runtime_external_id
FROM devices d
LEFT JOIN device_access_bindings b ON b.tenant_id=d.tenant_id AND b.device_id=d.id
WHERE d.tenant_id=$1 AND d.deleted_at IS NULL AND d.id IN (`+strings.Join(placeholders, ",")+`)`, filter.args...)
	if err != nil {
		return nil, wrapDBErr(err, "read device summaries failed")
	}
	defer rows.Close()
	devices := make([]NativeDevice, 0, len(ids))
	for rows.Next() {
		var d NativeDevice
		var namespace, externalID sql.NullString
		if err := rows.Scan(&d.ID, &namespace, &externalID); err != nil {
			return nil, wrapDBErr(err, "read device summaries failed")
		}
		if namespace.Valid && externalID.Valid {
			d.RuntimeRef = &RuntimeRef{Namespace: namespace.String, ExternalID: externalID.String}
		}
		devices = append(devices, d)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDBErr(err, "read device summaries failed")
	}
	if len(devices) != len(ids) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "read device summaries failed")
	}
	if err := attachDeviceSummaries(ctx, devices, nativeDeviceListQuery{}); err != nil {
		return nil, err
	}
	items := make([]nativeDeviceSummaryItem, 0, len(devices))
	for _, d := range devices {
		items = append(items, nativeDeviceSummaryItem{ID: d.ID, RuntimeSummary: d.RuntimeSummary, AlarmSummary: d.AlarmSummary})
	}
	return items, nil
}

//encore:api auth raw method=POST path=/api/v2/device-status-summaries
func (s *Service) ReadNativeDeviceSummaries(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16*1024))
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, invalidGroupQuery("request body is too large or unreadable"))
		return
	}
	ids, err := decodeNativeDeviceSummaryIDs(body, identity)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	items, err := readNativeDeviceSummaryItems(nativeRuntimeReadContext(r), identity, ids)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, "", struct {
		Data []nativeDeviceSummaryItem `json:"data"`
		Meta RequestMeta               `json:"meta"`
	}{items, RequestMeta{RequestID: requestID}})
}
