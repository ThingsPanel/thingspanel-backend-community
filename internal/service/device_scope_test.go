package service

import (
	"testing"

	dal "project/internal/dal"
	"project/pkg/utils"
)

func TestCanReadTenantDevice(t *testing.T) {
	tests := []struct {
		name           string
		claims         *utils.UserClaims
		deviceTenantID string
		want           bool
	}{
		{
			name:           "tenant admin cannot read another tenant",
			claims:         &utils.UserClaims{Authority: "TENANT_ADMIN", TenantID: "tenant-a"},
			deviceTenantID: "tenant-b",
			want:           false,
		},
		{
			name:           "system admin can read across tenants",
			claims:         &utils.UserClaims{Authority: dal.SYS_ADMIN},
			deviceTenantID: "tenant-b",
			want:           true,
		},
		{
			name:           "missing claims deny access",
			claims:         nil,
			deviceTenantID: "tenant-b",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canReadTenantDevice(tt.claims, tt.deviceTenantID); got != tt.want {
				t.Fatalf("canReadTenantDevice() = %v, want %v", got, tt.want)
			}
		})
	}
}
