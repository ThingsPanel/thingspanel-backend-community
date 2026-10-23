package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"encore.app/l3-core/notification/contract"
)

type groupHTTPStore interface {
	GroupManagementRepository
	groupValidationReader
	groupQueryStore
	FindManagementReplay(context.Context, string, string, string, string) (ManagementReplay, bool, error)
	GetGroupRevision(context.Context, string, string, int64) (contract.GroupView, error)
	RegisterGroupProjectionIdempotent(context.Context, SourceProjectionPrincipal, string, string, contract.SourceGroupProjectionRequest) (contract.SourceGroupProjection, error)
}

type groupProjectionHTTPStore interface {
	RegisterGroupProjectionIdempotent(context.Context, SourceProjectionPrincipal, string, string, contract.SourceGroupProjectionRequest) (contract.SourceGroupProjection, error)
}

// ServeNotificationGroupRoutes is called by the root raw handler after user
// authentication. A true result means the path belongs to the group API.
func (h *NotificationHTTPHandler) ServeNotificationGroupRoutes(w http.ResponseWriter, r *http.Request, principal UserPrincipal) bool {
	if r == nil {
		return false
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path != "/api/v2/notification-groups" && !strings.HasPrefix(path, "/api/v2/notification-groups/") {
		return false
	}
	if h == nil {
		setGroupHTTPHeaders(w)
		writeGroupDomainError(w, http.StatusServiceUnavailable, "notification_unavailable", nil)
		return true
	}
	serveNotificationGroupRoutes(w, r, principal, h.Store)
	return true
}

// ServeNotificationGroupProjectionRoute is called before user-JWT verification;
// the endpoint accepts only a deployment-scoped source projection credential.
func (h *NotificationHTTPHandler) ServeNotificationGroupProjectionRoute(w http.ResponseWriter, r *http.Request) bool {
	if r == nil || strings.TrimSuffix(r.URL.Path, "/") != "/api/v2/notification-group-projections" {
		return false
	}
	setGroupHTTPHeaders(w)
	if h == nil {
		writeGroupDomainError(w, http.StatusServiceUnavailable, "notification_unavailable", nil)
		return true
	}
	serveNotificationGroupProjectionRoute(w, r, h.Verifier, h.Store)
	return true
}

func serveNotificationGroupProjectionRoute(w http.ResponseWriter, r *http.Request, verifier PrincipalVerifier, store groupProjectionHTTPStore) {
	if r.Method != http.MethodPost {
		writeGroupDomainError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	if verifier == nil || store == nil {
		writeGroupDomainError(w, http.StatusServiceUnavailable, "notification_unavailable", nil)
		return
	}
	principal, err := verifier.VerifySourceProjection(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeGroupDomainError(w, http.StatusUnauthorized, "source_authentication_required", nil)
		return
	}
	var request contract.SourceGroupProjectionRequest
	_, digest, err := readManagementBody(r, "SourceGroupProjectionRequest", &request)
	if err != nil {
		writeGroupDomainError(w, http.StatusBadRequest, "invalid_projection", nil)
		return
	}
	if principal.DeploymentID != request.SourceDeploymentID || !containsString(principal.TenantIDs, request.TenantID) {
		writeGroupDomainError(w, http.StatusForbidden, "source_scope_mismatch", nil)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !validManagementRequest(key, digest) {
		writeGroupDomainError(w, http.StatusBadRequest, "invalid_idempotency_key", nil)
		return
	}
	projection, err := store.RegisterGroupProjectionIdempotent(r.Context(), principal, key, digest, request)
	if err != nil {
		writeGroupStoreError(w, err)
		return
	}
	writeGroupSuccess(w, http.StatusOK, projection)
}

func serveNotificationGroupRoutes(w http.ResponseWriter, r *http.Request, principal UserPrincipal, store groupHTTPStore) {
	setGroupHTTPHeaders(w)
	if store == nil {
		writeGroupDomainError(w, http.StatusServiceUnavailable, "notification_unavailable", nil)
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	collection := "/api/v2/notification-groups"
	if path == collection && r.Method == http.MethodGet {
		if err := requireNotificationScope(principal, "notification:read"); err != nil {
			writeGroupStoreError(w, err)
			return
		}
		page, pageSize, err := parseNotificationGroupPage(r.URL.Query())
		if err != nil {
			writeGroupDomainError(w, http.StatusBadRequest, "invalid_query", nil)
			return
		}
		result, err := loadNotificationGroupPage(r.Context(), store, principal.TenantID, page, pageSize)
		if err != nil {
			writeGroupStoreError(w, err)
			return
		}
		writeGroupSuccess(w, http.StatusOK, result)
		return
	}
	if path == collection && r.Method == http.MethodPost {
		if err := requireNotificationScope(principal, "notification:manage"); err != nil {
			writeGroupStoreError(w, err)
			return
		}
		var request contract.GroupCreate
		_, digest, err := readManagementBody(r, "GroupCreate", &request)
		if err != nil {
			writeGroupDomainError(w, http.StatusBadRequest, "invalid_group", nil)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !validManagementRequest(key, digest) {
			writeGroupDomainError(w, http.StatusBadRequest, "invalid_idempotency_key", nil)
			return
		}
		const operation = "group.create"
		if replay, found, err := store.FindManagementReplay(r.Context(), principal.TenantID, operation, key, digest); err != nil {
			writeGroupStoreError(w, err)
			return
		} else if found {
			if replay.ResourceType != "group" {
				writeGroupDomainError(w, http.StatusConflict, "idempotency_conflict", nil)
				return
			}
			view, err := store.GetGroupRevision(r.Context(), principal.TenantID, replay.ResourceID, replay.Version)
			if err != nil {
				writeGroupStoreError(w, err)
				return
			}
			view, err = attachGroupProjections(r.Context(), store, principal.TenantID, view)
			if err != nil {
				writeGroupStoreError(w, err)
				return
			}
			writeGroupSuccess(w, http.StatusCreated, view)
			return
		}
		if fields := validateGroupCreate(r.Context(), store, principal.TenantID, request); len(fields) > 0 {
			writeGroupDomainError(w, http.StatusUnprocessableEntity, "invalid_group", fields)
			return
		}
		view, err := store.CreateGroup(r.Context(), principal.TenantID, key, digest, request)
		if err != nil {
			writeGroupStoreError(w, err)
			return
		}
		view, err = attachGroupProjections(r.Context(), store, principal.TenantID, view)
		if err != nil {
			writeGroupStoreError(w, err)
			return
		}
		writeGroupSuccess(w, http.StatusCreated, view)
		return
	}
	if !strings.HasPrefix(path, collection+"/") {
		writeGroupDomainError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	id, err := managementPathID(path, collection+"/")
	if err != nil {
		writeGroupDomainError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if err := requireNotificationScope(principal, "notification:read"); err != nil {
			writeGroupStoreError(w, err)
			return
		}
		view, err := store.GetGroup(r.Context(), principal.TenantID, id)
		if err == nil {
			view, err = attachGroupProjections(r.Context(), store, principal.TenantID, view)
		}
		if err != nil {
			writeGroupStoreError(w, err)
			return
		}
		writeGroupSuccess(w, http.StatusOK, view)
	case http.MethodPut:
		if err := requireNotificationScope(principal, "notification:manage"); err != nil {
			writeGroupStoreError(w, err)
			return
		}
		var request contract.GroupUpdate
		_, digest, err := readManagementBody(r, "GroupUpdate", &request)
		if err != nil {
			writeGroupDomainError(w, http.StatusBadRequest, "invalid_group", nil)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !validManagementRequest(key, digest) {
			writeGroupDomainError(w, http.StatusBadRequest, "invalid_idempotency_key", nil)
			return
		}
		operation := "group.update:" + id
		if replay, found, err := store.FindManagementReplay(r.Context(), principal.TenantID, operation, key, digest); err != nil {
			writeGroupStoreError(w, err)
			return
		} else if found {
			if replay.ResourceType != "group" || replay.ResourceID != id {
				writeGroupDomainError(w, http.StatusConflict, "idempotency_conflict", nil)
				return
			}
			view, err := store.GetGroupRevision(r.Context(), principal.TenantID, id, replay.Version)
			if err != nil {
				writeGroupStoreError(w, err)
				return
			}
			view, err = attachGroupProjections(r.Context(), store, principal.TenantID, view)
			if err != nil {
				writeGroupStoreError(w, err)
				return
			}
			writeGroupSuccess(w, http.StatusOK, view)
			return
		}
		if fields := validateGroupUpdate(r.Context(), store, principal.TenantID, request); len(fields) > 0 {
			writeGroupDomainError(w, http.StatusUnprocessableEntity, "invalid_group", fields)
			return
		}
		view, err := store.UpdateGroup(r.Context(), principal.TenantID, id, key, digest, request)
		if err != nil {
			writeGroupStoreError(w, err)
			return
		}
		view, err = attachGroupProjections(r.Context(), store, principal.TenantID, view)
		if err != nil {
			writeGroupStoreError(w, err)
			return
		}
		writeGroupSuccess(w, http.StatusOK, view)
	default:
		writeGroupDomainError(w, http.StatusNotFound, "not_found", nil)
	}
}

func setGroupHTTPHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
}

func writeGroupSuccess(w http.ResponseWriter, status int, data any) {
	requestID := newStableID()
	if requestID == "" {
		requestID = "notification"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.Envelope[any]{Code: 200, Message: "ok", RequestID: requestID, Data: data})
}

func writeGroupDomainError(w http.ResponseWriter, status int, reason string, fields []contract.FieldError) {
	requestID := newStableID()
	if requestID == "" {
		requestID = "notification"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.DomainError{
		Code: status, Message: "notification group request failed",
		Details: contract.ErrorDetail{Reason: reason, Fields: fields}, RequestID: requestID,
	})
}

func writeGroupStoreError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	var core *CoreFailure
	if errors.As(err, &core) {
		writeGroupDomainError(w, core.Status, core.Reason, nil)
		return
	}
	if errors.Is(err, ErrIdentityUnauthorized) {
		writeGroupDomainError(w, http.StatusUnauthorized, "authentication_required", nil)
		return
	}
	if errors.Is(err, ErrTenantContextRequired) {
		writeGroupDomainError(w, http.StatusForbidden, "tenant_context_required", nil)
		return
	}
	if errors.Is(err, ErrIdentityForbidden) {
		writeGroupDomainError(w, http.StatusForbidden, "permission_denied", nil)
		return
	}
	if errors.Is(err, ErrManagementInvalid) {
		writeGroupDomainError(w, http.StatusUnprocessableEntity, "invalid_request", nil)
		return
	}
	if errors.Is(err, ErrManagementNotFound) {
		writeGroupDomainError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	if errors.Is(err, ErrManagementConflict) {
		writeGroupDomainError(w, http.StatusConflict, "conflict", nil)
		return
	}
	status, reason := notificationErrorStatus(err)
	writeGroupDomainError(w, status, reason, nil)
}
