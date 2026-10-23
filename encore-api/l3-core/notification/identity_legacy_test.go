package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func fixtureJWT(key string, claims string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte("{\"alg\":\"HS256\",\"typ\":\"JWT\"}"))
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	message := header + "." + payload
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(message))
	return message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyLegacyUserTokenRequiresSignatureExpiryAndSupportedRole(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	key := "fixture-jwt-key"
	valid := fixtureJWT(key, "{\"id\":\"user-1\",\"tenant_id\":\"tenant-1\",\"authority\":\"TENANT_ADMIN\",\"exp\":2000000100}")
	principal, err := verifyLegacyUserToken(valid, key, now)
	if err != nil || principal.UserID != "user-1" || principal.TenantID != "tenant-1" || len(principal.Roles) != 1 || principal.Roles[0] != "TENANT_ADMIN" {
		t.Fatalf("valid JWT principal=(%+v,%v)", principal, err)
	}
	for name, token := range map[string]string{
		"missing exp":      fixtureJWT(key, "{\"id\":\"user-1\",\"tenant_id\":\"tenant-1\",\"authority\":\"TENANT_ADMIN\"}"),
		"expired":          fixtureJWT(key, "{\"id\":\"user-1\",\"tenant_id\":\"tenant-1\",\"authority\":\"TENANT_ADMIN\",\"exp\":1999999999}"),
		"bad signature":    valid[:len(valid)-1] + "x",
		"duplicate claim":  fixtureJWT(key, "{\"id\":\"user-1\",\"id\":\"other\",\"tenant_id\":\"tenant-1\",\"authority\":\"TENANT_ADMIN\",\"exp\":2000000100}"),
		"unsupported role": fixtureJWT(key, "{\"id\":\"user-1\",\"tenant_id\":\"tenant-1\",\"authority\":\"platform_admin\",\"exp\":2000000100}"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifyLegacyUserToken(token, key, now); err == nil {
				t.Fatal("invalid user token accepted")
			}
		})
	}
	if _, err := verifyLegacyUserToken(valid, "", now); err == nil {
		t.Fatal("missing signing key accepted")
	}
}

func TestPlatformAdminMayAuthenticateWithoutTenantButCannotUseTenantResources(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	key := "fixture-jwt-key"
	token := fixtureJWT(key, `{"id":"platform-user-1","tenant_id":"","authority":"SYS_ADMIN","exp":2000000100}`)
	principal, err := verifyLegacyUserToken(token, key, now)
	if err != nil || principal.UserID != "platform-user-1" || principal.TenantID != "" || !containsRole(principal.Roles, "SYS_ADMIN") {
		t.Fatalf("empty-tenant platform principal=(%+v,%v)", principal, err)
	}
	if err := requireUserCapability(principal, "plugin_manage"); err != nil {
		t.Fatalf("platform plugin management should be allowed: %v", err)
	}
	if err := requireUserCapability(principal, "platform_read"); err != nil {
		t.Fatalf("global plugin read should be allowed: %v", err)
	}
	if err := requireUserCapability(principal, "manage"); err != ErrTenantContextRequired {
		t.Fatalf("tenant management without a selected tenant error=%v", err)
	}
	if err := requireNotificationScope(principal, "notification:read"); err == nil {
		t.Fatal("tenant group/read scope was accepted without tenant context")
	} else if core, ok := err.(*CoreFailure); !ok || core.Status != http.StatusForbidden || core.Reason != "tenant_context_required" {
		t.Fatalf("tenant scope failure=%v", err)
	}
	if got := platformActorScope(principal); got != "platform-user:platform-user-1" {
		t.Fatalf("platform idempotency namespace=%q", got)
	}

	if _, err := verifyLegacyUserToken(fixtureJWT(key, `{"id":"tenant-user-1","tenant_id":"","authority":"TENANT_ADMIN","exp":2000000100}`), key, now); err == nil {
		t.Fatal("tenant administrator without tenant was accepted")
	}
}

