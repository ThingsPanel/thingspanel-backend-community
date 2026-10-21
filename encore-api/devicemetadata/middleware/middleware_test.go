package middleware

import (
	"context"
	"testing"
)

// TestParseJWT_MissingHeader tests JWT parsing with missing header
func TestParseJWT_MissingHeader(t *testing.T) {
	_, err := ParseJWT("")
	if err == nil {
		t.Error("expected error for missing header")
	}
}

// TestParseJWT_InvalidFormat tests JWT parsing with invalid format
func TestParseJWT_InvalidFormat(t *testing.T) {
	_, err := ParseJWT("InvalidHeader abc")
	if err == nil {
		t.Error("expected error for invalid header format")
	}
}

// TestParseJWT_MissingClaims tests JWT with missing required claims
func TestParseJWT_MissingTenantID(t *testing.T) {
	// Create a valid JWT token without tenant_id
	header := "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoidXNlci0xIiwicm9sZXMiOlsidmlld2VyIl19.test"
	_, err := ParseJWT(header)
	if err == nil {
		t.Error("expected error for missing tenant_id")
	}
}

// TestTenantIDFromCtx tests tenant ID extraction
func TestTenantIDFromCtx(t *testing.T) {
	ctx := context.Background()
	ctx = WithTenantID(ctx, "tenant-123")

	tenantID, ok := TenantIDFromCtx(ctx)
	if !ok || tenantID != "tenant-123" {
		t.Errorf("expected tenant-123, got %s (ok=%v)", tenantID, ok)
	}
}

// TestTenantIDFromCtx_Missing tests tenant ID extraction when missing
func TestTenantIDFromCtx_Missing(t *testing.T) {
	ctx := context.Background()
	_, ok := TenantIDFromCtx(ctx)
	if ok {
		t.Error("expected ok=false for missing tenant_id")
	}
}

// TestUserIDFromCtx tests user ID extraction
func TestUserIDFromCtx(t *testing.T) {
	ctx := context.Background()
	ctx = WithUserID(ctx, "user-456")

	userID, ok := UserIDFromCtx(ctx)
	if !ok || userID != "user-456" {
		t.Errorf("expected user-456, got %s (ok=%v)", userID, ok)
	}
}

// TestRolesFromCtx tests roles extraction
func TestRolesFromCtx(t *testing.T) {
	ctx := context.Background()
	roles := []string{"viewer", "editor"}
	ctx = WithRoles(ctx, roles)

	extractedRoles, ok := RolesFromCtx(ctx)
	if !ok || len(extractedRoles) != 2 {
		t.Errorf("expected 2 roles, got %d (ok=%v)", len(extractedRoles), ok)
	}
}

// TestMustTenantID tests must tenant ID extraction
func TestMustTenantID(t *testing.T) {
	ctx := context.Background()
	ctx = WithTenantID(ctx, "tenant-789")

	tenantID := MustTenantID(ctx)
	if tenantID != "tenant-789" {
		t.Errorf("expected tenant-789, got %s", tenantID)
	}
}

// TestMustTenantID_Panic tests must tenant ID extraction panics when missing
func TestMustTenantID_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for missing tenant_id")
		}
	}()

	ctx := context.Background()
	_ = MustTenantID(ctx)
}

// TestActorIDFromCtx tests actor ID extraction
func TestActorIDFromCtx(t *testing.T) {
	ctx := context.Background()
	ctx = WithActorID(ctx, "actor-111")

	actorID, ok := ActorIDFromCtx(ctx)
	if !ok || actorID != "actor-111" {
		t.Errorf("expected actor-111, got %s (ok=%v)", actorID, ok)
	}
}

// TestHasPermission_ViewerRead tests viewer can read
func TestHasPermission_ViewerRead(t *testing.T) {
	if !HasPermission(RoleViewer, "thing_model:read") {
		t.Error("viewer should have thing_model:read")
	}
}

