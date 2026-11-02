package marketinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"encore.app/marketinstall/app"
	"encore.dev/beta/errs"
)

type authorizedLookupReader struct {
	calls int
	op    app.Operation
}

func (r *authorizedLookupReader) Get(_ context.Context, tenant, subject, key string) (app.Operation, error) {
	r.calls++
	if r.op.TenantID != tenant || r.op.SubjectID != subject || r.op.RequestKey != key {
		return app.Operation{}, app.ErrNotFound
	}
	return r.op, nil
}

func TestMarketInstallationReadAuthorizationNativeTransport(t *testing.T) {
	identity := app.Identity{TenantID: "tenant", SubjectID: "subject", AuthorizationRef: "never-transmit-original-grant"}
	credential := strings.Repeat("s", 40)
	var permissionCalls atomic.Int64
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permissionCalls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/internal/market-install/read-authorize" || r.Header.Get("X-TP-Service-Token") != credential || r.Header.Get("X-Token") != "" || r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
			t.Error("native authorization left the trusted service-only transport")
		}
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request) != 3 || request["protocolVersion"] != float64(1) || request["tenantId"] != "tenant" || request["subjectId"] != "subject" {
			t.Errorf("authorization request leaked or substituted claims: %#v", request)
		}
		fmt.Fprint(w, `{"protocolVersion":1,"authorized":true,"tenantId":"tenant","subjectId":"subject","resource":"api/v1/device/market/bundles/install"}`)
	}))
	defer owner.Close()
	provider, err := newHTTPProvider(owner.URL, credential)
	if err != nil {
		t.Fatal(err)
	}
	reader := &authorizedLookupReader{op: app.Operation{ID: "original-id", BundleKey: "bundle", Version: "1", TenantID: "tenant", SubjectID: "subject", RequestKey: "original-key", State: app.Running}}
	result, err := lookupAuthorizedOperation(context.Background(), reader, identity, "original-key", provider.authorizeInstallationManagement)
	if err != nil || result == nil || result.State != app.Confirmed || result.Operation == nil || result.Operation.ID != "original-id" || result.Operation.State != app.Running || reader.calls != 1 || permissionCalls.Load() != 1 {
		t.Fatalf("authorized original read: %#v reads=%d checks=%d error=%v", result, reader.calls, permissionCalls.Load(), err)
	}
}

func TestMarketInstallationReadAuthorizationNativeDeniesBeforeLedger(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"revoked", 403, `{"errorCode":"INSTALLATION_READ_AUTHORIZATION_DENIED"}`},
		{"unavailable", 503, `{"errorCode":"INSTALLATION_READ_AUTHORIZATION_UNKNOWN"}`},
		{"missing", 200, ``}, {"malformed", 200, `{`},
		{"false", 200, `{"protocolVersion":1,"authorized":false,"tenantId":"tenant","subjectId":"subject","resource":"api/v1/device/market/bundles/install"}`},
		{"wrong-version", 200, `{"protocolVersion":2,"authorized":true,"tenantId":"tenant","subjectId":"subject","resource":"api/v1/device/market/bundles/install"}`},
		{"wrong-tenant", 200, `{"protocolVersion":1,"authorized":true,"tenantId":"other","subjectId":"subject","resource":"api/v1/device/market/bundles/install"}`},
		{"wrong-subject", 200, `{"protocolVersion":1,"authorized":true,"tenantId":"tenant","subjectId":"other","resource":"api/v1/device/market/bundles/install"}`},
		{"wrong-resource", 200, `{"protocolVersion":1,"authorized":true,"tenantId":"tenant","subjectId":"subject","resource":"some-other-resource"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status); fmt.Fprint(w, test.body) }))
			defer owner.Close()
			provider, err := newHTTPProvider(owner.URL, strings.Repeat("s", 40))
			if err != nil {
				t.Fatal(err)
			}
			reader := &authorizedLookupReader{}
			result, err := lookupAuthorizedOperation(context.Background(), reader, app.Identity{TenantID: "tenant", SubjectID: "subject"}, "original-key", provider.authorizeInstallationManagement)
			if err == nil || result != nil || reader.calls != 0 {
				t.Fatalf("unauthorized native lookup read ledger: %#v reads=%d error=%v", result, reader.calls, err)
			}
			if test.status == 403 {
				classified, ok := err.(*errs.Error)
				if !ok || classified.Code != errs.PermissionDenied {
					t.Fatalf("known policy revocation was not permission denied: %v", err)
				}
			}
		})
	}
}

func TestMarketInstallationReadAuthorizationNativeRedirectAndUnavailable(t *testing.T) {
	var destinationCalls atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1); w.WriteHeader(200) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	provider, err := newHTTPProvider(redirect.URL, strings.Repeat("s", 40))
	if err != nil {
		t.Fatal(err)
	}
	identity := app.Identity{TenantID: "tenant", SubjectID: "subject"}
	reader := &authorizedLookupReader{}
	if _, err := lookupAuthorizedOperation(context.Background(), reader, identity, "original-key", provider.authorizeInstallationManagement); err == nil || reader.calls != 0 {
		t.Fatal("redirect authorized a native read")
	}
	redirect.Close()
	if _, err := lookupAuthorizedOperation(context.Background(), reader, identity, "original-key", provider.authorizeInstallationManagement); err == nil || reader.calls != 0 {
		t.Fatal("unreachable provider authorized a native read")
	}
	if destinationCalls.Load() != 0 {
		t.Fatal("read authorization followed a credential-bearing redirect")
	}
	if _, err := lookupAuthorizedOperation(context.Background(), reader, identity, "original-key", nil); err == nil || reader.calls != 0 {
		t.Fatal("missing authorizer allowed native read")
	}
	t.Setenv("TP_MARKETINSTALL_PROVIDER_BASE_URL", "")
	t.Setenv("TP_MARKETINSTALL_PROVIDER_TOKEN", "")
	if _, err := configuredInstallationManagementProvider(); err == nil {
		t.Fatal("missing permission provider accepted")
	}
}