type platformSessionFixture struct{}

func (platformSessionFixture) CheckUserSession(_ context.Context, _ string, expected UserPrincipal) error {
	if expected.TenantID != "" || !containsRole(expected.Roles, "SYS_ADMIN") {
		return ErrLegacySessionRejected
	}
	return nil
}

func TestTenantlessPlatformAdminGetsExplicitTenantContextForbiddenFromTenantAPI(t *testing.T) {
	const key = "fixture-jwt-key"
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	t.Setenv("TP_JWT_KEY", key)
	infraPath := filepath.Join(t.TempDir(), "infra.json")
	infra := []byte(`{}`)
	if err := os.WriteFile(infraPath, infra, 0600); err != nil {
		t.Fatal("write guard fixture")
	}
	infraHash := sha256.Sum256(infra)
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00")
	privacy := &PrivacyGuard{infraPath: infraPath, infraHash: infraHash, verified: true, readEnv: func() ([]byte, error) { return env, nil }, linux: true}
	service := &ManagementService{sessions: platformSessionFixture{}, privacy: privacy, clock: time.Now}
	token := fixtureJWT(key, `{"id":"platform-user-1","tenant_id":"","authority":"SYS_ADMIN","exp":2000000100}`)
	request := httptest.NewRequest("GET", "/api/v2/notification-instances", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	NewManagementHTTPHandler(service).ServeHTTP(response, request)
	var body contract.DomainError
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode HTTP error: %v", err)
	}
	if response.Code != http.StatusForbidden || body.Details.Reason != "tenant_context_required" {
		t.Fatalf("tenantless platform tenant API response=%d reason=%q", response.Code, body.Details.Reason)
	}
}

func TestLegacySessionCheckerRequiresMatchingRedisBackedIdentity(t *testing.T) {
	var requestToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/notification/session-context" {
			t.Errorf("wrong session endpoint path %q", r.URL.Path)
		}
		requestToken = r.Header.Get("x-token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"userId\":\"user-1\",\"tenantId\":\"tenant-1\",\"authority\":\"TENANT_ADMIN\"}"))
	}))
	defer server.Close()
	checker, err := NewLegacySessionChecker(server.URL, LegacySessionOptions{TestMode: true, AllowHTTPTestEndpoint: true})
	if err != nil {
		t.Fatal(err)
	}
	expected := UserPrincipal{UserID: "user-1", TenantID: "tenant-1", Roles: []string{"TENANT_ADMIN"}}
	if err := checker.CheckUserSession(context.Background(), "fixture-original-jwt", expected); err != nil {
		t.Fatalf("matching session rejected: %v", err)
	}
	if requestToken != "fixture-original-jwt" {
		t.Fatal("original token was not sent in x-token")
	}
	expected.TenantID = "tenant-other"
	if err := checker.CheckUserSession(context.Background(), "fixture-original-jwt", expected); err == nil {
		t.Fatal("mismatched tenant accepted")
	}
}

func TestLegacySessionCheckerAcceptsOnlyMatchingTenantlessPlatformIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"userId":"platform-user-1","tenantId":"","authority":"SYS_ADMIN"}`))
	}))
	defer server.Close()
	checker, err := NewLegacySessionChecker(server.URL, LegacySessionOptions{TestMode: true, AllowHTTPTestEndpoint: true})
	if err != nil {
		t.Fatal(err)
	}
	principal := UserPrincipal{UserID: "platform-user-1", Roles: []string{"SYS_ADMIN"}}
	if err := checker.CheckUserSession(context.Background(), "fixture-platform-jwt", principal); err != nil {
		t.Fatalf("tenantless platform session rejected: %v", err)
	}
	principal.Roles = []string{"TENANT_ADMIN"}
	if err := checker.CheckUserSession(context.Background(), "fixture-platform-jwt", principal); err == nil {
		t.Fatal("tenantless tenant role was accepted")
	}
}
