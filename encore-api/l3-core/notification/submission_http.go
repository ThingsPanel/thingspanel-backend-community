package notification

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encore.app/l3-core/notification/contract"
)

type NotificationHTTPHandler struct {
	Verifier   PrincipalVerifier
	Core       *Core
	Store      *Store
	Management http.Handler
	Ready      func() error
}

// ServeHTTP is called by native Encore raw endpoints. The running service must
// pass its startup privacy gate before any of these endpoints can be served.
func (h *NotificationHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if h == nil || h.Ready == nil || h.Ready() != nil || h.Verifier == nil || h.Core == nil {
		writeNotificationFailure(w, r, coreFailure(503, "notification_unavailable"))
		return
	}
	path := r.URL.Path
	if h.serveSourceGroupInspection(w, r) {
		return
	}
	if r.Method == http.MethodPost && h.ServeNotificationGroupProjectionRoute(w, r) {
		return
	}
	if r.Method == http.MethodPost && path == "/api/v2/notification-ingress/events" {
		principal, err := h.Verifier.VerifySource(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			writeNotificationFailure(w, r, coreFailure(401, "source_authentication_required"))
			return
		}
		var event contract.SourceEventV1
		if _, _, err := readManagementBody(r, "SourceEventV1", &event); err != nil {
			writeNotificationFailure(w, r, coreFailure(400, "invalid_source_event"))
			return
		}
		accepted, err := h.Core.ReceiveSourceEvent(r.Context(), principal, r.Header.Get("Idempotency-Key"), event)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		writeManagementSuccess(w, r, http.StatusAccepted, accepted)
		return
	}
	if strings.HasPrefix(path, "/api/v1/notification-instances/") && strings.HasSuffix(path, "/receipts") && r.Method == http.MethodPost {
		id, err := notificationNestedID(path, "/api/v1/notification-instances/", "/receipts")
		if err != nil {
			writeReceiptFailure(w, 404, "not_found")
			return
		}
		principal, err := h.Verifier.VerifyReceipt(r.Context(), r.Header.Get("Authorization"), id)
		if err != nil {
			writeReceiptFailure(w, 401, "unauthorized")
			return
		}
		if h.Store == nil {
			writeReceiptFailure(w, 503, "receipt_unavailable")
			return
		}
		var receipt contract.PluginReceiptV1
		if _, _, err := readManagementBody(r, "PluginReceipt", &receipt); err != nil {
			writeReceiptFailure(w, 400, "invalid_receipt")
			return
		}
		digest, err := NotificationIntentDigest(receipt)
		if err != nil {
			writeReceiptFailure(w, 400, "invalid_receipt")
			return
		}
		ack, err := h.Store.AcceptReceipt(r.Context(), principal, receipt, digest)
		if err != nil {
			status, reason := notificationErrorStatus(err)
			writeReceiptFailure(w, status, reason)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ack)
		return
	}
	if strings.HasPrefix(path, platformNotificationTenantPrefix) || path == "/api/v2/notification-plugin-grants" || strings.HasPrefix(path, "/api/v2/notification-plugins/") || path == "/api/v2/notification-plugins" || path == "/api/v2/notification-instances" || path == "/api/v2/notification-instances/validate" || (strings.HasPrefix(path, "/api/v2/notification-instances/") && !strings.HasSuffix(path, "/test")) {
		if h.Management == nil {
			writeNotificationFailure(w, r, coreFailure(503, "notification_unavailable"))
			return
		}
		h.Management.ServeHTTP(w, r)
		return
	}
	principal, err := h.Verifier.VerifyUser(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeNotificationFailure(w, r, coreFailure(401, "authentication_required"))
		return
	}
	if h.ServeNotificationGroupRoutes(w, r, principal) {
		return
	}
	if path == "/v1/notifications/history" && r.Method == http.MethodGet {
		h.serveHistoryCompatibility(w, r, principal)
		return
	}
	if r.Method == http.MethodPost && path == "/api/v2/notifications" {
		if err := requireNotificationScope(principal, "notification:send"); err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		var intent contract.NotificationSubmit
		if _, _, err := readManagementBody(r, "NotificationSubmit", &intent); err != nil {
			writeNotificationFailure(w, r, coreFailure(400, "invalid_intent"))
			return
		}
		accepted, err := h.Core.Submit(r.Context(), principal, r.Header.Get("Idempotency-Key"), intent)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		writeManagementSuccess(w, r, 202, accepted)
		return
	}
	if r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v2/notification-instances/") && strings.HasSuffix(path, "/test") {
		for _, scope := range []string{"notification:manage", "notification:send_test"} {
			if err := requireNotificationScope(principal, scope); err != nil {
				writeNotificationFailure(w, r, err)
				return
			}
		}
		id, err := notificationNestedID(path, "/api/v2/notification-instances/", "/test")
		if err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		var intent contract.TestSendRequest
		if _, _, err := readManagementBody(r, "TestSendRequest", &intent); err != nil {
			writeNotificationFailure(w, r, coreFailure(400, "invalid_intent"))
			return
		}
		accepted, err := h.Core.TestSend(r.Context(), principal, id, r.Header.Get("Idempotency-Key"), intent)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		writeManagementSuccess(w, r, 202, accepted)
		return
	}
	if err := requireNotificationScope(principal, "notification:read"); err != nil {
		writeNotificationFailure(w, r, err)
		return
	}
	if h.Store == nil {
		writeNotificationFailure(w, r, coreFailure(503, "notification_unavailable"))
		return
	}
	if r.Method == http.MethodGet && path == "/api/v2/notifications" {
		query := r.URL.Query()
		allowed := map[string]bool{"page": true, "pageSize": true, "sourceType": true, "sourceId": true, "intakeStatus": true, "from": true, "to": true}
		for key, values := range query {
			if !allowed[key] || len(values) != 1 {
				writeNotificationFailure(w, r, coreFailure(400, "invalid_query"))
				return
			}
		}
		page, size, err := parseNotificationPage(query)
		if err != nil {
			writeNotificationFailure(w, r, coreFailure(400, "invalid_query"))
			return
		}
		if queryHas(query, "sourceType") && !validNotificationSourceType(query.Get("sourceType")) {
			writeNotificationFailure(w, r, coreFailure(400, "invalid_query"))
			return
		}
		if queryHas(query, "intakeStatus") && !validNotificationIntakeStatus(contract.IntakeStatus(query.Get("intakeStatus"))) {
			writeNotificationFailure(w, r, coreFailure(400, "invalid_query"))
			return
		}
		filter := NotificationListFilter{Page: page, PageSize: size, SourceType: query.Get("sourceType"), SourceID: query.Get("sourceId"), IntakeStatus: contract.IntakeStatus(query.Get("intakeStatus"))}
		for _, field := range []string{"from", "to"} {
			if raw := query.Get(field); raw != "" {
				parsed, err := time.Parse(time.RFC3339Nano, raw)
				if err != nil {
					writeNotificationFailure(w, r, coreFailure(400, "invalid_query"))
					return
				}
				if field == "from" {
					filter.From = &parsed
				} else {
					filter.To = &parsed
				}
			}
		}
		result, err := h.Store.ListNotifications(r.Context(), principal.TenantID, filter)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		writeManagementSuccess(w, r, 200, result)
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v2/notifications/") {
		id, err := managementPathID(path, "/api/v2/notifications/")
		if err != nil {
			writeNotificationFailure(w, r, coreFailure(404, "not_found"))
			return
		}
		view, err := h.Store.GetNotification(r.Context(), principal.TenantID, id)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		writeManagementSuccess(w, r, 200, view)
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v2/notification-deliveries/") {
		id, err := managementPathID(path, "/api/v2/notification-deliveries/")
		if err != nil {
			writeNotificationFailure(w, r, coreFailure(404, "not_found"))
			return
		}
		view, err := h.Store.GetDelivery(r.Context(), principal.TenantID, id)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		writeManagementSuccess(w, r, 200, view)
		return
	}
	if r.Method == http.MethodGet && path == "/api/v2/notification-deliveries" {
		filter, err := notificationDeliveryFilter(r)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		page, err := h.Store.ListDeliveries(r.Context(), principal.TenantID, filter)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return
		}
		writeManagementSuccess(w, r, 200, page)
		return
	}
	writeNotificationFailure(w, r, coreFailure(404, "not_found"))
}