// TestHasPermission_ViewerNoWrite tests viewer cannot write
func TestHasPermission_ViewerNoWrite(t *testing.T) {
	if HasPermission(RoleViewer, "thing_model:write") {
		t.Error("viewer should not have thing_model:write")
	}
}

// TestHasPermission_EditorWrite tests editor can write
func TestHasPermission_EditorWrite(t *testing.T) {
	if !HasPermission(RoleEditor, "thing_model:write") {
		t.Error("editor should have thing_model:write")
	}
}

// TestHasPermission_EditorNoPublish tests editor cannot publish
func TestHasPermission_EditorNoPublish(t *testing.T) {
	if HasPermission(RoleEditor, "thing_model:publish") {
		t.Error("editor should not have thing_model:publish")
	}
}

// TestHasPermission_PublisherPublish tests publisher can publish
func TestHasPermission_PublisherPublish(t *testing.T) {
	if !HasPermission(RolePublisher, "thing_model:publish") {
		t.Error("publisher should have thing_model:publish")
	}
}

// TestHasPermission_AdminAll tests admin has all permissions
func TestHasPermission_AdminAll(t *testing.T) {
	if !HasPermission(RoleAdmin, "anything:whatsoever") {
		t.Error("admin should have all permissions")
	}
}

// TestHasAnyPermission_SingleRole tests permission check with single role
func TestHasAnyPermission_SingleRole(t *testing.T) {
	roles := []string{"editor"}
	if !HasAnyPermission(roles, "thing_model:write") {
		t.Error("editor should have thing_model:write")
	}
}

// TestHasAnyPermission_MultipleRoles tests permission check with multiple roles
func TestHasAnyPermission_MultipleRoles(t *testing.T) {
	roles := []string{"viewer", "editor"}
	if !HasAnyPermission(roles, "thing_model:write") {
		t.Error("editor (in roles) should have thing_model:write")
	}
}

// TestHasAnyPermission_NoPermission tests permission denied
func TestHasAnyPermission_NoPermission(t *testing.T) {
	roles := []string{"viewer"}
	if HasAnyPermission(roles, "thing_model:write") {
		t.Error("viewer should not have thing_model:write")
	}
}

// TestHasAnyPermission_EmptyRequired tests no permission required
func TestHasAnyPermission_EmptyRequired(t *testing.T) {
	roles := []string{"viewer"}
	if !HasAnyPermission(roles, "") {
		t.Error("empty permission should always be allowed")
	}
}

// TestIsValidRole tests role validation
func TestIsValidRole(t *testing.T) {
	tests := []struct {
		role  string
		valid bool
	}{
		{"viewer", true},
		{"editor", true},
		{"publisher", true},
		{"admin", true},
		{"invalid", false},
		{"", false},
	}

	for _, test := range tests {
		if IsValidRole(test.role) != test.valid {
			t.Errorf("IsValidRole(%s) = %v, expected %v", test.role, !test.valid, test.valid)
		}
	}
}

// TestParsePermission tests permission parsing
func TestParsePermission(t *testing.T) {
	tests := []struct {
		input    string
		resource string
		action   string
	}{
		{"thing_model:read", "thing_model", "read"},
		{"device:write", "device", "write"},
		{"thing_model", "thing_model", ""},
	}

	for _, test := range tests {
		perm := ParsePermission(test.input)
		if perm.Resource != test.resource || perm.Action != test.action {
			t.Errorf("ParsePermission(%s) = {%s, %s}, expected {%s, %s}",
				test.input, perm.Resource, perm.Action, test.resource, test.action)
		}
	}
}

// TestIsMutatingRequest tests mutating request detection
func TestIsMutatingRequest(t *testing.T) {
	tests := []struct {
		method    string
		isMutating bool
	}{
		{"POST", true},
		{"PUT", true},
		{"PATCH", true},
		{"DELETE", true},
		{"GET", false},
		{"HEAD", false},
	}

	for _, test := range tests {
		if IsMutatingRequest(test.method) != test.isMutating {
			t.Errorf("IsMutatingRequest(%s) = %v, expected %v",
				test.method, !test.isMutating, test.isMutating)
		}
	}
}

