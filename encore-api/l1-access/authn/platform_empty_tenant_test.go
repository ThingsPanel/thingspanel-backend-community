package authn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func platformLegacyToken(t *testing.T, key, authority, tenant string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{"id": "platform-subject", "tenant_id": tenant, "authority": authority, "exp": time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestLegacyPlatformEmptyTenantStillRequiresCurrentSession(t *testing.T) {
	key := strings.Repeat("isolated-platform-jwt-key-", 2)
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	t.Setenv("TP_JWT_KEY", key)
	for _, test := range []struct {
		name, body string
		status     int
		allowed    bool
	}{
		{"explicit empty", `{"code":200,"data":{"id":"platform-subject","tenant_id":"","authority":"SYS_ADMIN","status":"N"}}`, 200, true},
		{"explicit null", `{"code":200,"data":{"id":"platform-subject","tenant_id":null,"authority":"SYS_ADMIN","status":"N"}}`, 200, true},
		{"missing tenant", `{"code":200,"data":{"id":"platform-subject","authority":"SYS_ADMIN","status":"N"}}`, 200, false},
		{"tenant changed", `{"code":200,"data":{"id":"platform-subject","tenant_id":"foreign","authority":"SYS_ADMIN","status":"N"}}`, 200, false},
		{"whitespace tenant", `{"code":200,"data":{"id":"platform-subject","tenant_id":" ","authority":"SYS_ADMIN","status":"N"}}`, 200, false},
		{"authority revoked", `{"code":200,"data":{"id":"platform-subject","tenant_id":"","authority":"TENANT_ADMIN","status":"N"}}`, 200, false},
		{"subject changed", `{"code":200,"data":{"id":"other","tenant_id":"","authority":"SYS_ADMIN","status":"N"}}`, 200, false},
		{"disabled", `{"code":200,"data":{"id":"platform-subject","tenant_id":"","authority":"SYS_ADMIN","status":"F"}}`, 200, false},
		{"session revoked", `{"code":200005}`, 401, false},
		{"owner unavailable", `{"code":100001}`, 503, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			token := platformLegacyToken(t, key, "SYS_ADMIN", "")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != communitySessionPath || r.Header.Get("x-token") != token {
					t.Error("current session request changed")
				}
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			t.Setenv("TP_COMMUNITY_SESSION_BASE_URL", server.URL)
			uid, data, err := AuthHandler(context.Background(), &AuthParams{LegacyToken: token})
			if test.allowed {
				if err != nil || string(uid) != "platform-subject" || data == nil || data.TenantID != "" || len(data.Roles) != 1 || data.Roles[0] != "SYS_ADMIN" {
					t.Fatalf("existing platform session rejected: %#v %v", data, err)
				}
			} else if err == nil || data != nil || uid != "" {
				t.Fatalf("unconfirmed platform session accepted: %#v %v", data, err)
			}
			if calls.Load() != 1 {
				t.Fatal("platform exception bypassed or retried current session check")
			}
			if _, _, err := AuthHandler(context.Background(), &AuthParams{Authorization: "Bearer " + token, LegacyToken: "different-token"}); err == nil || calls.Load() != 1 {
				t.Fatal("platform exception accepted conflicting credentials")
			}
			wrongSignature := platformLegacyToken(t, key+"-wrong", "SYS_ADMIN", "")
			if _, _, err := AuthHandler(context.Background(), &AuthParams{LegacyToken: wrongSignature}); err == nil || calls.Load() != 1 {
				t.Fatal("platform exception bypassed signature verification")
			}
		})
	}
}

func TestLegacyEmptyTenantIsNotAvailableToOrdinaryOrInventedRoles(t *testing.T) {
	key := strings.Repeat("isolated-platform-jwt-key-", 2)
	for _, authority := range []string{"TENANT_ADMIN", "TENANT_USER", "PLATFORM_ADMIN", "platform_admin", "", " SYS_ADMIN "} {
		if _, _, err := parseLegacyJwtToken(platformLegacyToken(t, key, authority, ""), key); err == nil {
			t.Fatalf("ordinary/invented authority %q gained platform identity", authority)
		}
	}
	for _, role := range []string{"TENANT_ADMIN", "TENANT_USER", "PLATFORM_ADMIN", "platform_admin", "", " SYS_ADMIN "} {
		if err := checkCommunitySession(context.Background(), "http://127.0.0.1:1", "token", &Data{UserID: "subject", Roles: []string{role}}); err == nil {
			t.Fatalf("ordinary/invented session role %q accepted without tenant", role)
		}
	}
}
