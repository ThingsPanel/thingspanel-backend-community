package notification

import (
	"encore.app/l3-core/notification/contract"
	"net/http"
	"net/url"
	"strings"
)

// The old URL and page_size shape remain. Unscoped demonstration memory is
// deliberately excluded: only durable records belonging to this user tenant
// are mapped, and provider acceptance is never presented as delivery success.
func (h *NotificationHTTPHandler) serveHistoryCompatibility(w http.ResponseWriter, r *http.Request, p UserPrincipal) {
	if err := requireNotificationScope(p, "notification:read"); err != nil {
		writeNotificationFailure(w, r, err)
		return
	}
	query := r.URL.Query()
	translated := url.Values{}
	for key, values := range query {
		if (key != "page" && key != "page_size") || len(values) != 1 {
			writeNotificationFailure(w, r, coreFailure(400, "invalid_query"))
			return
		}
		if key == "page_size" {
			translated.Set("pageSize", values[0])
		} else {
			translated.Set(key, values[0])
		}
	}
	page, size, err := parseManagementPage(translated)
	if err != nil {
		writeNotificationFailure(w, r, coreFailure(400, "invalid_query"))
		return
	}
	if h.Store == nil {
		writeNotificationFailure(w, r, coreFailure(503, "notification_unavailable"))
		return
	}
	records, err := h.Store.ListNotifications(r.Context(), p.TenantID, NotificationListFilter{Page: page, PageSize: size})
	if err != nil {
		writeNotificationFailure(w, r, err)
		return
	}
	result := HistoryData{Items: []HistoryItem{}, Page: page, PageSize: size, Total: int(records.Total)}
	for _, record := range records.Items {
		result.Items = append(result.Items, legacyHistoryItem(record))
	}
	writeManagementSuccess(w, r, http.StatusOK, result)
}

func legacyHistoryItem(record contract.NotificationView) HistoryItem {
	states := []string{"intake=" + string(record.IntakeStatus)}
	if record.BlockedReason != "" {
		states = append(states, "reason="+record.BlockedReason)
	}
	seen := map[string]bool{}
	for _, delivery := range record.Deliveries {
		state := "dispatch=" + string(delivery.DispatchStatus) + ",delivery=" + string(delivery.DeliveryStatus)
		if !seen[state] {
			states = append(states, state)
			seen[state] = true
		}
	}
	return HistoryItem{ID: record.ID, CreatedAt: record.CreatedAt, Message: strings.Join(states, "; ")}
}