// TestDeriveAction tests action derivation
func TestDeriveAction(t *testing.T) {
	tests := []struct {
		method   string
		expected AuditAction
	}{
		{"POST", ActionCreated},
		{"PUT", ActionUpdated},
		{"PATCH", ActionUpdated},
		{"DELETE", ActionDeleted},
		{"GET", ""},
	}

	for _, test := range tests {
		action := DeriveAction(test.method, "/api/test")
		if action != test.expected {
			t.Errorf("DeriveAction(%s) = %v, expected %v", test.method, action, test.expected)
		}
	}
}

// TestDeriveTargetType tests target type derivation
func TestDeriveTargetType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/thing-models/123", "THING_MODEL"},
		{"/api/devices/456", "DEVICE"},
		{"/api/test-resources/789", "TEST_RESOURCE"},
	}

	for _, test := range tests {
		targetType := DeriveTargetType(test.path)
		if targetType != test.expected {
			t.Errorf("DeriveTargetType(%s) = %s, expected %s", test.path, targetType, test.expected)
		}
	}
}

// TestSanitizeDetails tests sensitive data removal
func TestSanitizeDetails(t *testing.T) {
	details := map[string]interface{}{
		"name":       "test",
		"password":   "secret123",
		"api_key":    "key123",
		"description": "public",
	}

	sanitized := SanitizeDetails(details)

	if _, ok := sanitized["password"]; ok {
		t.Error("password should be removed")
	}
	if _, ok := sanitized["api_key"]; ok {
		t.Error("api_key should be removed")
	}
	if _, ok := sanitized["name"]; !ok {
		t.Error("name should be preserved")
	}
	if _, ok := sanitized["description"]; !ok {
		t.Error("description should be preserved")
	}
}

// TestParseAcceptLanguage_Empty tests empty Accept-Language
func TestParseAcceptLanguage_Empty(t *testing.T) {
	result := ParseAcceptLanguage("")
	if result != "zh-CN" {
		t.Errorf("expected default zh-CN, got %s", result)
	}
}

// TestParseAcceptLanguage_Single tests single language
func TestParseAcceptLanguage_Single(t *testing.T) {
	result := ParseAcceptLanguage("en-US")
	if result != "en-US" {
		t.Errorf("expected en-US, got %s", result)
	}
}

// TestParseAcceptLanguage_WithQuality tests language with quality factor
func TestParseAcceptLanguage_WithQuality(t *testing.T) {
	result := ParseAcceptLanguage("en-US;q=0.9,zh-CN;q=0.8")
	if result != "en-US" {
		t.Errorf("expected en-US, got %s", result)
	}
}

// TestParseAcceptLanguage_Multiple tests multiple languages
func TestParseAcceptLanguage_Multiple(t *testing.T) {
	result := ParseAcceptLanguage("zh-CN,en;q=0.9,fr;q=0.8")
	if result != "zh-CN" {
		t.Errorf("expected zh-CN, got %s", result)
	}
}

// TestParseAcceptLanguage_Invalid tests invalid Accept-Language
func TestParseAcceptLanguage_Invalid(t *testing.T) {
	result := ParseAcceptLanguage("!!!invalid!!!")
	if result != "zh-CN" {
		t.Errorf("expected default zh-CN for invalid, got %s", result)
	}
}

// TestNormalizeLanguageCode tests language code normalization
func TestNormalizeLanguageCode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"en", "en"},
		{"EN", "en"},
		{"en-US", "en-US"},
		{"EN-us", "en-US"},
		{"zh-CN", "zh-CN"},
		{"zh-cn", "zh-CN"},
	}

	for _, test := range tests {
		result := NormalizeLanguageCode(test.input)
		if result != test.expected {
			t.Errorf("NormalizeLanguageCode(%s) = %s, expected %s", test.input, result, test.expected)
		}
	}
}

// TestLocaleFromCtx tests locale extraction with fallback
func TestLocaleFromCtx(t *testing.T) {
	ctx := context.Background()
	ctx = WithLocale(ctx, "en-US")

	locale := LocaleFromCtx(ctx)
	if locale != "en-US" {
		t.Errorf("expected en-US, got %s", locale)
	}
}

