package authcore

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExternalJWTBoundary(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var issuer string
	mux := http.NewServeMux()
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test-key", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	issuer = server.URL
	cfg := ProviderConfig{ID: "mini", Label: "WeChat", Kind: "oidc", Issuer: issuer, ClientID: "mini-client", TokenURL: issuer + "/token", JWKSURL: issuer + "/keys", AuthorizeURL: issuer + "/authorize", RedirectURL: "https://platform.example/auth-callback", ClientSecret: "fixture", ReviewID: "isolated-fixture", TenantIDs: []string{"tenant-1"}}
	p, e := NewProvider(cfg)
	if e != nil {
		t.Fatal(e)
	}
	p.HTTP = server.Client()
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), p.HTTP), cfg.JWKSURL)
	p.Verifier = oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: cfg.ClientID, SupportedSigningAlgs: []string{"RS256"}})
	sign := func(change func(jwt.MapClaims)) string {
		claims := jwt.MapClaims{"iss": issuer, "sub": "stable-subject", "aud": "mini-client", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "nonce": "expected"}
		if change != nil {
			change(claims)
		}
		jwtToken := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		jwtToken.Header["kid"] = "test-key"
		raw, e := jwtToken.SignedString(key)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	if _, e = p.verify(context.Background(), sign(nil), "expected"); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		change func(jwt.MapClaims)
	}{
		{"issuer", func(c jwt.MapClaims) { c["iss"] = "https://attacker" }}, {"audience", func(c jwt.MapClaims) { c["aud"] = "other-client" }},
		{"authorized-party", func(c jwt.MapClaims) { c["azp"] = "other-client" }},
		{"multiple-audiences-without-party", func(c jwt.MapClaims) { c["aud"] = []string{"mini-client", "other-client"} }},
		{"missing-expiry", func(c jwt.MapClaims) { delete(c, "exp") }},
		{"expiry", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }}, {"subject", func(c jwt.MapClaims) { c["sub"] = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e = p.verify(context.Background(), sign(tc.change), ""); e == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
	if _, e = p.verify(context.Background(), sign(nil), "different"); e == nil {
		t.Fatal("wrong nonce accepted")
	}
	parts := strings.Split(sign(nil), ".")
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	signature[0] ^= 1
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)
	if _, e = p.verify(context.Background(), strings.Join(parts, "."), "expected"); e == nil {
		t.Fatal("tampered signature accepted")
	}
	if p.Allows(User{TenantID: "other", Authority: "TENANT_ADMIN"}) || p.Allows(User{TenantID: "tenant-1", Authority: "SYS_ADMIN"}) {
		t.Fatal("source allowlist bypass")
	}
}

func TestWebExchangeUsesPKCEAndNonce(t *testing.T) {
	// A token endpoint refusing the PKCE verifier must not produce platform identity.
	seen := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		seen = r.Form.Get("code_verifier") == "verifier" && r.Form.Get("code") == "code"
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()
	p := &Provider{HTTP: server.Client(), OAuth: oauth2.Config{ClientID: "client", Endpoint: oauth2.Endpoint{TokenURL: server.URL, AuthStyle: oauth2.AuthStyleInParams}}}
	if _, e := p.Exchange(context.Background(), "code", "verifier", "nonce"); e == nil || !seen {
		t.Fatal("PKCE failure accepted or verifier not sent")
	}
}

type miniTransport func(*http.Request) (*http.Response, error)

func (f miniTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDirectWeChatWithoutCasdoor(t *testing.T) {
	p, e := NewProvider(ProviderConfig{ID: "wechat", Label: "WeChat", Kind: "wechat-mini", AppID: "wx-fixture", AppSecret: "fixture-secret", TenantIDs: []string{"tenant-1"}, ReviewID: "test"})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	payload := `{"openid":"open-1","session_key":"never-returned","unionid":"ignored"}`
	p.HTTP.Transport = miniTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if r.Method != "GET" || r.URL.Host != "api.weixin.qq.com" || r.URL.Path != "/sns/jscode2session" || q.Get("appid") != "wx-fixture" || q.Get("secret") != "fixture-secret" || q.Get("grant_type") != "authorization_code" || q.Get("js_code") == "" {
			t.Fatal("wrong official request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
	})
	s, _, _ := fixture(t)
	id, e := p.Mini(context.Background(), s, "fresh-code-1")
	if e != nil || id.Subject != "open-1" || id.ClientID != "wx-fixture" || id.Issuer != "https://api.weixin.qq.com/mini/wx-fixture" {
		t.Fatal("direct identity", id, e)
	}
	if _, e = p.Mini(context.Background(), s, "fresh-code-1"); e == nil || calls != 1 {
		t.Fatal("replay sent upstream")
	}
	for i, tc := range []struct {
		body string
		want error
	}{
		{`{"errcode":40029,"errmsg":"invalid"}`, ErrUnauthorized},
		{`{"errcode":45011}`, ErrRateLimit}, {`{"errcode":-1}`, ErrUnavailable},
		{`{"openid":"open-1"}`, ErrUnauthorized}, {`{"unionid":"ignored","session_key":"key"}`, ErrUnauthorized},
	} {
		payload = tc.body
		if _, e = p.Mini(context.Background(), s, "next-code-"+string(rune('a'+i))); !errors.Is(e, tc.want) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	other, e := NewProvider(ProviderConfig{ID: "other", Label: "Other", Kind: "wechat-mini", AppID: "wx-other", AppSecret: "fixture", TenantIDs: []string{"tenant-1"}, ReviewID: "test"})
	if e != nil || other.Config.Issuer == p.Config.Issuer {
		t.Fatal("OpenID app isolation")
	}
}
