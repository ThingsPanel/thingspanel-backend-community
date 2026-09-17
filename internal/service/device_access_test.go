package service

import (
	"testing"

	"project/pkg/utils"
)

func TestHasFullDeviceAccess(t *testing.T) {
	tests := []struct {
		name      string
		authority string
		want      bool
	}{
		{name: "tenant admin", authority: "TENANT_ADMIN", want: true},
		{name: "system admin", authority: "SYS_ADMIN", want: true},
		{name: "tenant user", authority: "TENANT_USER", want: false},
		{name: "empty authority", authority: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasFullDeviceAccess(&utils.UserClaims{Authority: tt.authority})
			if got != tt.want {
				t.Fatalf("hasFullDeviceAccess(%q) = %v, want %v", tt.authority, got, tt.want)
			}
		})
	}
	if hasFullDeviceAccess(nil) {
		t.Fatal("nil claims must not have device access")
	}
}

func TestEnsureTenantDeviceAdministrator(t *testing.T) {
	if err := ensureTenantDeviceAdministrator(&utils.UserClaims{Authority: "TENANT_ADMIN"}); err != nil {
		t.Fatalf("tenant admin should be allowed: %v", err)
	}
	if err := ensureTenantDeviceAdministrator(&utils.UserClaims{Authority: "TENANT_USER"}); err == nil {
		t.Fatal("tenant user must not create or activate devices")
	}
}
