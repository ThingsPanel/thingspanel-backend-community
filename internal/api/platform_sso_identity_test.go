package api

import (
	"project/internal/model"
	"project/pkg/utils"
	"testing"
)

func TestPlatformIdentityCurrentAuthorityAndTenant(t *testing.T) {
	tenant, authority, status := "tenant", "TENANT_USER", "N"
	user := &model.User{ID: "id", Email: "person@example.com", TenantID: &tenant, Authority: &authority, Status: &status}
	claims := &utils.UserClaims{ID: "id", TenantID: tenant, Authority: authority}
	got, ok := currentPlatformIdentity(claims, user)
	if !ok || got.Role != "VIEWER" {
		t.Fatal("ordinary user was elevated or rejected")
	}
	claims.Delegated = true
	if _, ok = currentPlatformIdentity(claims, user); ok {
		t.Fatal("delegation accepted")
	}
	claims.Delegated = false
	claims.TenantID = "other"
	if _, ok = currentPlatformIdentity(claims, user); ok {
		t.Fatal("tenant mismatch accepted")
	}
	claims.TenantID = tenant
	claims.Authority = "TENANT_ADMIN"
	if _, ok = currentPlatformIdentity(claims, user); ok {
		t.Fatal("stale role accepted")
	}
	claims.Authority = authority
	status = "F"
	if _, ok = currentPlatformIdentity(claims, user); ok {
		t.Fatal("frozen accepted")
	}
}
