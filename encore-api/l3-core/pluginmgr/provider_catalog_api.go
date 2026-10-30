package pluginmgr

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"encore.app/internal/authz"
	"encore.app/internal/nativehttp"
	"encore.app/internal/tenantidentity"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

type providerCatalogReadIdentity struct {
	tenantID string
}

type providerCatalogListQuery struct {
	page     int
	pageSize int
}

type NativeConnectionProvider struct {
	ID                    string                   `json:"id"`
	Name                  string                   `json:"name"`
	Version               string                   `json:"version"`
	Protocols             []string                 `json:"protocols"`
	DeviceKinds           []string                 `json:"deviceKinds"`
	SchemaDigest          string                   `json:"schemaDigest"`
	ConfigurationSchema   json.RawMessage          `json:"configurationSchema"`
	DeviceParameterSchema json.RawMessage          `json:"deviceParameterSchema"`
	Instances             []NativeProviderInstance `json:"instances"`
}

type NativeProviderInstance struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type NativeConnectionProviderPage struct {
	Items    []NativeConnectionProvider `json:"items"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"pageSize"`
	Total    int64                      `json:"total"`
}

type NativeConnectionProviderPageResponse struct {
	Data NativeConnectionProviderPage `json:"data"`
	Meta nativehttp.RequestMeta       `json:"meta"`
}

