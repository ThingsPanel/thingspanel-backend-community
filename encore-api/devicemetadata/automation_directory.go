package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"encore.app/internal/tenantidentity"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

const automationDirectoryMaxResults = 10000

type ResolveAutomationDirectoryRequest struct {
	TenantID   string   `json:"tenantId"`
	Selector   string   `json:"selector"`
	IDs        []string `json:"ids,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	MatchAll   bool     `json:"matchAll,omitempty"`
	DeviceKind string   `json:"deviceKind,omitempty"`
}

type NativeAutomationDirectoryDevice struct {
	ID         string          `json:"id"`
	TenantID   string          `json:"tenantId"`
	Name       string          `json:"name"`
	TemplateID *string         `json:"templateId"`
	DeviceKind string          `json:"deviceKind"`
	Labels     json.RawMessage `json:"labels"`
}

type ResolveAutomationDirectoryResponse struct {
	Devices  []NativeAutomationDirectoryDevice `json:"devices"`
	Complete bool                              `json:"complete"`
}

type ValidateAutomationScopeRequest struct {
	TenantID  string `json:"tenantId"`
	ScopeType string `json:"scopeType"`
	ScopeID   string `json:"scopeId"`
}

type ValidateAutomationScopeResponse struct {
	ID        string `json:"id"`
	ScopeType string `json:"scopeType"`
}

// ResolveAutomationDirectory is an internal owner-to-owner directory RPC. It
// is deliberately private: automation rules must resolve against this owner's
// tenant-scoped native records, never the legacy public device API.
//
//encore:api private
func (s *Service) ResolveAutomationDirectory(ctx context.Context, req *ResolveAutomationDirectoryRequest) (*ResolveAutomationDirectoryResponse, error) {
	if req == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "directory request is required"}
	}
	tenantID, err := canonicalAutomationTenant(req.TenantID)
	if err != nil {
		return nil, err
	}
	var scopedIDs []string
	if data := authn.CurrentData(); data != nil {
		rawTenant, err := tenantidentity.RequiredTenantID(data)
		if err != nil {
			return nil, err
		}
		if tenantidentity.NormalizePrincipalID(rawTenant) != tenantID {
			return nil, &errs.Error{Code: errs.PermissionDenied, Message: "directory tenant does not match authenticated tenant"}
		}
		for _, rawID := range data.AuthorizedDeviceIDs {
			id, err := canonicalAutomationUUID(rawID, "authorized device id")
			if err != nil {
				return nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated device scope"}
			}
			scopedIDs = append(scopedIDs, id)
		}
	}
	query, args, err := automationDirectoryQuery(tenantID, *req, scopedIDs)
	if err != nil {
		return nil, err
	}
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, errs.WrapCode(err, errs.Unavailable, "native device directory query failed")
	}
	defer rows.Close()
	out := &ResolveAutomationDirectoryResponse{Devices: make([]NativeAutomationDirectoryDevice, 0), Complete: true}
	for rows.Next() {
		var d NativeAutomationDirectoryDevice
		var templateID *string
		var labels []byte
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Name, &templateID, &d.DeviceKind, &labels); err != nil {
			return nil, errs.WrapCode(err, errs.Unavailable, "native device directory row is invalid")
		}
		if len(out.Devices) == automationDirectoryMaxResults {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "native device directory result exceeds the complete-result limit"}
		}
		d.TemplateID = templateID
		if len(labels) == 0 {
			d.Labels = json.RawMessage(`{}`)
		} else {
			d.Labels = append(json.RawMessage(nil), labels...)
		}
		out.Devices = append(out.Devices, d)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.WrapCode(err, errs.Unavailable, "native device directory query failed")
	}
	return out, nil
}

// ValidateAutomationScope confirms that an automation target is a live native
// device or template owned by the requested tenant.
//
//encore:api private
func (s *Service) ValidateAutomationScope(ctx context.Context, req *ValidateAutomationScopeRequest) (*ValidateAutomationScopeResponse, error) {
	if req == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "scope request is required"}
	}
	tenantID, err := canonicalAutomationTenant(req.TenantID)
	if err != nil {
		return nil, err
	}
	scopeID, err := canonicalAutomationUUID(req.ScopeID, "scopeId")
	if err != nil {
		return nil, err
	}
	if req.ScopeType != "device" && req.ScopeType != "device_config" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "scopeType must be device or device_config"}
	}
	var scopedIDs []string
	if data := authn.CurrentData(); data != nil {
		rawTenant, err := tenantidentity.RequiredTenantID(data)
		if err != nil {
			return nil, err
		}
		if tenantidentity.NormalizePrincipalID(rawTenant) != tenantID {
			return nil, &errs.Error{Code: errs.PermissionDenied, Message: "scope tenant does not match authenticated tenant"}
		}
		for _, rawID := range data.AuthorizedDeviceIDs {
			id, err := canonicalAutomationUUID(rawID, "authorized device id")
			if err != nil {
				return nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated device scope"}
			}
			scopedIDs = append(scopedIDs, id)
		}
		if req.ScopeType == "device_config" && len(scopedIDs) > 0 {
			return nil, &errs.Error{Code: errs.PermissionDenied, Message: "template scope requires tenant-wide authorization"}
		}
	}
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var foundID string
	if req.ScopeType == "device" {
		if len(scopedIDs) > 0 && !containsAutomationID(scopedIDs, scopeID) {
			return nil, &errs.Error{Code: errs.NotFound, Message: "automation scope not found"}
		}
		err = tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, scopeID).Scan(&foundID)
	} else {
		err = tx.QueryRow(ctx, `SELECT id::text FROM device_access_templates WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, scopeID).Scan(&foundID)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
			return nil, &errs.Error{Code: errs.NotFound, Message: "automation scope not found"}
		}
		return nil, errs.WrapCode(err, errs.Unavailable, "native automation scope lookup failed")
	}
	return &ValidateAutomationScopeResponse{ID: foundID, ScopeType: req.ScopeType}, nil
}

