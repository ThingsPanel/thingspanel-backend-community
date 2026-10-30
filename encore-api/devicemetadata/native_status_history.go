package devicemetadata

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
)

// Online/offline history served from the Community device_status_history table.
// The Community filter has second resolution and an inclusive upper bound, so only
// `from` is accepted; `to` is refused instead of returning rows that differ from the
// contract's [from,to) semantics. Community never records "unknown".

type NativeDeviceStatusEvent struct {
	ID           string  `json:"id"`
	DeviceID     string  `json:"deviceId"`
	Connectivity string  `json:"connectivity"`
	ObservedAt   string  `json:"observedAt"`
	Reason       *string `json:"reason"`
}

type NativeDeviceStatusEventPage struct {
	Items    []NativeDeviceStatusEvent `json:"items"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"pageSize"`
	Total    int64                     `json:"total"`
}

type NativeDeviceStatusEventPageResponse struct {
	Data NativeDeviceStatusEventPage `json:"data"`
	Meta RequestMeta                 `json:"meta"`
}

type nativeStatusHistoryQuery struct {
	page, pageSize int
	fromSec        *int64
	online         *bool
	unknownOnly    bool
}

var nativeStatusHistoryKeys = []string{"page", "pageSize", "from", "to", "connectivity"}

func decodeNativeStatusHistoryQuery(values url.Values) (nativeStatusHistoryQuery, error) {
	var out nativeStatusHistoryQuery
	var err error
	out.page, out.pageSize, err = normalizeDeviceGroupPagination(values.Get("page"), values.Get("pageSize"))
	if err != nil {
		return out, err
	}
	if values.Has("to") {
		return out, &errs.Error{Code: errs.InvalidArgument, Message: "to is not supported by the Community history adapter; filter with from only"}
	}
	if values.Has("from") {
		parsed, parseErr := time.Parse(time.RFC3339, values.Get("from"))
		if parseErr != nil {
			return out, invalidGroupQuery("from must be an RFC 3339 timestamp")
		}
		if parsed.Nanosecond() != 0 {
			return out, invalidGroupQuery("from must be a whole second for the Community history adapter")
		}
		seconds := parsed.Unix()
		out.fromSec = &seconds
	}
	if values.Has("connectivity") {
		switch values.Get("connectivity") {
		case "online":
			v := true
			out.online = &v
		case "offline":
			v := false
			out.online = &v
		case "unknown":
			out.unknownOnly = true
		default:
			return out, invalidGroupQuery("connectivity must be online, offline or unknown")
		}
	}
	return out, nil
}

func readNativeStatusHistory(ctx context.Context, identity nativeDeviceReadIdentity, query nativeStatusHistoryQuery, deviceID, requestID string) (*NativeDeviceStatusEventPageResponse, error) {
	legacyID, err := loadRuntimeDeviceID(ctx, identity, deviceID)
	if err != nil {
		return nil, err
	}
	page := NativeDeviceStatusEventPage{Items: []NativeDeviceStatusEvent{}, Page: query.page, PageSize: query.pageSize}
	if !query.unknownOnly {
		rows, total, err := nativeRuntimeLegacy().ListStatusHistory(ctx, legacyID, query.page, query.pageSize, query.fromSec, query.online)
		if err != nil {
			return nil, runtimeUpstreamError(err)
		}
		page.Total = total
		for _, row := range rows {
			state := "offline"
			if row.Online {
				state = "online"
			}
			page.Items = append(page.Items, NativeDeviceStatusEvent{
				ID: "legacy-status:" + strconv.FormatInt(row.ID, 10), DeviceID: deviceID,
				Connectivity: state, ObservedAt: rfc3339Millis(row.ChangeTime),
			})
		}
	}
	return &NativeDeviceStatusEventPageResponse{Data: page, Meta: RequestMeta{RequestID: requestID}}, nil
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/status-history
func (s *Service) ListNativeDeviceStatusHistory(w http.ResponseWriter, r *http.Request) {
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
	values, err := parseNativeReadValues(r, nativeStatusHistoryKeys)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	query, err := decodeNativeStatusHistoryQuery(values)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	response, err := readNativeStatusHistory(r.Context(), identity, query, deviceID, requestID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}
