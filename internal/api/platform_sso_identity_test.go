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

func TestPlatformIdentityPreservesLegacySpaces(t *testing.T) {
	tenant, authority, status := "tenant-a", "SYS_ADMIN", "N"
	user := &model.User{ID: "id", Email: "admin@example.com", TenantID: &tenant, Authority: &authority, Status: &status}
	claims := &utils.UserClaims{ID: "id", TenantID: tenant, Authority: authority}
	identity, ok := currentPlatformIdentity(claims, user)
	if !ok || identity.TenantID != "thingspanel-sys-admin" || identity.Role != "SUPER_ADMIN" {
		t.Fatal("legacy admin space changed")
	}
	authority = "TENANT_ADMIN"
	claims.Authority = authority
	identity, ok = currentPlatformIdentity(claims, user)
	if !ok || identity.TenantID != tenant || identity.Role != "TENANT_ADMIN" {
		t.Fatal("tenant space changed")
	}
}

func TestPlatformIdentityLegacyAdminWithoutTenant(t *testing.T) {
	authority, status := "SYS_ADMIN", "N"
	user := &model.User{ID: "id", Email: "admin@example.com", Authority: &authority, Status: &status}
	identity, ok := currentPlatformIdentity(&utils.UserClaims{ID: "id", Authority: authority}, user)
	if !ok || identity.TenantID != "thingspanel-sys-admin" {
		t.Fatal("legacy administrator without tenant rejected")
	}
	authority = "TENANT_ADMIN"
	if _, ok := currentPlatformIdentity(&utils.UserClaims{ID: "id", Authority: authority}, user); ok {
		t.Fatal("tenant account without tenant accepted")
	}
}
