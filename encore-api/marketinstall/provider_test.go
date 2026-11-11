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
)

func TestMarketInstallProviderUsesFixedAuthenticatedContract(t *testing.T) {
	var calls atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/internal/market-install/query" || r.Header.Get("X-TP-Service-Token") != "fixture-token" {
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "invalid", 400)
			return
		}
		var req app.StepRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.TenantID != "trusted-tenant" || req.SubjectID != "trusted-subject" || req.OperationID != "original" || req.IdempotencyKey != strings.Repeat("a", 64) {
			t.Error("provider identity changed")
		}
		fmt.Fprint(w, `{"state":"not_observed"}`)
	}))
	defer fake.Close()
	provider, err := newHTTPProvider(fake.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	observation, err := provider.Query(context.Background(), app.StepRequest{ProtocolVersion: 1, Identity: app.Identity{TenantID: "trusted-tenant", SubjectID: "trusted-subject", AuthorizationRef: "opaque"}, OperationID: "original", IdempotencyKey: strings.Repeat("a", 64)})
	if err != nil || observation.State != app.NotObserved || calls.Load() != 1 {
		t.Fatalf("query: %#v %v", observation, err)
	}
}
func TestMarketInstallProviderNeverFollowsCredentialRedirect(t *testing.T) {
	var leaked atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1); fmt.Fprint(w, `{"state":"confirmed"}`) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	provider, err := newHTTPProvider(redirect.URL, "credential")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Query(context.Background(), app.StepRequest{}); err == nil {
		t.Fatal("redirect became an owner observation")
	}
	if leaked.Load() != 0 {
		t.Fatal("provider service credential forwarded to redirect")
	}
}
