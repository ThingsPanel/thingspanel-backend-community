package notification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"encore.app/l1-access/authn"
	"encore.app/l3-core/notification/contract"
)

type ManagementHTTPHandler struct {
	service *ManagementService
}

func NewManagementHTTPHandler(service *ManagementService) http.Handler {
	return &ManagementHTTPHandler{service: service}
}

func (h *ManagementHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		writeManagementError(w, r, http.StatusServiceUnavailable, ErrManagementUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	bearer := r.Header.Get("Authorization")
	principal, err := h.service.VerifyUser(r.Context(), bearer)
	if err != nil {
		writeManagementError(w, r, http.StatusUnauthorized, ErrIdentityUnauthorized)
		return
	}
	data := &authn.Data{UserID: principal.UserID, TenantID: principal.TenantID, Roles: append([]string(nil), principal.Roles...), Scopes: append([]string(nil), principal.Scopes...)}
	ctx := withVerifiedUserPrincipal(r.Context(), principal)
	path := strings.TrimSuffix(r.URL.Path, "/")
	if h.servePlatformTenantRoutes(w, r, principal, data, bearer, ctx, path) {
		return
	}

	switch {
	case path == "/api/v2/notification-instances/validate" && r.Method == http.MethodPost:
		// Account validation can carry secret fields. Only the explicit
		// platform-selected tenant route is allowed to read and validate it.
		writeManagementError(w, r, http.StatusForbidden, ErrIdentityForbidden)
	case path == "/api/v2/notification-plugin-grants":
		if err := requireUserCapability(principal, "plugin_manage"); err != nil || !containsRole(principal.Roles, "SYS_ADMIN") {
			writeManagementError(w, r, http.StatusForbidden, ErrIdentityForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet:
			registrationID, page, pageSize, err := parsePluginGrantQuery(r.URL.Query())
			if err != nil {
				writeManagementError(w, r, http.StatusBadRequest, ErrManagementInvalid)
				return
			}
			result, err := h.service.ListPluginGrants(ctx, data, bearer, registrationID, page, pageSize)
			if err != nil {
				writeManagementError(w, r, managementStatus(err), err)
				return
			}
			writeManagementSuccess(w, r, http.StatusOK, result)
		case http.MethodPut:
			var update contract.PluginGrantUpdate
			_, digest, err := readManagementBody(r, "PluginGrantUpdate", &update)
			if err != nil {
				writeManagementError(w, r, http.StatusBadRequest, ErrNotificationBody)
				return
			}
			view, created, err := h.service.SetPluginGrant(ctx, data, bearer, r.Header.Get("Idempotency-Key"), digest, update)
			if err != nil {
				writeManagementError(w, r, managementStatus(err), err)
				return
			}
			status := http.StatusOK
			if created {
				status = http.StatusCreated
			}
			writeManagementSuccess(w, r, status, view)
		default:
			writeManagementError(w, r, http.StatusMethodNotAllowed, ErrManagementInvalid)
		}
	case path == "/api/v2/notification-plugins" && r.Method == http.MethodGet:
		items, err := h.service.ListPlugins(ctx, data, bearer)
		if err != nil {
			writeManagementError(w, r, managementStatus(err), err)
			return
		}
		writeManagementSuccess(w, r, http.StatusOK, map[string]any{"items": items})
	case path == "/api/v2/notification-plugins" && r.Method == http.MethodPost:
		if err := requireUserCapability(principal, "plugin_manage"); err != nil {
			writeManagementError(w, r, http.StatusForbidden, err)
			return
		}
		var request contract.PluginRegistration
		raw, digest, err := readManagementBody(r, "PluginRegistration", &request)
		if err != nil {
			writeManagementError(w, r, http.StatusBadRequest, ErrNotificationBody)
			return
		}
		_ = raw
		view, err := h.service.RegisterPlugin(ctx, data, bearer, r.Header.Get("Idempotency-Key"), digest, request)
		if err != nil {
			writeManagementError(w, r, managementStatus(err), err)
			return
		}
		writeManagementSuccess(w, r, http.StatusCreated, view)
	case strings.HasPrefix(path, "/api/v2/notification-plugins/"):
		id, err := managementPathID(path, "/api/v2/notification-plugins/")
		if err != nil {
			writeManagementError(w, r, http.StatusNotFound, ErrManagementNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			view, err := h.service.GetPlugin(ctx, data, bearer, id)
			if err != nil {
				writeManagementError(w, r, managementStatus(err), err)
				return
			}
			writeManagementSuccess(w, r, http.StatusOK, view)
		case http.MethodPut:
			if err := requireUserCapability(principal, "plugin_manage"); err != nil {
				writeManagementError(w, r, http.StatusForbidden, err)
				return
			}
			var update contract.PluginUpdate
			_, digest, err := readManagementBody(r, "PluginUpdate", &update)
			if err != nil {
				writeManagementError(w, r, http.StatusBadRequest, ErrNotificationBody)
				return
			}
			view, err := h.service.UpdatePlugin(ctx, data, bearer, id, r.Header.Get("Idempotency-Key"), digest, update)
			if err != nil {
				writeManagementError(w, r, managementStatus(err), err)
				return
			}
			writeManagementSuccess(w, r, http.StatusOK, view)
		default:
			writeManagementError(w, r, http.StatusMethodNotAllowed, ErrManagementInvalid)
		}
	case path == "/api/v2/notification-instances" && r.Method == http.MethodGet:
		items, err := h.service.ListInstances(ctx, data, bearer)
		if err != nil {
			writeManagementError(w, r, managementStatus(err), err)
			return
		}
		page, pageSize, err := parseManagementPage(r.URL.Query())
		if err != nil {
			writeManagementError(w, r, http.StatusBadRequest, ErrManagementInvalid)
			return
		}
		pluginFilter := r.URL.Query().Get("pluginId")
		channelFilter := contract.Channel(r.URL.Query().Get("channel"))
		if channelFilter != "" && !validChannel(channelFilter) {
			writeManagementError(w, r, http.StatusBadRequest, ErrManagementInvalid)
			return
		}
		filtered := make([]contract.InstanceView, 0, len(items))
		for _, item := range items {
			if channelFilter != "" && item.Channel != channelFilter {
				continue
			}
			if pluginFilter != "" {
				plugin, err := h.service.GetPlugin(ctx, data, bearer, item.PluginRegistrationID)
				if err != nil || plugin.PluginID != pluginFilter {
					continue
				}
			}
			filtered = append(filtered, item)
		}
		total := len(filtered)
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		writeManagementSuccess(w, r, http.StatusOK, map[string]any{"items": filtered[start:end], "page": page, "pageSize": pageSize, "total": total})
	case path == "/api/v2/notification-instances" && r.Method == http.MethodPost:
		writeManagementError(w, r, http.StatusForbidden, ErrIdentityForbidden)
	case strings.HasPrefix(path, "/api/v2/notification-instances/") && strings.HasSuffix(path, "/receipt-credential"):
		writeManagementError(w, r, http.StatusForbidden, ErrIdentityForbidden)
	case strings.HasPrefix(path, "/api/v2/notification-instances/"):
		id, err := managementPathID(path, "/api/v2/notification-instances/")
		if err != nil {
			writeManagementError(w, r, http.StatusNotFound, ErrManagementNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			view, err := h.service.GetInstance(ctx, data, bearer, id)
			if err != nil {
				writeManagementError(w, r, managementStatus(err), err)
				return
			}
			writeManagementSuccess(w, r, http.StatusOK, view)
		case http.MethodPut:
			writeManagementError(w, r, http.StatusForbidden, ErrIdentityForbidden)
		default:
			writeManagementError(w, r, http.StatusMethodNotAllowed, ErrManagementInvalid)
		}
	default:
		writeManagementError(w, r, http.StatusNotFound, ErrManagementNotFound)
	}
}

func parsePluginGrantQuery(query url.Values) (string, int, int, error) {
	for name, values := range query {
		if (name != "pluginRegistrationId" && name != "page" && name != "pageSize") || len(values) != 1 {
			return "", 0, 0, ErrManagementInvalid
		}
	}
	registrationID := query.Get("pluginRegistrationId")
	if registrationID != "" && !validManifestToken(registrationID) {
		return "", 0, 0, ErrManagementInvalid
	}
	paging := make(url.Values, 2)
	if values, ok := query["page"]; ok {
		paging["page"] = values
	}
	if values, ok := query["pageSize"]; ok {
		paging["pageSize"] = values
	}
	page, pageSize, err := parseManagementPage(paging)
	if err != nil {
		return "", 0, 0, err
	}
	return registrationID, page, pageSize, nil
}

func publicValidationResult(result contract.PluginValidateResultV1) contract.ValidationResult {
	response := contract.ValidationResult{Valid: result.Valid, Errors: make([]contract.FieldError, 0, len(result.Errors))}
	for range result.Errors {
		// Plugin-supplied diagnostics are untrusted and may echo configuration
		// or credentials. Keep the public response value-free.
		response.Errors = append(response.Errors, contract.FieldError{Field: "config", Reason: "plugin_validation_failed"})
	}
	return response
}

func readManagementBody(r *http.Request, schema string, dst any) ([]byte, string, error) {
	if r == nil || r.Body == nil {
		return nil, "", ErrNotificationBody
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return nil, "", ErrNotificationBody
	}
	if DecodeNotificationBody(raw, schema, dst) != nil {
		return nil, "", ErrNotificationBody
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:]), nil
}

func managementPathID(path, prefix string) (string, error) {
	if !strings.HasPrefix(path, prefix) {
		return "", ErrManagementNotFound
	}
	escaped := strings.TrimPrefix(path, prefix)
	if escaped == "" || strings.Contains(escaped, "/") {
		return "", ErrManagementNotFound
	}
	id, err := url.PathUnescape(escaped)
	if err != nil || id == "" || strings.ContainsAny(id, "/\\\x00") {
		return "", ErrManagementNotFound
	}
	return id, nil
}

func parseManagementPage(query url.Values) (int, int, error) {
	for name := range query {
		if name != "page" && name != "pageSize" && name != "pluginId" && name != "channel" {
			return 0, 0, ErrManagementInvalid
		}
	}
	page, pageSize := 1, 20
	if raw := query.Get("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return 0, 0, ErrManagementInvalid
		}
		page = value
	}
	if raw := query.Get("pageSize"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return 0, 0, ErrManagementInvalid
		}
		pageSize = value
	}
	if page > int(^uint(0)>>1)/pageSize {
		return 0, 0, ErrManagementInvalid
	}
	return page, pageSize, nil
}

func writeManagementSuccess(w http.ResponseWriter, r *http.Request, status int, data any) {
	requestID := newStableID()
	if requestID == "" {
		requestID = "notification"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.Envelope[any]{Code: 200, Message: "ok", RequestID: requestID, Data: data})
}

func writeManagementError(w http.ResponseWriter, r *http.Request, status int, err error) {
	reason := "notification_unavailable"
	message := "notification request failed"
	switch {
	case errors.Is(err, ErrIdentityUnauthorized):
		reason, message = "unauthorized", "authentication required"
	case errors.Is(err, ErrTenantContextRequired):
		reason, message = "tenant_context_required", "select a tenant to use tenant resources"
	case errors.Is(err, ErrIdentityForbidden):
		reason, message = "forbidden", "permission denied"
	case errors.Is(err, ErrManagementNotFound):
		reason, message = "not_found", "resource not found"
	case errors.Is(err, ErrIdentityChangeRequiresNewInstance):
		reason, message = "identity_change_requires_new_instance", "identity changes require a new instance"
	case errors.Is(err, ErrManagementConflict):
		reason, message = "conflict", "resource version conflicts"
	case errors.Is(err, ErrManagementInvalid), errors.Is(err, ErrNotificationBody), errors.Is(err, ErrPluginManifest):
		reason, message = "invalid_request", "request is invalid"
	case errors.Is(err, ErrPrivacyGuardClosed):
		reason, message = "privacy_guard_closed", "notification management is unavailable"
	}
	requestID := newStableID()
	if requestID == "" {
		requestID = "notification"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.DomainError{Code: status, Message: message, Details: contract.ErrorDetail{Reason: reason}, RequestID: requestID})
}

func managementStatus(err error) int {
	switch {
	case errors.Is(err, ErrIdentityUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, ErrIdentityForbidden), errors.Is(err, ErrTenantContextRequired):
		return http.StatusForbidden
	case errors.Is(err, ErrManagementNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrIdentityChangeRequiresNewInstance):
		return http.StatusConflict
	case errors.Is(err, ErrManagementConflict):
		return http.StatusConflict
	case errors.Is(err, ErrManagementInvalid), errors.Is(err, ErrNotificationBody), errors.Is(err, ErrPluginManifest):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusServiceUnavailable
	}
}
