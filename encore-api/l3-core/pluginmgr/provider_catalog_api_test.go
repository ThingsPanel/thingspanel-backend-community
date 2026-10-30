package pluginmgr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encore.app/internal/authz"
	"encore.app/internal/nativehttp"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func TestDecodeProviderCatalogListQueryStrictly(t *testing.T) {
	for _, raw := range []string{"", "page=1", "pageSize=100", "page=1000000&pageSize=1"} {
		t.Run("valid_"+strings.ReplaceAll(raw, "&", "_"), func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v2/connection-providers?"+raw, nil)
			query, err := decodeProviderCatalogListQuery(r)
			if err != nil {
				t.Fatalf("decode %q: %v", raw, err)
			}
			if query.page < 1 || query.page > 1_000_000 || query.pageSize < 1 || query.pageSize > 100 {
				t.Fatalf("out-of-contract pagination: %#v", query)
			}
		})
	}
	for _, raw := range []string{
		"page=0", "page=1000001", "pageSize=0", "pageSize=101", "page=abc", "page=", "page=1&page=2",
		"page=1&tenantId=secret-sentinel", "page=%FF", "%FF=1", "page=%Q1", "page=1;pageSize=2",
	} {
		t.Run("invalid_"+strings.ReplaceAll(raw, "&", "_"), func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v2/connection-providers?"+raw, nil)
			if _, err := decodeProviderCatalogListQuery(r); errs.Code(err) != errs.InvalidArgument {
				t.Fatalf("decode %q error=%v, want InvalidArgument", raw, err)
			}
		})
	}
}

func TestConnectionProviderRawBadQueryReturnsProblem(t *testing.T) {
	for _, raw := range []string{"page=1&page=2", "unknown=secret-sentinel", "page=%FF"} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v2/connection-providers?"+raw, nil)
		(&Service{}).ListConnectionProviders(recorder, req)
		if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("query=%q status=%d content-type=%q", raw, recorder.Code, recorder.Header().Get("Content-Type"))
		}
		if recorder.Header().Get("X-Request-Id") == "" {
			t.Fatal("X-Request-Id is missing")
		}
		var problem nativehttp.Problem
		if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
			t.Fatalf("decode Problem: %v", err)
		}
		if problem.Status != http.StatusBadRequest || problem.RequestID != recorder.Header().Get("X-Request-Id") || problem.Instance != req.URL.Path {
			t.Fatalf("Problem contract mismatch: %#v", problem)
		}
		if strings.Contains(recorder.Body.String(), "secret-sentinel") {
			t.Fatalf("Problem echoed raw query: %s", recorder.Body.String())
		}
	}
}

func TestProviderCatalogReadIdentityRequiresVerifiedTenantTemplateReader(t *testing.T) {
	tenantID := uuid.NewString()
	identity, err := providerCatalogReadIdentityFromData(&authn.Data{TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	if err != nil || identity.tenantID != tenantID {
		t.Fatalf("verified tenant admin identity=%#v err=%v", identity, err)
	}
	for name, data := range map[string]*authn.Data{
		"system admin":  {TenantID: tenantID, Roles: []string{"TENANT_ADMIN", "SYS_ADMIN"}},
		"platform role": {TenantID: tenantID, Roles: []string{"platform_admin"}},
		"nil tenant":    {TenantID: uuid.Nil.String(), Roles: []string{"TENANT_ADMIN"}},
		"no reader":     {TenantID: tenantID, Roles: []string{"tenant_viewer"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := providerCatalogReadIdentityFromData(data); err == nil {
				t.Fatal("unauthorized identity accepted")
			}
		})
	}
	if _, err := providerCatalogReadIdentityFromData(&authn.Data{TenantID: tenantID, Roles: []string{"TENANT_ADMIN", "platform_admin"}}); err != nil {
		t.Fatal("verified tenant admin must follow the same shared boundary as device and asset reads")
	}
	if !authz.Can(&authn.Data{Roles: []string{"TENANT_ADMIN"}}, "template", "read") {
		t.Fatal("test principal lacks the existing template:read mapping")
	}
}

func TestNativeProviderPublicDTOHasOnlyOASFields(t *testing.T) {
	release := ProviderRelease{
		ProviderID:                    "123e4567-e89b-12d3-a456-426614174000",
		Name:                          "public provider",
		Version:                       "1.opaque",
		Protocols:                     []string{},
		DeviceKinds:                   []string{"standalone"},
		SchemaDigest:                  strings.Repeat("a", 64),
		ArtifactDigest:                "artifact-secret-sentinel",
		ManifestDigest:                "manifest-secret-sentinel",
		OriginalConfigurationSchema:   json.RawMessage(`{"type":"object"}`),
		OriginalDeviceParameterSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
	}
	item := nativeConnectionProviderFromRelease(release)
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	want := []string{"id", "name", "version", "protocols", "deviceKinds", "schemaDigest", "configurationSchema", "deviceParameterSchema", "instances"}
	if len(fields) != len(want) {
		t.Fatalf("public fields=%v, want exactly %v", fields, want)
	}
	for _, field := range want {
		if _, ok := fields[field]; !ok {
			t.Errorf("public field %q missing", field)
		}
	}
	if strings.Contains(string(encoded), "artifact-secret-sentinel") || strings.Contains(string(encoded), "manifest-secret-sentinel") {
		t.Fatalf("internal digest leaked in public DTO: %s", encoded)
	}
	var decoded NativeConnectionProvider
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Instances == nil || len(decoded.Instances) != 0 {
		t.Fatalf("instances must be an empty array: %#v err=%v", decoded.Instances, err)
	}
}

func TestProviderCatalogProblemRedactsUnexpectedServerError(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeProviderCatalogProblem(recorder, "safe-request-id", "/api/v2/connection-providers", errors.New("database password sentinel"))
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status=%d headers=%v", recorder.Code, recorder.Header())
	}
	if strings.Contains(recorder.Body.String(), "database password sentinel") {
		t.Fatalf("server error leaked internal detail: %s", recorder.Body.String())
	}
}

func TestProviderCatalogReadSnapshotUsesVerifiedTenant(t *testing.T) {
	ctx := context.Background()
	identity, err := providerCatalogReadIdentityFromData(&authn.Data{TenantID: uuid.NewString(), Roles: []string{"TENANT_ADMIN"}})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := beginProviderCatalogReadTx(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var configuredTenant, isolation, readOnly string
	if err := tx.QueryRow(ctx, `SELECT current_setting('app.current_tenant_id'), current_setting('transaction_isolation'), current_setting('transaction_read_only')`).Scan(&configuredTenant, &isolation, &readOnly); err != nil {
		t.Fatal(err)
	}
	if configuredTenant != identity.tenantID || isolation != "repeatable read" || readOnly != "on" {
		t.Fatalf("catalog snapshot context tenant=%q isolation=%q readonly=%q", configuredTenant, isolation, readOnly)
	}
	items, total, err := listProviderReleasesTx(ctx, tx, 1, 20)
	if err != nil {
		t.Fatalf("read existing catalog snapshot: %v", err)
	}
	if items == nil || total < 0 || int64(len(items)) > total {
		t.Fatalf("invalid catalog page items=%#v total=%d", items, total)
	}
}