func containsAutomationID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func canonicalAutomationTenant(raw string) (string, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || len(raw) != 36 || id.String() != raw {
		return "", &errs.Error{Code: errs.InvalidArgument, Message: "tenantId must be a canonical UUID"}
	}
	return id.String(), nil
}

func canonicalAutomationUUID(raw, field string) (string, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || len(raw) != 36 || id.String() != raw {
		return "", &errs.Error{Code: errs.InvalidArgument, Message: field + " must be a canonical UUID"}
	}
	return id.String(), nil
}

func automationDirectoryQuery(tenantID string, req ResolveAutomationDirectoryRequest, scopeIDs []string) (string, []any, error) {
	args := []any{tenantID}
	where := []string{"d.tenant_id=$1", "d.deleted_at IS NULL"}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	addIDs := func(ids []string, column string) (string, error) {
		if len(ids) > 1000 {
			return "", &errs.Error{Code: errs.InvalidArgument, Message: "directory selector accepts at most 1000 ids"}
		}
		canonical := make([]string, 0, len(ids))
		for _, raw := range ids {
			id, err := canonicalAutomationUUID(raw, "selector id")
			if err != nil {
				return "", err
			}
			canonical = append(canonical, id)
		}
		return column + "=ANY(" + arg(canonical) + "::uuid[])", nil
	}
	switch req.Selector {
	case "ids":
		p, err := addIDs(req.IDs, "d.id")
		if err != nil {
			return "", nil, err
		}
		where = append(where, p)
	case "templates":
		p, err := addIDs(req.IDs, "b.template_id")
		if err != nil {
			return "", nil, err
		}
		where = append(where, p)
	case "labels":
		if len(req.Labels) == 0 || len(req.Labels) > 100 {
			return "", nil, &errs.Error{Code: errs.InvalidArgument, Message: "labels selector requires 1 to 100 labels"}
		}
		predicates := make([]string, 0, len(req.Labels))
		for _, token := range req.Labels {
			parts := strings.SplitN(token, "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) != parts[0] || parts[0] == "" {
				return "", nil, &errs.Error{Code: errs.InvalidArgument, Message: "labels must be exact key=value tokens"}
			}
			encoded, _ := json.Marshal(map[string]string{parts[0]: parts[1]})
			predicates = append(predicates, "d.labels @> "+arg(string(encoded))+"::jsonb")
		}
		joiner := " AND "
		if !req.MatchAll {
			joiner = " OR "
		}
		where = append(where, "("+strings.Join(predicates, joiner)+")")
	case "kind":
		kind := req.DeviceKind
		switch kind {
		case "1":
			kind = "standalone"
		case "2":
			kind = "gateway"
		case "3":
			kind = "subdevice"
		}
		if kind != "standalone" && kind != "gateway" && kind != "subdevice" {
			return "", nil, &errs.Error{Code: errs.InvalidArgument, Message: "deviceKind must be standalone, gateway, or subdevice"}
		}
		where = append(where, "d.device_kind="+arg(kind))
	default:
		return "", nil, &errs.Error{Code: errs.InvalidArgument, Message: "unsupported directory selector"}
	}
	if len(scopeIDs) > 0 {
		where = append(where, "d.id=ANY("+arg(scopeIDs)+"::uuid[])")
	}
	query := `SELECT d.id::text,d.tenant_id::text,d.name,b.template_id::text,d.device_kind,d.labels FROM devices d LEFT JOIN device_access_bindings b ON b.tenant_id=d.tenant_id AND b.device_id=d.id WHERE ` + strings.Join(where, " AND ") + ` ORDER BY d.id LIMIT 10001`
	return query, args, nil
}
