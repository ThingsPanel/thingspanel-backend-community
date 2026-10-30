package authn

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.dev"
)

const testJWTKey = "authn-isolated-test-key"

func TestParseLegacyJwtTokenAcceptsCommunityClaimsWithoutOIDCClaims(t *testing.T) {
	claims := map[string]any{
		"id": "user-1", "tenant_id": "tenant-1", "authority": "TENANT_ADMIN",
		"exp": time.Now().Add(time.Minute).Unix(),
	}
	uid, data, err := parseLegacyJwtToken(signedTestJWT(t, "HS256", claims), testJWTKey)
	if err != nil {
		t.Fatalf("parse current Community claims: %v", err)
	}
	if string(uid) != "user-1" || data.UserID != "user-1" || data.TenantID != "tenant-1" || len(data.Roles) != 1 || data.Roles[0] != "TENANT_ADMIN" {
		t.Fatalf("unexpected identity mapping: uid=%q data=%+v", uid, data)
	}
}

func TestParseLegacyJwtTokenAcceptsSysAdminIdentityWithoutTenantContext(t *testing.T) {
	claims := map[string]any{
		"id": "sys-admin-1", "tenant_id": "", "authority": "SYS_ADMIN",
		"exp": time.Now().Add(time.Minute).Unix(),
	}
	uid, data, err := parseLegacyJwtToken(signedTestJWT(t, "HS256", claims), testJWTKey)
	if err != nil {
		t.Fatalf("parse SYS_ADMIN identity without tenant context: %v", err)
	}
	if string(uid) != "sys-admin-1" || data.UserID != "sys-admin-1" || data.TenantID != "" || len(data.Roles) != 1 || data.Roles[0] != "SYS_ADMIN" {
		t.Fatalf("unexpected SYS_ADMIN identity mapping: uid=%q data=%+v", uid, data)
	}
	if len(data.Scopes) != 0 {
		t.Fatalf("JWT authority must not become an implicit resource scope: %#v", data.Scopes)
	}
}

func TestParseLegacyJwtTokenRequiresCommunityAuthorityAndTenant(t *testing.T) {
	for _, tc := range []struct {
		name      string
		authority string
		tenantID  string
	}{
		{name: "missing authority", tenantID: "tenant-1"},
		{name: "unknown authority", authority: "TENANT_USER", tenantID: "tenant-1"},
		{name: "lowercase authority", authority: "tenant_admin", tenantID: "tenant-1"},
		{name: "tenant admin without tenant", authority: "TENANT_ADMIN"},
		{name: "whitespace tenant", authority: "TENANT_ADMIN", tenantID: " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{
				"id": "user-1", "tenant_id": tc.tenantID, "authority": tc.authority,
				"exp": time.Now().Add(time.Minute).Unix(),
			}
			if _, _, err := parseLegacyJwtToken(signedTestJWT(t, "HS256", claims), testJWTKey); err == nil {
				t.Fatal("accepted unsupported authority or missing tenant context")
			}
		})
	}
}

func TestParseLegacyJwtTokenRequiresExactHS256(t *testing.T) {
	for _, alg := range []string{"hs256", "HS384", "none", ""} {
		t.Run(fmt.Sprintf("alg_%q", alg), func(t *testing.T) {
			claims := map[string]any{
				"id": "user-1", "tenant_id": "tenant-1", "exp": time.Now().Add(time.Minute).Unix(),
			}
			if _, _, err := parseLegacyJwtToken(signedTestJWT(t, alg, claims), testJWTKey); err == nil {
				t.Fatalf("accepted signed token with alg %q", alg)
			}
		})
	}
}

func TestParseLegacyJwtTokenRequiresPositiveFutureExpiration(t *testing.T) {
	cases := []struct {
		name string
		omit bool
		exp  any
	}{
		{name: "missing", omit: true},
		{name: "zero", exp: int64(0)},
		{name: "negative", exp: int64(-1)},
		{name: "expired", exp: time.Now().Add(-time.Second).Unix()},
		{name: "wrong_type", exp: "tomorrow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"id": "user-1", "tenant_id": "tenant-1"}
			if !tc.omit {
				claims["exp"] = tc.exp
			}
			if _, _, err := parseLegacyJwtToken(signedTestJWT(t, "HS256", claims), testJWTKey); err == nil {
				t.Fatal("accepted token with invalid expiration")
			}
		})
	}
}