func notificationNestedID(path, prefix, suffix string) (string, error) {
	if !strings.HasSuffix(path, suffix) {
		return "", coreFailure(404, "not_found")
	}
	id, err := managementPathID(strings.TrimSuffix(path, suffix), prefix)
	if err != nil || len(id) > 256 {
		return "", coreFailure(404, "not_found")
	}
	return id, nil
}

func notificationErrorStatus(err error) (int, string) {
	var core *CoreFailure
	if errors.As(err, &core) {
		return core.Status, core.Reason
	}
	var store *StoreError
	if errors.As(err, &store) {
		switch store.Kind {
		case StoreErrorInvalid:
			return 422, "invalid_request"
		case StoreErrorNotFound:
			return 404, "not_found"
		case StoreErrorConflict:
			return 409, "idempotency_conflict"
		case StoreErrorVersionConflict:
			return 409, "version_conflict"
		case StoreErrorStaleClaim:
			return 409, "stale_claim"
		}
	}
	return 503, "notification_unavailable"
}
func writeNotificationFailure(w http.ResponseWriter, r *http.Request, err error) {
	status, reason := notificationErrorStatus(err)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.DomainError{Code: status, Message: "notification request failed", Details: contract.ErrorDetail{Reason: reason}, RequestID: newStableID()})
}
func writeReceiptFailure(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.PluginProtocolErrorV1{Code: code, Message: "receipt request failed"})
}

