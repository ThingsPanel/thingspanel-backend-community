package marketinstall

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"encore.app/marketinstall/app"
	"encore.app/marketinstall/store"
	"encore.dev/beta/errs"
)

// These probes exercise the actual public Service methods and coordinator.
// They are unit-level permission plumbing, not JWT/session acceptance evidence.
type managementLedgerProbe struct {
	app.Store
	reads, creates int
}

func (s *managementLedgerProbe) Get(context.Context, string, string, string) (app.Operation, error) {
	s.reads++
	return app.Operation{}, app.ErrNotFound
}
func (s *managementLedgerProbe) Create(context.Context, app.Operation) (app.Operation, error) {
	s.creates++
	return app.Operation{}, errors.New("unexpected ledger write")
}

type managementProviderProbe struct {
	app.Provider
	prepares     int
	identity     app.Identity
	prepareError error
}

func (p *managementProviderProbe) Prepare(_ context.Context, req app.PrepareRequest) (app.Plan, error) {
	p.prepares++
	p.identity = req.Identity
	return app.Plan{}, p.prepareError
}

func TestMarketInstallationManagementPublicEntriesDenyBeforeOwnerAccess(t *testing.T) {
	for _, decision := range []struct {
		name   string
		status int
		body   string
		denied bool
	}{
		{"revoked", http.StatusForbidden, `{}`, true},
		{"permission-owner-unavailable", http.StatusServiceUnavailable, `{}`, false},
		{"invalid-echo", http.StatusOK, `{"protocolVersion":1,"authorized":true,"resource":"unrelated"}`, true},
	} {
		t.Run(decision.name, func(t *testing.T) {
			var checks atomic.Int64
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				checks.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/internal/market-install/read-authorize" {
					t.Error("unexpected authorization request")
				}
				w.WriteHeader(decision.status)
				_, _ = w.Write([]byte(decision.body))
			}))
			defer owner.Close()
			t.Setenv("TP_MARKETINSTALL_PROVIDER_BASE_URL", owner.URL)
			t.Setenv("TP_MARKETINSTALL_PROVIDER_TOKEN", strings.Repeat("s", 40))
			for _, entry := range []string{"install", "get-by-id", "get-by-key", "resume"} {
				t.Run(entry, func(t *testing.T) {
					ledger, provider := &managementLedgerProbe{}, &managementProviderProbe{}
					// A nil database will panic if Get/Resume/GetByKey reaches the real
					// ledger. It cannot make a missing authorization guard look safe.
					service := &Service{ledger: store.New(nil), coordinator: &app.Coordinator{Store: ledger, Provider: provider, NewID: func() string { return "unexpected-new-id" }}}
					before := checks.Load()
					var err error
					switch entry {
					case "install":
						_, err = service.Install(context.Background(), &InstallRequest{IdempotencyKey: "original-key", AuthorizationRef: "original-grant", Request: app.Request{BundleKey: "bundle", Version: "1"}})
					case "get-by-id":
						_, err = service.Get(context.Background(), "original-operation")
					case "get-by-key":
						_, err = service.GetByKey(context.Background(), &OperationLookupRequest{OperationKey: "original-key"})
					case "resume":
						err = service.Resume(context.Background(), "original-operation")
					}
					var classified *errs.Error
					expected := errs.Unavailable
					if decision.denied {
						expected = errs.PermissionDenied
					}
					if !errors.As(err, &classified) || classified.Code != expected || checks.Load() != before+1 || ledger.reads != 0 || ledger.creates != 0 || provider.prepares != 0 {
						t.Fatalf("denied entry reached owner or skipped policy: error=%v checks=%d reads=%d creates=%d prepare=%d", err, checks.Load()-before, ledger.reads, ledger.creates, provider.prepares)
					}
				})
			}
		})
	}
}

func TestMarketInstallationManagementDoesNotReplaceOriginalExecutionGrant(t *testing.T) {
	identity, err := installationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	var checks atomic.Int64
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks.Add(1)
		if r.URL.Path != "/internal/market-install/read-authorize" {
			t.Error("unexpected permission endpoint")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"protocolVersion": 1, "authorized": true, "tenantId": identity.TenantID, "subjectId": identity.SubjectID, "resource": installationReadResource})
	}))
	defer owner.Close()
	t.Setenv("TP_MARKETINSTALL_PROVIDER_BASE_URL", owner.URL)
	t.Setenv("TP_MARKETINSTALL_PROVIDER_TOKEN", strings.Repeat("s", 40))
	ledger := &managementLedgerProbe{}
	provider := &managementProviderProbe{prepareError: errors.New("original execution grant rejected")}
	service := &Service{coordinator: &app.Coordinator{Store: ledger, Provider: provider, NewID: func() string { return "unexpected-new-id" }}}
	result, err := service.Install(context.Background(), &InstallRequest{IdempotencyKey: "original-key", AuthorizationRef: "original-revoked-grant", Request: app.Request{BundleKey: "bundle", Version: "1"}})
	if err == nil || result != nil || checks.Load() != 1 || ledger.reads != 1 || ledger.creates != 0 || provider.prepares != 1 || provider.identity.AuthorizationRef != "original-revoked-grant" || provider.identity.TenantID != identity.TenantID || provider.identity.SubjectID != identity.SubjectID {
		t.Fatalf("current management permission replaced original execution constraints: result=%#v error=%v reads=%d creates=%d prepare=%d", result, err, ledger.reads, ledger.creates, provider.prepares)
	}
}
