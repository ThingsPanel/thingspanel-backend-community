package authz

import (
	"testing"

	"encore.app/l1-access/authn"
)

func TestNativeTemplateReadPolicyRequiresTenantAdmin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		roles  []string
		scopes []string
		want   bool
	}{
		{name: "verified tenant administrator", roles: []string{"TENANT_ADMIN"}, want: true},
		{name: "sys admin lacks tenant delegation", roles: []string{"SYS_ADMIN"}},
		{name: "tenant user cannot self grant", roles: []string{"TENANT_USER"}, scopes: []string{"template:read", "*"}},
		{name: "tenant viewer cannot self grant", roles: []string{"TENANT_VIEWER"}, scopes: []string{"template:read", "*"}},
		{name: "scope without role is not authority", scopes: []string{"template:read", "*"}},
		{name: "lowercase role is not verified community identity", roles: []string{"tenant_admin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &authn.Data{Roles: tc.roles, Scopes: tc.scopes}
			if got := Can(data, "template", "read"); got != tc.want {
				t.Fatalf("template read authorization=%v, want %v", got, tc.want)
			}
			if got := Can(data, "template", "write"); got != tc.want {
				t.Fatalf("template write authorization=%v, want %v", got, tc.want)
			}
		})
	}
}
