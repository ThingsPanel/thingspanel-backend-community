package authz

import (
	"testing"

	"encore.app/l1-access/authn"
)

func TestCanMapsNativeGroupAccessOnlyToCommunityTenantAdmin(t *testing.T) {
	cases := []struct {
		name   string
		roles  []string
		scopes []string
		read   bool
		write  bool
	}{
		{name: "verified tenant admin", roles: []string{"TENANT_ADMIN"}, read: true, write: true},
		{name: "sys admin is not a delegated tenant", roles: []string{"SYS_ADMIN"}, read: false, write: false},
		{name: "tenant user cannot self grant", roles: []string{"TENANT_USER"}, scopes: []string{"group:read", "group:write", "*"}},
		{name: "tenant viewer cannot self grant", roles: []string{"TENANT_VIEWER"}, scopes: []string{"group:read", "group:write", "*"}},
		{name: "scope without role cannot grant", scopes: []string{"group:read", "group:write", "*"}},
		{name: "lowercase legacy role is not a Community authority", roles: []string{"tenant_admin"}, read: false, write: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := &authn.Data{Roles: tc.roles, Scopes: tc.scopes}
			if got := Can(data, "group", "read"); got != tc.read {
				t.Errorf("group read = %v, want %v", got, tc.read)
			}
			if got := Can(data, "group", "write"); got != tc.write {
				t.Errorf("group write = %v, want %v", got, tc.write)
			}
		})
	}
}

func TestCanPreservesExistingDeviceAndPluginPolicies(t *testing.T) {
	cases := []struct {
		name     string
		role     string
		resource string
		action   string
		want     bool
	}{
		{name: "tenant admin views device", role: "tenant_admin", resource: "device", action: "view", want: true},
		{name: "tenant admin manages plugin", role: "tenant_admin", resource: "plugin", action: "manage", want: true},
		{name: "tenant operator controls device", role: "tenant_operator", resource: "device", action: "control", want: true},
		{name: "tenant viewer views device", role: "tenant_viewer", resource: "device", action: "view", want: true},
		{name: "viewer cannot manage plugin", role: "tenant_viewer", resource: "plugin", action: "manage", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Can(&authn.Data{Roles: []string{tc.role}}, tc.resource, tc.action); got != tc.want {
				t.Fatalf("Can(%q, %q) = %v, want %v", tc.resource, tc.action, got, tc.want)
			}
		})
	}
}

func TestNativeDeviceReadRequiresVerifiedCommunityAuthority(t *testing.T) {
	for _, role := range []string{"TENANT_ADMIN", "SYS_ADMIN", "TENANT_USER", "TENANT_VIEWER", "tenant_admin", ""} {
		got := Can(&authn.Data{Roles: []string{role}, Scopes: []string{"*", "device:read"}}, "device", "read")
		if got != (role == "TENANT_ADMIN") {
			t.Errorf("native device read role=%q got=%v", role, got)
		}
	}
}

func TestNativeDeviceWriteRequiresVerifiedCommunityTenantAdmin(t *testing.T) {
	for _, role := range []string{"TENANT_ADMIN", "SYS_ADMIN", "TENANT_USER", "TENANT_VIEWER", "tenant_admin", ""} {
		got := Can(&authn.Data{Roles: []string{role}, Scopes: []string{"*", "device:write"}}, "device", "write")
		if got != (role == "TENANT_ADMIN") {
			t.Errorf("native device write role=%q got=%v, want %v", role, got, role == "TENANT_ADMIN")
		}
	}
}

func TestVerifiedTenantAdminCanControlDevicesButOthersCannot(t *testing.T) {
	cases := []struct {
		name  string
		roles []string
		want  bool
	}{
		{name: "verified tenant admin", roles: []string{"TENANT_ADMIN"}, want: true},
		{name: "sys admin is not a delegated tenant", roles: []string{"SYS_ADMIN"}, want: false},
		{name: "tenant user", roles: []string{"TENANT_USER"}, want: false},
		{name: "tenant viewer", roles: []string{"TENANT_VIEWER"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := &authn.Data{Roles: tc.roles, Scopes: []string{"*"}}
			for _, action := range []string{"control", "view"} {
				if got := Can(data, "device", action); got != tc.want {
					t.Errorf("device %s = %v, want %v", action, got, tc.want)
				}
			}
			// manage stays out of reach: the native API does not need it.
			if Can(data, "device", "manage") {
				t.Error("device manage must not be granted to a Community authority")
			}
		})
	}
}
