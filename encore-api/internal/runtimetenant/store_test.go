package runtimetenant

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type fixtureVerifier struct{ principals map[string]Principal }

func (v fixtureVerifier) Verify(_ context.Context, token string) (Principal, error) {
	p, ok := v.principals[token]
	if !ok {
		return p, ErrUnproven
	}
	return p, nil
}
func fixtureScope() Scope {
	return Scope{Environment: "isolated-fixture", ProviderID: "fixture-provider", Namespace: "community", ServiceInstanceID: "fixture-instance", CommunityOrigin: "https://community.example.invalid"}
}
func TestProvisionRejectsUnprovenOperatorAuthorityBeforeDatabaseUse(t *testing.T) {
	goodAdmin := Principal{UserID: "fixture-admin", Authority: "SYS_ADMIN"}
	goodTenant := Principal{UserID: "fixture-tenant-admin", RawTenantID: "1234abcd", Authority: "TENANT_ADMIN"}
	for _, test := range []struct {
		name          string
		admin, tenant Principal
	}{
		{"tenant cannot grant platform scope", goodTenant, goodTenant},
		{"delegated admin", Principal{UserID: "fixture-admin", Authority: "SYS_ADMIN", Delegated: true}, goodTenant},
		{"tenant user", goodAdmin, Principal{UserID: "fixture-user", RawTenantID: "1234abcd", Authority: "TENANT_USER"}},
		{"same actor", goodAdmin, Principal{UserID: goodAdmin.UserID, RawTenantID: "1234abcd", Authority: "TENANT_ADMIN"}},
		{"delegated tenant", goodAdmin, Principal{UserID: "fixture-tenant-admin", RawTenantID: "1234abcd", Authority: "TENANT_ADMIN", Delegated: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// A zero DB would panic if authorization reached BeginTx. Every negative
			// case must stop before touching it.
			verifier := fixtureVerifier{map[string]Principal{"admin": test.admin, "tenant": test.tenant}}
			if _, err := Provision(context.Background(), &sql.DB{}, verifier, "admin", "tenant", fixtureScope(), "1234abcd-0000-0000-0000-000000000000", strings.Repeat("a", 64)); !errors.Is(err, ErrUnproven) {
				t.Fatal("unproven operator reached persistence")
			}
		})
	}
}
func TestRuntimeSourceScopeRejectsCredentialURLsAndIncompleteDeployment(t *testing.T) {
	for _, scope := range []Scope{{}, {Environment: "env", ProviderID: "p", Namespace: "community", CommunityOrigin: "https://user:secret@example.invalid"}, {Environment: "env", ProviderID: "p", Namespace: "community", CommunityOrigin: "https://example.invalid?tenant=other"}} {
		if scope.Validate() == nil {
			t.Fatal("unproven deployment scope accepted")
		}
	}
	if fixtureScope().Validate() != nil {
		t.Fatal("complete fixture scope rejected")
	}
}

func TestProvisionFailsClosedUntilEmptyTenantOwnerIsVerified(t *testing.T) {
	verifier := fixtureVerifier{map[string]Principal{"admin": {UserID: "fixture-platform-admin", Authority: "SYS_ADMIN"}, "tenant": {UserID: "fixture-tenant-admin", RawTenantID: "1234abcd", Authority: "TENANT_ADMIN"}}}
	_, err := Provision(context.Background(), &sql.DB{}, verifier, "admin", "tenant", fixtureScope(), "1234abcd-0000-0000-0000-000000000000", strings.Repeat("a", 64))
	if !errors.Is(err, ErrRegistrationPaused) {
		t.Fatalf("unverified native owner registered: %v", err)
	}
}