func TestParseLegacyJwtTokenValidatesOptionalNotBefore(t *testing.T) {
	cases := []struct {
		name    string
		nbf     any
		wantErr bool
	}{
		{name: "future", nbf: time.Now().Add(time.Minute).Unix(), wantErr: true},
		{name: "current_or_past", nbf: time.Now().Add(-time.Minute).Unix()},
		{name: "negative", nbf: int64(-1), wantErr: true},
		{name: "wrong_type", nbf: "later", wantErr: true},
		{name: "null", nbf: nil, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{
				"id": "user-1", "tenant_id": "tenant-1", "authority": "TENANT_ADMIN", "exp": time.Now().Add(time.Minute).Unix(), "nbf": tc.nbf,
			}
			_, _, err := parseLegacyJwtToken(signedTestJWT(t, "HS256", claims), testJWTKey)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parse error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAuthHandlerVerifiesCommunitySession(t *testing.T) {
	const token = "signed-authhandler-fixture-token"
	claims := map[string]any{
		"id": "user-1", "tenant_id": "tenant-1", "authority": "TENANT_ADMIN",
		"exp": time.Now().Add(time.Minute).Unix(),
	}
	signedToken := signedTestJWT(t, "HS256", claims)

	tests := []struct {
		name       string
		statusCode int
		user       string
		wantErr    bool
	}{
		{name: "active identity", statusCode: http.StatusOK, user: `{"id":"user-1","tenant_id":"tenant-1","authority":"TENANT_ADMIN","status":"N"}`},
		{name: "identity mismatch", statusCode: http.StatusOK, user: `{"id":"user-1","tenant_id":"tenant-2","authority":"TENANT_ADMIN","status":"N"}`, wantErr: true},
		{name: "frozen identity", statusCode: http.StatusOK, user: `{"id":"user-1","tenant_id":"tenant-1","authority":"TENANT_ADMIN","status":"F"}`, wantErr: true},
		{name: "revoked session", statusCode: http.StatusUnauthorized, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != communitySessionPath {
					t.Errorf("unexpected session request: %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("x-token"); got != signedToken {
					t.Error("session endpoint did not receive the signed test token")
				}
				w.WriteHeader(tc.statusCode)
				if tc.user != "" {
					_, _ = fmt.Fprintf(w, `{"code":200,"data":%s}`, tc.user)
				}
			}))
			defer server.Close()

			t.Setenv("TP_AUTH_MODE", "legacy-jwt")
			t.Setenv("TP_JWT_KEY", testJWTKey)
			t.Setenv("TP_COMMUNITY_SESSION_BASE_URL", server.URL)

			var logs bytes.Buffer
			logger := log.Default()
			previousOutput := logger.Writer()
			logger.SetOutput(&logs)
			defer logger.SetOutput(previousOutput)

			uid, data, err := AuthHandler(context.Background(), &AuthParams{Authorization: "Bearer " + signedToken})
			if (err != nil) != tc.wantErr {
				t.Fatalf("AuthHandler error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && (string(uid) != "user-1" || data == nil || data.TenantID != "tenant-1" || data.Roles[0] != "TENANT_ADMIN") {
				t.Fatalf("unexpected authenticated principal: uid=%q data=%+v", uid, data)
			}
			if strings.Contains(logs.String(), signedToken) || (err != nil && strings.Contains(err.Error(), signedToken)) {
				t.Fatal("authentication failure exposed the bearer token")
			}
		})
	}
}

func TestAuthHandlerAcceptsSysAdminIdentityWithoutTenantContext(t *testing.T) {
	const userID = "sys-admin-1"
	token := signedTestJWT(t, "HS256", map[string]any{
		"id": userID, "tenant_id": "", "authority": "SYS_ADMIN",
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != communitySessionPath {
			t.Errorf("unexpected session request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-token"); got != token {
			t.Error("session endpoint did not receive the signed test token")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"code":200,"data":{"id":"sys-admin-1","tenant_id":"","authority":"SYS_ADMIN","status":"N"}}`)
	}))
	defer server.Close()

	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	t.Setenv("TP_JWT_KEY", testJWTKey)
	t.Setenv("TP_COMMUNITY_SESSION_BASE_URL", server.URL)

	uid, data, err := AuthHandler(context.Background(), &AuthParams{Authorization: "Bearer " + token})
	if err != nil {
		t.Fatalf("authenticate active SYS_ADMIN identity: %v", err)
	}
	if string(uid) != userID || data == nil || data.TenantID != "" || len(data.Roles) != 1 || data.Roles[0] != "SYS_ADMIN" || len(data.Scopes) != 0 {
		t.Fatalf("unexpected authenticated SYS_ADMIN principal: uid=%q data=%+v", uid, data)
	}
}

func TestInsecureAuthModesAreRestrictedToLocalAndTest(t *testing.T) {
	for _, mode := range []string{"dev", "passthrough"} {
		for _, envType := range []encore.EnvironmentType{encore.EnvProduction, encore.EnvDevelopment, encore.EnvEphemeral, "unknown"} {
			if err := validateAuthModeEnvironment(mode, envType, encore.CloudAWS); err == nil {
				t.Errorf("mode %q accepted in environment %q on a real cloud", mode, envType)
			}
		}
		// A production environment never counts as local, whatever cloud it reports.
		if err := validateAuthModeEnvironment(mode, encore.EnvProduction, encore.CloudLocal); err == nil {
			t.Errorf("mode %q accepted in production on the local cloud", mode)
		}
		for _, envType := range []encore.EnvironmentType{encore.EnvLocal, encore.EnvTest} {
			if err := validateAuthModeEnvironment(mode, envType, ""); err != nil {
				t.Errorf("mode %q rejected in environment %q: %v", mode, envType, err)
			}
		}
		// `encore run` reports type development on the local cloud.
		if err := validateAuthModeEnvironment(mode, encore.EnvDevelopment, encore.CloudLocal); err != nil {
			t.Errorf("mode %q rejected under `encore run`: %v", mode, err)
		}
	}
	for _, envType := range []encore.EnvironmentType{encore.EnvProduction, encore.EnvDevelopment} {
		if err := validateAuthModeEnvironment("legacy-jwt", envType, encore.CloudAWS); err != nil {
			t.Errorf("legacy-jwt rejected in environment %q: %v", envType, err)
		}
	}
}

func signedTestJWT(t *testing.T, alg string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"typ": "JWT", "alg": alg})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(testJWTKey))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
