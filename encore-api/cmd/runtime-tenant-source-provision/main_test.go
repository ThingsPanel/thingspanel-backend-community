package main

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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/l3-core/notification"
)

func signedFixtureSession(t *testing.T, key string, delegated bool) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, e := json.Marshal(map[string]any{"id": "isolated-tenant-admin", "tenant_id": "1234abcd", "authority": "TENANT_ADMIN", "delegated": delegated, "exp": time.Now().Add(time.Minute).Unix()})
	if e != nil {
		t.Fatal(e)
	}
	input := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	signature := hmac.New(sha256.New, []byte(key))
	signature.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
}
func TestLiveVerifierRequiresSignedSessionAndActualMatchingSource(t *testing.T) {
	const fixtureKey = "isolated-runtime-source-auth-test-key"
	var mismatch atomic.Bool
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/user/detail":
			json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"id": "isolated-tenant-admin", "tenant_id": "1234abcd", "authority": "TENANT_ADMIN", "status": "N"}})
		case "/api/v1/notification/session-context":
			tenant := "1234abcd"
			if mismatch.Load() {
				tenant = "another-source-tenant"
			}
			json.NewEncoder(w).Encode(map[string]any{"userId": "isolated-tenant-admin", "tenantId": tenant, "authority": "TENANT_ADMIN"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer source.Close()
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	t.Setenv("TP_JWT_KEY", fixtureKey)
	t.Setenv("TP_COMMUNITY_SESSION_BASE_URL", source.URL)
	sessions, e := notification.NewLegacySessionChecker(source.URL, notification.LegacySessionOptions{TestMode: true, AllowHTTPTestEndpoint: true, Timeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	verifier := liveVerifier{origin: source.URL, sessions: sessions}
	token := signedFixtureSession(t, fixtureKey, false)
	principal, e := verifier.Verify(context.Background(), token)
	if e != nil || principal.RawTenantID != "1234abcd" {
		t.Fatal("valid isolated signed source session refused")
	}
	mismatch.Store(true)
	if _, e = verifier.Verify(context.Background(), token); e == nil {
		t.Fatal("same signed token was accepted from a different actual source tenant")
	}
	mismatch.Store(false)
	if _, e = verifier.Verify(context.Background(), signedFixtureSession(t, fixtureKey, true)); e == nil {
		t.Fatal("delegated session provisioned source authority")
	}
	if _, e = verifier.Verify(context.Background(), signedFixtureSession(t, "wrong-isolated-test-key", false)); e == nil {
		t.Fatal("unverified claims were accepted")
	}
	t.Setenv("TP_AUTH_MODE", "dev")
	if _, e = verifier.Verify(context.Background(), token); e == nil {
		t.Fatal("development identity mode provisioned a source")
	}
}
func TestOperatorJSONRejectsDuplicateAndCaseAliasedReviewFields(t *testing.T) {
	for _, raw := range []string{`{"nativeTenantId":"one","nativeTenantId":"two"}`, `{"scope":{"environment":"a","Environment":"b"}}`, `{} {}`} {
		if rejectAmbiguousKeys([]byte(raw)) == nil {
			t.Fatal("ambiguous reviewed manifest accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "private-auth.json")
	if e := os.WriteFile(path, []byte(`{"adminToken":"isolated-admin","tenantToken":"isolated-tenant"}`), 0600); e != nil {
		t.Fatal(e)
	}
	var input authFile
	if _, e := readPrivateJSON(path, &input); e != nil {
		t.Fatal("private isolated auth fixture refused")
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := readPrivateJSON(path, &input); e == nil {
		t.Fatal("world-readable operator session file accepted")
	}
	if rejectAmbiguousKeys([]byte(strings.Repeat("[", 40)+"0"+strings.Repeat("]", 40))) == nil {
		t.Fatal("unbounded manifest nesting accepted")
	}
}