func notificationDeliveryFilter(r *http.Request) (DeliveryFilter, error) {
	allowed := map[string]bool{"page": true, "pageSize": true, "instanceId": true, "sourceType": true, "sourceId": true, "dispatchStatus": true, "deliveryStatus": true, "from": true, "to": true}
	for key, values := range r.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			return DeliveryFilter{}, coreFailure(400, "invalid_query")
		}
	}
	page, size, err := parseNotificationPage(r.URL.Query())
	if err != nil {
		return DeliveryFilter{}, coreFailure(400, "invalid_query")
	}
	query := r.URL.Query()
	if queryHas(query, "sourceType") && !validNotificationSourceType(query.Get("sourceType")) {
		return DeliveryFilter{}, coreFailure(400, "invalid_query")
	}
	if queryHas(query, "dispatchStatus") && !validDispatchStatus(contract.DispatchStatus(query.Get("dispatchStatus"))) {
		return DeliveryFilter{}, coreFailure(400, "invalid_query")
	}
	if queryHas(query, "deliveryStatus") && !validDeliveryStatus(contract.DeliveryStatus(query.Get("deliveryStatus"))) {
		return DeliveryFilter{}, coreFailure(400, "invalid_query")
	}
	filter := DeliveryFilter{Page: page, PageSize: size, InstanceID: query.Get("instanceId"), SourceType: query.Get("sourceType"), SourceID: query.Get("sourceId"), DispatchStatus: contract.DispatchStatus(query.Get("dispatchStatus")), DeliveryStatus: contract.DeliveryStatus(query.Get("deliveryStatus"))}
	for _, field := range []string{"from", "to"} {
		if raw := query.Get(field); raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return filter, coreFailure(400, "invalid_query")
			}
			if field == "from" {
				filter.From = &parsed
			} else {
				filter.To = &parsed
			}
		}
	}
	return filter, nil
}

func queryHas(query url.Values, key string) bool {
	_, ok := query[key]
	return ok
}

func validNotificationSourceType(value string) bool {
	switch value {
	case "alarm", "automation", "manual", "test":
		return true
	default:
		return false
	}
}

func validNotificationIntakeStatus(value contract.IntakeStatus) bool {
	return value == contract.IntakeReady || value == contract.IntakeBlocked
}

func validDispatchStatus(value contract.DispatchStatus) bool {
	switch value {
	case contract.DispatchQueued, contract.DispatchSending, contract.DispatchAccepted, contract.DispatchFailed, contract.DispatchUnknown:
		return true
	default:
		return false
	}
}

func validDeliveryStatus(value contract.DeliveryStatus) bool {
	switch value {
	case contract.DeliveryUnsupported, contract.DeliveryPending, contract.DeliveryDelivered, contract.DeliveryFailed, contract.DeliveryUnknown:
		return true
	default:
		return false
	}
}

// Each notification query validates its own full filter set before extracting
// pagination. Management-list filters are a different API contract.
func parseNotificationPage(query url.Values) (int, int, error) {
	page := url.Values{}
	for _, key := range []string{"page", "pageSize"} {
		if values, ok := query[key]; ok {
			if len(values) != 1 {
				return 0, 0, ErrManagementInvalid
			}
			page[key] = values
		}
	}
	return parseManagementPage(page)
}
