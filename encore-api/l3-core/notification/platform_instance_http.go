package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"encore.app/l1-access/authn"
	"encore.app/l3-core/notification/contract"
)

const platformNotificationTenantPrefix = "/api/v2/platform/notification-tenants/"

func (h *ManagementHTTPHandler) servePlatformTenantRoutes(w http.ResponseWriter, r *http.Request, principal UserPrincipal, data *authn.Data, bearer string, ctx context.Context, path string) bool {
	if !strings.HasPrefix(path, platformNotificationTenantPrefix) {
		return false
	}
	if !containsRole(principal.Roles, "SYS_ADMIN") {
		writeManagementError(w, r, http.StatusForbidden, ErrIdentityForbidden)
		return true
	}
	tenantID, route, err := parsePlatformTenantRoute(path)
	if err != nil {
		writeManagementError(w, r, http.StatusBadRequest, ErrManagementInvalid)
		return true
	}
	if err := h.service.checkPlatformTenant(ctx, principal, tenantID); err != nil {
		writeManagementError(w, r, managementStatus(err), err)
		return true
	}

	switch route {
	case "/notification-plugins":
		if r.Method != http.MethodGet {
			writeManagementError(w, r, http.StatusMethodNotAllowed, ErrManagementInvalid)
			return true
		}
		if len(r.URL.Query()) != 0 {
			writeManagementError(w, r, http.StatusBadRequest, ErrManagementInvalid)
			return true
		}
		items, err := h.service.ListPlatformTenantPlugins(ctx, data, bearer, tenantID)
		if err != nil {
			writeManagementError(w, r, managementStatus(err), err)
			return true
		}
		writeManagementSuccess(w, r, http.StatusOK, map[string]any{"items": items})
		return true
	case "/notification-instances":
		switch r.Method {
		case http.MethodGet:
			items, err := h.service.ListPlatformTenantInstances(ctx, data, bearer, tenantID)
			if err != nil {
				writeManagementError(w, r, managementStatus(err), err)
				return true
			}
			page, pageSize, pluginFilter, channelFilter, err := parsePlatformInstanceListQuery(r.URL.Query())
			if err != nil {
				writeManagementError(w, r, http.StatusBadRequest, ErrManagementInvalid)
				return true
			}
			pluginIDs := map[string]string(nil)
			if pluginFilter != "" {
				plugins, err := h.service.ListPlatformTenantPlugins(ctx, data, bearer, tenantID)
				if err != nil {
					writeManagementError(w, r, managementStatus(err), err)
					return true
				}
				pluginIDs = make(map[string]string, len(plugins))
				for _, plugin := range plugins {
					pluginIDs[plugin.ID] = plugin.PluginID
				}
			}
			filtered := make([]contract.InstanceView, 0, len(items))
			for _, item := range items {
				if channelFilter != "" && item.Channel != channelFilter {
					continue
				}
				if pluginFilter != "" && pluginIDs[item.PluginRegistrationID] != pluginFilter {
					continue
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
			return true
		case http.MethodPost:
			var request contract.InstanceCreate
			_, digest, err := readManagementBody(r, "InstanceCreate", &request)
			if err != nil {
				writeManagementError(w, r, http.StatusBadRequest, ErrNotificationBody)
				return true
			}
			view, err := h.service.CreatePlatformTenantInstance(ctx, data, bearer, tenantID, r.Header.Get("Idempotency-Key"), digest, request)
			if err != nil {
				writeManagementError(w, r, managementStatus(err), err)
				return true
			}
			writeManagementSuccess(w, r, http.StatusCreated, view)
			return true
		default:
			writeManagementError(w, r, http.StatusMethodNotAllowed, ErrManagementInvalid)
			return true
		}
	case "/notification-instances/validate":
		if r.Method != http.MethodPost {
			writeManagementError(w, r, http.StatusMethodNotAllowed, ErrManagementInvalid)
			return true
		}
		var request contract.InstanceValidate
		if _, _, err := readManagementBody(r, "InstanceValidate", &request); err != nil {
			writeManagementError(w, r, http.StatusBadRequest, ErrNotificationBody)
			return true
		}
		result, err := h.service.ValidatePlatformTenantInstance(ctx, data, bearer, tenantID, request)
		if err != nil {
			writeManagementError(w, r, managementStatus(err), err)
			return true
		}
		writeManagementSuccess(w, r, http.StatusOK, publicValidationResult(result))
		return true
	}

	const instancePrefix = "/notification-instances/"
	if !strings.HasPrefix(route, instancePrefix) {
		writeManagementError(w, r, http.StatusNotFound, ErrManagementNotFound)
		return true
	}
	tail := strings.TrimPrefix(route, instancePrefix)
	segments := strings.Split(tail, "/")
	if len(segments) == 0 || segments[0] == "" {
		writeManagementError(w, r, http.StatusNotFound, ErrManagementNotFound)
		return true
	}
	instanceID, err := url.PathUnescape(segments[0])
	if err != nil || instanceID == "" || strings.ContainsAny(instanceID, "/\\\x00\r\n") {
		writeManagementError(w, r, http.StatusNotFound, ErrManagementNotFound)
		return true
	}
	if len(segments) == 2 && segments[1] == "receipt-credential" {
		if r.Method != http.MethodPost {
			writeManagementError(w, r, http.StatusMethodNotAllowed, ErrManagementInvalid)
			return true
		}
		digest := SnapshotDigest([]byte("rotateReceiptCredential:" + instanceID))
		credential, err := h.service.RotatePlatformTenantReceiptCredential(ctx, data, bearer, tenantID, instanceID, r.Header.Get("Idempotency-Key"), digest)
		if err != nil {
			writeManagementError(w, r, managementStatus(err), err)
			return true
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(credential)
		return true
	}
	if len(segments) != 1 {
		writeManagementError(w, r, http.StatusNotFound, ErrManagementNotFound)
		return true
	}
	switch r.Method {
	case http.MethodGet:
		view, err := h.service.GetPlatformTenantInstance(ctx, data, bearer, tenantID, instanceID)
		if err != nil {
			writeManagementError(w, r, managementStatus(err), err)
			return true
		}
		writeManagementSuccess(w, r, http.StatusOK, view)
	case http.MethodPut:
		var update contract.InstanceUpdate
		_, digest, err := readManagementBody(r, "InstanceUpdate", &update)
		if err != nil {
			writeManagementError(w, r, http.StatusBadRequest, ErrNotificationBody)
			return true
		}
		view, err := h.service.UpdatePlatformTenantInstance(ctx, data, bearer, tenantID, instanceID, r.Header.Get("Idempotency-Key"), digest, update)
		if err != nil {
			writeManagementError(w, r, managementStatus(err), err)
			return true
		}
		writeManagementSuccess(w, r, http.StatusOK, view)
	default:
		writeManagementError(w, r, http.StatusMethodNotAllowed, ErrManagementInvalid)
	}
	return true
}

func parsePlatformTenantRoute(path string) (string, string, error) {
	rest := strings.TrimPrefix(path, platformNotificationTenantPrefix)
	separator := strings.IndexByte(rest, '/')
	if separator <= 0 || separator == len(rest)-1 {
		return "", "", ErrManagementInvalid
	}
	tenantID, err := url.PathUnescape(rest[:separator])
	if err != nil || !validPlatformTenantID(tenantID) {
		return "", "", ErrManagementInvalid
	}
	return tenantID, rest[separator:], nil
}

func parsePlatformInstanceListQuery(query url.Values) (int, int, string, contract.Channel, error) {
	for name, values := range query {
		if (name != "page" && name != "pageSize" && name != "pluginId" && name != "channel") || len(values) != 1 {
			return 0, 0, "", "", ErrManagementInvalid
		}
	}
	page, pageSize, err := parseManagementPage(query)
	if err != nil {
		return 0, 0, "", "", err
	}
	channel := contract.Channel(query.Get("channel"))
	if channel != "" && !validChannel(channel) {
		return 0, 0, "", "", ErrManagementInvalid
	}
	return page, pageSize, query.Get("pluginId"), channel, nil
}
