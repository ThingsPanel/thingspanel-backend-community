package devicemetadata

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"encore.dev"
)

// NativeTemplateReference is one resource that still uses a template. This
// service only owns devices; products, automation and market packages report
// through the cross-service reference protocol, which is not implemented yet.
type NativeTemplateReference struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type NativeTemplateReferencePage struct {
	Items    []NativeTemplateReference `json:"items"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"pageSize"`
	Total    int                       `json:"total"`
}

type NativeTemplateReferencePageResponse struct {
	Data NativeTemplateReferencePage `json:"data"`
	Meta RequestMeta                 `json:"meta"`
}

func decodeNativeReferenceQuery(values url.Values) (page, pageSize int, err error) {
	page, pageSize = 1, 20
	for _, field := range []struct {
		name string
		max  int
		out  *int
	}{{"page", 1000000, &page}, {"pageSize", 100, &pageSize}} {
		if values.Has(field.name) {
			value, convErr := strconv.Atoi(values.Get(field.name))
			if convErr != nil || value < 1 || value > field.max {
				return 0, 0, invalidGroupQuery("invalid reference pagination")
			}
			*field.out = value
		}
	}
	return page, pageSize, nil
}

// readNativeTemplateReferences lists the live devices bound to a template, newest first.
func readNativeTemplateReferences(ctx context.Context, identity nativeTemplateReadIdentity, id string, page, pageSize int) (*NativeTemplateReferencePage, error) {
	tx, err := beginTenantScopedSQLTx(ctx, identity.tenantID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// A template the caller cannot see answers NotFound, not an empty list.
	if _, err := loadNativeDeviceAccessTemplateTx(ctx, tx, identity.tenantID, id, identity.deviceIDs); err != nil {
		return nil, err
	}
	out := &NativeTemplateReferencePage{Items: []NativeTemplateReference{}, Page: page, PageSize: pageSize}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM device_access_bindings b JOIN devices d ON d.tenant_id=b.tenant_id AND d.id=b.device_id
		WHERE b.tenant_id=$1 AND b.template_id=$2 AND d.deleted_at IS NULL`, identity.tenantID, id).Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT d.id::text, COALESCE(NULLIF(d.name,''), d.device_number)
		FROM device_access_bindings b JOIN devices d ON d.tenant_id=b.tenant_id AND d.id=b.device_id
		WHERE b.tenant_id=$1 AND b.template_id=$2 AND d.deleted_at IS NULL
		ORDER BY d.created_at DESC, d.id DESC LIMIT $3 OFFSET $4`, identity.tenantID, id, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item := NativeTemplateReference{Kind: "device"}
		if err := rows.Scan(&item.ID, &item.DisplayName); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

//encore:api auth raw method=GET path=/api/v2/device-access-templates/:templateID/references
func (s *Service) ListNativeDeviceAccessTemplateReferences(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeTemplateReadIdentity()
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	values, err := parseNativeReadValues(r, []string{"page", "pageSize"})
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	page, pageSize, err := decodeNativeReferenceQuery(values)
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	id, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("templateID"))
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	result, err := readNativeTemplateReferences(r.Context(), identity, id, page, pageSize)
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, "", NativeTemplateReferencePageResponse{Data: *result, Meta: RequestMeta{RequestID: requestID}})
}
