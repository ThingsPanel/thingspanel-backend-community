package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDictionaryEmptyTenantPlatformIdentityStillNeedsCurrentPermission(t *testing.T) {
	for _, role := range []string{"SYS_ADMIN", "platform_admin"} {
		principal := Principal{SubjectID: "actual-platform-admin", Roles: []string{role}}
		for _, operation := range []Operation{CreateColumn, CreateLanguage, ReadEnum, ReadPage, ReadLanguages, DeleteColumn, DeleteLanguage, ReadProtocols} {
			repository := &recordingRepository{}
			calls := 0
			service := New(repository, authorizationFunc(func(_ context.Context, actual Principal, op Operation) error {
				calls++
				if actual.SubjectID != principal.SubjectID || actual.TenantID != "" || op != operation {
					t.Fatal("global platform identity was replaced with a tenant")
				}
				return ErrUnauthorized
			}), func() string { return "id" }, time.Now)
			if err := service.authorize(context.Background(), principal, operation); !errors.Is(err, ErrUnauthorized) || calls != 1 || repository.calls != 0 {
				t.Fatalf("role=%s operation=%s authorization=%v calls=%d", role, operation, err, calls)
			}
		}
	}
	for _, principal := range []Principal{{SubjectID: "tenant-admin", Roles: []string{"TENANT_ADMIN"}}, {SubjectID: "tenant-user", Roles: []string{"TENANT_USER"}}, {SubjectID: "unknown"}, {SubjectID: "admin", TenantID: " ", Roles: []string{"SYS_ADMIN"}}, {Roles: []string{"SYS_ADMIN"}}} {
		if CompleteIdentity(principal) {
			t.Fatalf("accepted incomplete non-platform identity: %#v", principal)
		}
	}
	principal := Principal{SubjectID: "actual-platform-admin", Roles: []string{"platform_admin"}}
	repository := &recordingRepository{}
	service := New(repository, authorizationFunc(func(_ context.Context, actual Principal, _ Operation) error {
		if actual.TenantID != "" {
			t.Fatal("invented platform tenant")
		}
		return nil
	}), func() string { return "id" }, time.Now)
	if err := service.CreateDictionary(context.Background(), principal, CreateColumnInput{DictCode: "global", DictValue: "entry"}); err != nil || repository.calls != 1 {
		t.Fatalf("authorized platform admin rejected: %v", err)
	}
}