func providerCatalogReadIdentityFromData(data *authn.Data) (providerCatalogReadIdentity, error) {
	rawTenantID, err := tenantidentity.RequiredTenantID(data)
	if err != nil {
		return providerCatalogReadIdentity{}, err
	}
	tenantID := tenantidentity.NormalizePrincipalID(rawTenantID)
	parsed, err := uuid.Parse(tenantID)
	if err != nil || parsed == uuid.Nil || parsed.String() != tenantID {
		return providerCatalogReadIdentity{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated tenant context"}
	}
	if !authz.Can(data, "template", "read") {
		return providerCatalogReadIdentity{}, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to read connection providers"}
	}
	return providerCatalogReadIdentity{tenantID: tenantID}, nil
}

func currentProviderCatalogReadIdentity() (providerCatalogReadIdentity, error) {
	return providerCatalogReadIdentityFromData(authn.CurrentData())
}

func decodeProviderCatalogListQuery(r *http.Request) (providerCatalogListQuery, error) {
	query := providerCatalogListQuery{page: 1, pageSize: 20}
	if r == nil || r.URL == nil {
		return query, invalidProviderCatalogQuery("malformed query string")
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return query, invalidProviderCatalogQuery("malformed query string")
	}
	for key, items := range values {
		if !utf8.ValidString(key) {
			return query, invalidProviderCatalogQuery("invalid query encoding")
		}
		for _, item := range items {
			if !utf8.ValidString(item) {
				return query, invalidProviderCatalogQuery("invalid query encoding")
			}
		}
		if key != "page" && key != "pageSize" {
			return query, invalidProviderCatalogQuery("unsupported query parameter")
		}
		if len(items) != 1 || items[0] == "" {
			return query, invalidProviderCatalogQuery("query parameter must have one non-empty value")
		}
	}
	if raw, ok := values["page"]; ok {
		page, err := strconv.Atoi(raw[0])
		if err != nil || page < 1 || page > 1_000_000 {
			return query, invalidProviderCatalogQuery("page must be between 1 and 1000000")
		}
		query.page = page
	}
	if raw, ok := values["pageSize"]; ok {
		pageSize, err := strconv.Atoi(raw[0])
		if err != nil || pageSize < 1 || pageSize > 100 {
			return query, invalidProviderCatalogQuery("pageSize must be between 1 and 100")
		}
		query.pageSize = pageSize
	}
	return query, nil
}

func invalidProviderCatalogQuery(message string) error {
	return &errs.Error{Code: errs.InvalidArgument, Message: message}
}

func beginProviderCatalogReadTx(ctx context.Context, identity providerCatalogReadIdentity) (*sqldb.Tx, error) {
	parsed, err := uuid.Parse(identity.tenantID)
	if err != nil || parsed == uuid.Nil || parsed.String() != identity.tenantID {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated tenant context"}
	}
	tx, err := providerCatalogDB.Begin(ctx)
	if err != nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "provider catalog unavailable"}
	}
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY`); err != nil {
		_ = tx.Rollback()
		return nil, &errs.Error{Code: errs.Unavailable, Message: "provider catalog unavailable"}
	}
	var configured string
	if err := tx.QueryRow(ctx, `SELECT set_config('app.current_tenant_id', $1, true)`, identity.tenantID).Scan(&configured); err != nil || configured != identity.tenantID {
		_ = tx.Rollback()
		return nil, &errs.Error{Code: errs.Unavailable, Message: "provider catalog unavailable"}
	}
	return tx, nil
}

func listProviderCatalogSnapshot(ctx context.Context, identity providerCatalogReadIdentity, query providerCatalogListQuery, requestID string) (*NativeConnectionProviderPageResponse, error) {
	tx, err := beginProviderCatalogReadTx(ctx, identity)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	releases, total, err := listProviderReleasesTx(ctx, tx, query.page, query.pageSize)
	if err != nil {
		return nil, providerCatalogAPIError(err)
	}
	if total > 9_007_199_254_740_991 {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "provider catalog count is outside the safe range"}
	}
	if err := tx.Commit(); err != nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "provider catalog unavailable"}
	}
	items := make([]NativeConnectionProvider, 0, len(releases))
	for _, release := range releases {
		items = append(items, nativeConnectionProviderFromRelease(release))
	}
	return &NativeConnectionProviderPageResponse{
		Data: NativeConnectionProviderPage{Items: items, Page: query.page, PageSize: query.pageSize, Total: total},
		Meta: nativehttp.RequestMeta{RequestID: requestID},
	}, nil
}

func nativeConnectionProviderFromRelease(release ProviderRelease) NativeConnectionProvider {
	return NativeConnectionProvider{
		ID:                    release.ProviderID,
		Name:                  release.Name,
		Version:               release.Version,
		Protocols:             append([]string{}, release.Protocols...),
		DeviceKinds:           append([]string{}, release.DeviceKinds...),
		SchemaDigest:          release.SchemaDigest,
		ConfigurationSchema:   append(json.RawMessage(nil), release.OriginalConfigurationSchema...),
		DeviceParameterSchema: append(json.RawMessage(nil), release.OriginalDeviceParameterSchema...),
		Instances:             []NativeProviderInstance{},
	}
}

func providerCatalogAPIError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *errs.Error
	if errors.As(err, &apiErr) {
		return err
	}
	switch {
	case errors.Is(err, ErrProviderReleaseNotFound):
		return &errs.Error{Code: errs.NotFound, Message: "provider release not found"}
	case errors.Is(err, ErrProviderReleaseConflict):
		return &errs.Error{Code: errs.AlreadyExists, Message: "provider release version conflicts with published content"}
	case errors.Is(err, ErrProviderCatalogInput):
		return &errs.Error{Code: errs.InvalidArgument, Message: "provider release request is invalid"}
	case errors.Is(err, ErrProviderCatalogCorrupt):
		return &errs.Error{Code: errs.Unavailable, Message: "provider catalog unavailable"}
	default:
		return &errs.Error{Code: errs.Unavailable, Message: "provider catalog unavailable"}
	}
}

func writeProviderCatalogProblem(w http.ResponseWriter, requestID, instance string, err error) {
	nativehttp.WriteProblem(w, requestID, instance, providerCatalogAPIError(err), 0, false)
}

// ListConnectionProviders returns published provider definitions. A catalog
// entry describes schema and protocol capability; it does not imply a deployed
// connector instance exists.
//
//encore:api auth raw method=GET path=/api/v2/connection-providers
func (s *Service) ListConnectionProviders(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	query, err := decodeProviderCatalogListQuery(r)
	if err != nil {
		writeProviderCatalogProblem(w, requestID, r.URL.Path, err)
		return
	}
	identity, err := currentProviderCatalogReadIdentity()
	if err != nil {
		writeProviderCatalogProblem(w, requestID, r.URL.Path, err)
		return
	}
	response, err := listProviderCatalogSnapshot(r.Context(), identity, query, requestID)
	if err != nil {
		writeProviderCatalogProblem(w, requestID, r.URL.Path, err)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}

type ResolveNativeProviderReleaseRequest struct {
	ProviderID   string `json:"providerId"`
	Version      string `json:"version"`
	SchemaDigest string `json:"schemaDigest"`
}

type ResolveNativeProviderReleaseResponse struct {
	ProviderID            string          `json:"providerId"`
	Version               string          `json:"version"`
	SchemaDigest          string          `json:"schemaDigest"`
	ConfigurationSchema   json.RawMessage `json:"configurationSchema"`
	DeviceParameterSchema json.RawMessage `json:"deviceParameterSchema"`
	Protocols             []string        `json:"protocols"`
	DeviceKinds           []string        `json:"deviceKinds"`
}

// ResolveNativeProviderRelease resolves one explicitly pinned immutable release
// for an already-authorized native service caller. It never selects latest.
//
//encore:api private
func (s *Service) ResolveNativeProviderRelease(ctx context.Context, req *ResolveNativeProviderReleaseRequest) (*ResolveNativeProviderReleaseResponse, error) {
	identity, err := currentProviderCatalogReadIdentity()
	if err != nil {
		return nil, providerCatalogAPIError(err)
	}
	if req == nil || !validProviderResolveRequest(req) {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "provider release reference is invalid"}
	}
	tx, err := beginProviderCatalogReadTx(ctx, identity)
	if err != nil {
		return nil, providerCatalogAPIError(err)
	}
	defer tx.Rollback()
	release, err := loadProviderReleaseTx(ctx, tx, req.ProviderID, req.Version)
	if err != nil {
		return nil, providerCatalogAPIError(err)
	}
	if release.SchemaDigest != req.SchemaDigest {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "provider schema digest does not match pinned release"}
	}
	if err := tx.Commit(); err != nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "provider catalog unavailable"}
	}
	return &ResolveNativeProviderReleaseResponse{
		ProviderID:            release.ProviderID,
		Version:               release.Version,
		SchemaDigest:          release.SchemaDigest,
		ConfigurationSchema:   append(json.RawMessage(nil), release.OriginalConfigurationSchema...),
		DeviceParameterSchema: append(json.RawMessage(nil), release.OriginalDeviceParameterSchema...),
		Protocols:             append([]string{}, release.Protocols...),
		DeviceKinds:           append([]string{}, release.DeviceKinds...),
	}, nil
}

func validProviderResolveRequest(req *ResolveNativeProviderReleaseRequest) bool {
	if req == nil || strings.ContainsRune(req.Version, 0) || utf8.RuneCountInString(req.Version) < 1 || utf8.RuneCountInString(req.Version) > 128 || !utf8.ValidString(req.Version) {
		return false
	}
	providerID, err := uuid.Parse(req.ProviderID)
	if err != nil || providerID == uuid.Nil || providerID.String() != req.ProviderID {
		return false
	}
	if len(req.SchemaDigest) != 64 {
		return false
	}
	_, err = hex.DecodeString(req.SchemaDigest)
	if err != nil || strings.ToLower(req.SchemaDigest) != req.SchemaDigest {
		return false
	}
	return true
}
