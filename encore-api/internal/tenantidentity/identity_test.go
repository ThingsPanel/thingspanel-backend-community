package tenantidentity

import (
	"testing"

	"github.com/google/uuid"
)

func TestCanonicalIdentityPreservesExistingModelOwners(t *testing.T) {
	for raw, want := range map[string]string{
		"63a4b529":                             "48c68fbd-b8b8-5c24-b524-a67edbd2d6e7",
		"d616bcbb":                             "15ed9916-6e08-5428-9548-301bcef62492",
		"550e8400-e29b-41d4-a716-446655440000": "550e8400-e29b-41d4-a716-446655440000",
	} {
		if got := NormalizePrincipalID(raw); got != want {
			t.Errorf("tenant %q resolved to %q; want existing model owner %q", raw, got, want)
		}
	}
}

func TestCanonicalIdentityDoesNotCollapseLegacyTenants(t *testing.T) {
	pairs := [][2]string{{"KlkRL8qX", "uOr8IGlG"}, {"a-b", "ab"}, {"TENANT-a", "tenant-a"}, {"same-x", "same-y"}}
	for _, pair := range pairs {
		a, b := NormalizePrincipalID(pair[0]), NormalizePrincipalID(pair[1])
		if a == b || a == uuid.Nil.String() || b == uuid.Nil.String() {
			t.Fatalf("distinct tenants collapsed: %q and %q", pair[0], pair[1])
		}
	}
}

func TestCanonicalIdentitySeparatesSubjectAndTenantNamespaces(t *testing.T) {
	if CanonicalIdentity("tenant", "same-source") == CanonicalIdentity("subject", "same-source") {
		t.Fatal("subject and tenant namespaces collapsed")
	}
	if got := NormalizePrincipalID(""); got != uuid.Nil.String() {
		t.Fatal("missing tenant must not become a valid tenant")
	}
}
