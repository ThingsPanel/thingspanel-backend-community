package dal

import (
	"context"
	"errors"
	"testing"

	"project/pkg/global"
)

func createNotificationMemberReaderUsersFixture(t *testing.T) {
	t.Helper()
	if err := global.DB.Exec(`CREATE TABLE users (
		id text PRIMARY KEY, tenant_id text NOT NULL, status text NOT NULL,
		authority text NOT NULL, email text NOT NULL, phone_number text NOT NULL)`).Error; err != nil {
		t.Fatal("could not create isolated users fixture")
	}
	rows := []struct{ id, tenant, status, authority, email, phone string }{
		{"member-active", "tenant-active", "N", "TENANT_USER", " active@example.test ", " +12025550101 "},
		{"member-inactive", "tenant-active", "F", "TENANT_USER", "inactive@example.test", "+12025550102"},
		{"member-empty", "tenant-active", "N", "TENANT_USER", "   ", "+12025550103"},
		{"tenant-admin-active", "tenant-active", "N", "TENANT_ADMIN", "admin@example.test", "+12025550104"},
		{"tenant-admin-inactive", "tenant-inactive", "F", "TENANT_ADMIN", "admin2@example.test", "+12025550105"},
		{"tenant-user-only", "tenant-no-admin", "N", "TENANT_USER", "user@example.test", "+12025550106"},
	}
	for _, row := range rows {
		if err := global.DB.Exec(`INSERT INTO users (id, tenant_id, status, authority, email, phone_number) VALUES (?, ?, ?, ?, ?, ?)`, row.id, row.tenant, row.status, row.authority, row.email, row.phone).Error; err != nil {
			t.Fatal("could not insert isolated user fixture")
		}
	}
}

func TestResolveNotificationMemberContactIsTenantAndStatusScoped(t *testing.T) {
	openNotificationSourceFixture(t)
	createNotificationMemberReaderUsersFixture(t)
	ctx := context.Background()

	email, err := ResolveNotificationMemberContact(ctx, "member-active", "tenant-active", "email")
	if err != nil || email.Address != "active@example.test" || email.UserID != "member-active" || email.TenantID != "tenant-active" || email.ContactField != "email" {
		t.Fatalf("active tenant email resolution = %+v, %v", email, err)
	}
	phone, err := ResolveNotificationMemberContact(ctx, "member-active", "tenant-active", "phone")
	if err != nil || phone.Address != "+12025550101" || phone.ContactField != "phone" {
		t.Fatalf("active tenant phone resolution = %+v, %v", phone, err)
	}
	for _, tc := range []struct{ userID, tenantID, field string }{
		{"member-active", "tenant-other", "email"},
		{"member-inactive", "tenant-active", "email"},
		{"member-empty", "tenant-active", "email"},
		{"member-active", "tenant-active", "applicationUserId"},
	} {
		if _, err := ResolveNotificationMemberContact(ctx, tc.userID, tc.tenantID, tc.field); !errors.Is(err, ErrNotificationMemberNotFound) {
			t.Errorf("out-of-scope or unusable contact (%s,%s,%s) returned %v", tc.userID, tc.tenantID, tc.field, err)
		}
	}
}

func TestResolveNotificationTenantRequiresActiveTenantAdmin(t *testing.T) {
	openNotificationSourceFixture(t)
	createNotificationMemberReaderUsersFixture(t)
	ctx := context.Background()
	active, err := ResolveNotificationTenant(ctx, "tenant-active")
	if err != nil || !active.Exists || active.TenantID != "tenant-active" {
		t.Fatalf("active tenant resolution = %+v, %v", active, err)
	}
	for _, tenantID := range []string{"tenant-inactive", "tenant-no-admin", "tenant-unknown"} {
		if _, err := ResolveNotificationTenant(ctx, tenantID); !errors.Is(err, ErrNotificationMemberNotFound) {
			t.Errorf("tenant %q should be absent/inactive, got %v", tenantID, err)
		}
	}
}