// TestLocaleFromCtx_Default tests locale extraction with default fallback
func TestLocaleFromCtx_Default(t *testing.T) {
	ctx := context.Background()
	locale := LocaleFromCtx(ctx)
	if locale != "zh-CN" {
		t.Errorf("expected default zh-CN, got %s", locale)
	}
}

// TestContextHelpers tests context helpers interface
func TestContextHelpers(t *testing.T) {
	helpers := NewContextHelpers()

	ctx := context.Background()
	ctx = WithTenantID(ctx, "tenant-123")
	ctx = WithUserID(ctx, "user-456")
	ctx = WithRoles(ctx, []string{"editor"})
	ctx = WithLocale(ctx, "en-US")

	tenantID, _ := helpers.TenantID(ctx)
	if tenantID != "tenant-123" {
		t.Errorf("expected tenant-123, got %s", tenantID)
	}

	userID, _ := helpers.UserID(ctx)
	if userID != "user-456" {
		t.Errorf("expected user-456, got %s", userID)
	}

	roles, _ := helpers.Roles(ctx)
	if len(roles) != 1 || roles[0] != "editor" {
		t.Errorf("expected [editor], got %v", roles)
	}

	locale := helpers.Locale(ctx)
	if locale != "en-US" {
		t.Errorf("expected en-US, got %s", locale)
	}
}

// TestFromContext tests FromContext helper
func TestFromContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithTenantID(ctx, "tenant-123")
	ctx = WithUserID(ctx, "user-456")
	ctx = WithRoles(ctx, []string{"editor", "publisher"})
	ctx = WithActorID(ctx, "actor-789")
	ctx = WithLocale(ctx, "en-US")
	ctx = WithRequestID(ctx, "req-111")

	cv := FromContext(ctx)

	if cv.TenantID != "tenant-123" {
		t.Errorf("expected tenant-123, got %s", cv.TenantID)
	}
	if cv.UserID != "user-456" {
		t.Errorf("expected user-456, got %s", cv.UserID)
	}
	if len(cv.Roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(cv.Roles))
	}
	if cv.ActorID != "actor-789" {
		t.Errorf("expected actor-789, got %s", cv.ActorID)
	}
	if cv.Locale != "en-US" {
		t.Errorf("expected en-US, got %s", cv.Locale)
	}
	if cv.RequestID != "req-111" {
		t.Errorf("expected req-111, got %s", cv.RequestID)
	}
}

// TestNewAuditLogEntry tests audit log entry creation
func TestNewAuditLogEntry(t *testing.T) {
	entry := NewAuditLogEntry(
		"tenant-123",
		"user-456",
		"CREATED",
		"THING_MODEL",
		"model-789",
		"req-111",
		map[string]interface{}{
			"name":     "Test Model",
			"password": "secret",
		},
	)

	if entry.TenantID != "tenant-123" {
		t.Errorf("expected tenant-123, got %s", entry.TenantID)
	}
	if entry.ActorID != "user-456" {
		t.Errorf("expected user-456, got %s", entry.ActorID)
	}
	if entry.Action != "CREATED" {
		t.Errorf("expected CREATED, got %s", entry.Action)
	}
	if _, ok := entry.Details["password"]; ok {
		t.Error("password should be sanitized")
	}
	if _, ok := entry.Details["name"]; !ok {
		t.Error("name should be preserved")
	}
}

// TestAuditLoggerAsync tests async audit logger
func TestAuditLoggerAsync(t *testing.T) {
	logger := NewAuditLogger(10)
	defer logger.Close()

	entry := NewAuditLogEntry(
		"tenant-123",
		"user-456",
		"CREATED",
		"THING_MODEL",
		"model-789",
		"req-111",
		map[string]interface{}{},
	)

	// This should not block
	logger.Log(entry)
	logger.Log(nil) // Should handle nil gracefully
}
