package devicemetadata

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
)

type nativeTemplateReadIdentity struct {
	tenantID  string
	deviceIDs []string
}

func currentNativeTemplateReadIdentity() (nativeTemplateReadIdentity, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nativeTemplateReadIdentity{}, err
	}
	data := authn.CurrentData()
	if !authz.Can(data, "template", "read") {
		return nativeTemplateReadIdentity{}, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to read device access templates"}
	}
	identity := nativeTemplateReadIdentity{tenantID: tenantID}
	for _, rawID := range data.AuthorizedDeviceIDs {
		id, err := nativeReadUUID(rawID)
		if err != nil {
			return nativeTemplateReadIdentity{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated device scope"}
		}
		identity.deviceIDs = append(identity.deviceIDs, id)
	}
	return identity, nil
}

type NativeDeviceAccessTemplatePage struct {
	Items    []NativeDeviceAccessTemplate `json:"items"`
	Page     int                          `json:"page"`
	PageSize int                          `json:"pageSize"`
	Total    int64                        `json:"total"`
}

type NativeDeviceAccessTemplatePageResponse struct {
	Data NativeDeviceAccessTemplatePage `json:"data"`
	Meta RequestMeta                    `json:"meta"`
}

type NativeDeviceAccessTemplateResponse struct {
	Data NativeDeviceAccessTemplate `json:"data"`
	Meta RequestMeta                `json:"meta"`
}

var nativeTemplateReadQueryKeys = []string{"page", "pageSize", "search", "deviceKind", "protocol", "brand", "tag", "modelId", "administrativeState", "sort"}

func decodeNativeTemplateListQuery(values url.Values) (nativeTemplateListQuery, error) {
	query := nativeTemplateListQuery{
		Page: 1, PageSize: 20, Search: values.Get("search"), DeviceKind: values.Get("deviceKind"),
		Protocol: values.Get("protocol"), Brand: values.Get("brand"), Tag: values.Get("tag"),
		ModelID: values.Get("modelId"), AdministrativeState: values.Get("administrativeState"), Sort: values.Get("sort"),
		ProtocolSet: values.Has("protocol"), BrandSet: values.Has("brand"), TagSet: values.Has("tag"),
	}
	for _, field := range []struct {
		name string
		max  int
		out  *int
	}{{"page", 1000000, &query.Page}, {"pageSize", 100, &query.PageSize}} {
		if values.Has(field.name) {
			value, err := strconv.Atoi(values.Get(field.name))
			if err != nil || value < 1 || value > field.max {
				return query, invalidGroupQuery("invalid template pagination")
			}
			*field.out = value
		}
	}
	for _, name := range []string{"deviceKind", "modelId", "administrativeState", "sort"} {
		if values.Has(name) && values.Get(name) == "" {
			return query, invalidGroupQuery("template enum and identifier filters must not be empty")
		}
	}
	if values.Has("modelId") {
		id, err := nativeReadUUID(query.ModelID)
		if err != nil {
			return query, invalidGroupQuery("invalid template modelId")
		}
		query.ModelID = id
	}
	return normalizeNativeTemplateListQuery(query)
}

func readNativeTemplatePage(ctx context.Context, identity nativeTemplateReadIdentity, query nativeTemplateListQuery, requestID string) (*NativeDeviceAccessTemplatePageResponse, error) {
	query, err := normalizeNativeTemplateListQuery(query)
	if err != nil {
		return nil, err
	}
	tx, err := beginTenantScopedSQLTx(ctx, identity.tenantID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, total, err := listNativeDeviceAccessTemplatesTx(ctx, tx, identity.tenantID, identity.deviceIDs, query)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &NativeDeviceAccessTemplatePageResponse{
		Data: NativeDeviceAccessTemplatePage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total},
		Meta: RequestMeta{RequestID: requestID},
	}, nil
}

func readNativeTemplate(ctx context.Context, identity nativeTemplateReadIdentity, id string) (*NativeDeviceAccessTemplate, error) {
	tx, err := beginTenantScopedSQLTx(ctx, identity.tenantID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	template, err := loadNativeDeviceAccessTemplateTx(ctx, tx, identity.tenantID, id, identity.deviceIDs)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return template, nil
}

// Templates expose only imageAssetId. File storage owns authorization and
// short-lived download URLs; clients resolve covers through FileAsset GET.
func nativeTemplateETag(template *NativeDeviceAccessTemplate) (string, error) {
	encoded, err := json.Marshal(template)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`"sha256-%x"`, sha256.Sum256(encoded)), nil
}

func writeNativeTemplateReadProblem(w http.ResponseWriter, r *http.Request, requestID string, err error) {
	code := errs.Code(err)
	if code == errs.Internal || code == errs.Unknown || code == errs.Unavailable {
		rlog.Error("native template read failed", "request_id", requestID, "code", code.String())
	}
	writeGroupReadProblem(w, requestID, r.URL.Path, err)
}

//encore:api auth raw method=GET path=/api/v2/device-access-templates
func (s *Service) ListNativeDeviceAccessTemplates(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeTemplateReadIdentity()
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	values, err := parseNativeReadValues(r, nativeTemplateReadQueryKeys)
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	query, err := decodeNativeTemplateListQuery(values)
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	page, err := readNativeTemplatePage(r.Context(), identity, query, requestID)
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, "", page)
}

//encore:api auth raw method=GET path=/api/v2/device-access-templates/:templateID
func (s *Service) GetNativeDeviceAccessTemplate(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeTemplateReadIdentity()
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	id, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("templateID"))
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	template, err := readNativeTemplate(r.Context(), identity, id)
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	etag, err := nativeTemplateETag(template)
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, etag, NativeDeviceAccessTemplateResponse{Data: *template, Meta: RequestMeta{RequestID: requestID}})
}
