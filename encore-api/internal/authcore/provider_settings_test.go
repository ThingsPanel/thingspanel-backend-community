package authcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestProviderConfigurationIsolationAndChannels(t *testing.T) {
	good := ProviderConfig{Enabled: true, ID: "mini", Label: "WeChat", Kind: "wechat-mini", AppID: "app", AppSecret: "not-public", ReviewID: "review", TenantIDs: []string{"tenant-1"}}
	bad := good
	bad.ID = "invalid"
	bad.AppSecret = ""
	disabled := good
	disabled.ID = "disabled"
	disabled.Enabled = false
	providers, diagnostics := configuredProviders([]ProviderConfig{good, bad, disabled}, "https://panel.example")
	if len(providers) != 1 || len(diagnostics) != 1 {
		t.Fatal("bad/disabled sources were not isolated")
	}
	s, _, _ := fixture(t)
	h := &HTTP{Runtime: &Runtime{Core: s, Providers: providers, Origin: "https://panel.example"}}
	for _, tc := range []struct {
		channel string
		enabled bool
	}{{"web", false}, {"mp-weixin", true}, {"app", false}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/auth/login-options?channel="+tc.channel, nil))
		var result struct {
			Data struct{ External struct{ Enabled bool } }
		}
		json.Unmarshal(w.Body.Bytes(), &result)
		if w.Code != 200 || result.Data.External.Enabled != tc.enabled || strings.Contains(w.Body.String(), "not-public") {
			t.Fatal("unsafe channel discovery", w.Code)
		}
	}
	duplicate := good
	duplicate.Label = "duplicate"
	providers, _ = configuredProviders([]ProviderConfig{good, duplicate}, "https://panel.example")
	if len(providers) != 0 {
		t.Fatal("duplicate source silently selected")
	}
}

func TestProviderSecretsAndNetworkBoundary(t *testing.T) {
	m := &ProviderSettings{Key: []byte(strings.Repeat("k", 32))}
	encrypted, e := m.seal("mini", "secret-value")
	if e != nil || strings.Contains(string(encrypted), "secret-value") {
		t.Fatal("secret not protected")
	}
	if value, e := m.open("mini", encrypted); e != nil || value != "secret-value" {
		t.Fatal("secret round trip")
	}
	if _, e = m.open("other", encrypted); e == nil {
		t.Fatal("secret transplanted between sources")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, e = m.open("mini", encrypted); e == nil {
		t.Fatal("tampered secret accepted")
	}
	for _, address := range []string{"127.0.0.1", "::1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.168.1.1", "fe80::1", "fc00::1"} {
		if publicProviderIP(net.ParseIP(address)) {
			t.Fatal("private provider target accepted", address)
		}
	}
	if !publicProviderIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public target rejected")
	}
}

func TestProviderStorageFailureDoesNotBreakPasswordLogin(t *testing.T) {
	s, a, _ := fixture(t)
	db, e := sql.Open("pgx", "postgres://unused:unused@localhost/unused")
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	h := &HTTP{Runtime: &Runtime{Core: s, Origin: "https://panel.example", Providers: map[string]*Provider{}, Settings: &ProviderSettings{DB: db}}}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/auth/login-options?channel=web", nil))
	if w.Code != 503 {
		t.Fatal("provider storage failure was disguised as disabled")
	}
	raw, _ := json.Marshal(map[string]string{"email": a.user.Email, "password": "correct-password"})
	r := httptest.NewRequest("POST", "/api/v1/login", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("provider storage failure broke password login", w.Code)
	}
}

func TestProviderSettingsPostgresLifecycle(t *testing.T) {
	dsn := os.Getenv("TP_AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated notification_test PostgreSQL")
	}
	c, e := pgx.ParseConfig(dsn)
	if e != nil || c.Database != "notification_test" {
		t.Fatal("test database must be notification_test")
	}
	admin := stdlib.OpenDB(*c)
	defer admin.Close()
	schema := "auth_settings_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(`CREATE SCHEMA ` + schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	c.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*c)
	defer db.Close()
	if _, e = db.Exec(`CREATE TABLE users(id varchar(36) PRIMARY KEY,tenant_id text,authority text); CREATE TABLE app_push_devices(id text); INSERT INTO users VALUES('tenant','tenant-1','TENANT_ADMIN')`); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"001_identity.sql", "002_self_unlink.sql", "003_provider_settings.sql"} {
		raw, e := os.ReadFile("../../../sql/auth/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	s, a, _ := fixture(t)
	a.user.Authority = "SYS_ADMIN"
	session, e := s.Issue(context.Background(), a.user)
	if e != nil {
		t.Fatal(e)
	}
	m := &ProviderSettings{DB: db, Key: []byte(strings.Repeat("k", 32)), Origin: "https://panel.example"}
	m.Build = func(c ProviderConfig) (*Provider, error) {
		p, e := NewProvider(c)
		if e == nil {
			p.HTTP.Transport = miniTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"openid":"test-only-subject","session_key":"must-not-leak","access_token":"test-token","expires_in":7200,"scope":"snsapi_login"}`))}, nil
			})
		}
		return p, e
	}
	h := &HTTP{Runtime: &Runtime{Core: s, Settings: m, Origin: m.Origin, Providers: map[string]*Provider{}}}
	request := func(in providerChange) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(in)
		r := httptest.NewRequest("POST", "/api/v1/auth/admin/providers", strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+session.Token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	cfg := ProviderConfig{ID: "mini", Label: "Mini", Kind: "wechat-mini", AppID: "app", AppSecret: "test-secret", TenantIDs: []string{"tenant-1"}}
	in := providerChange{Operation: "save", ID: cfg.ID, Config: cfg, Password: "wrong"}
	if w := request(in); w.Code != 401 {
		t.Fatal("settings saved without fresh password")
	}
	in.Password = "correct-password"
	if w := request(in); w.Code != 200 {
		t.Fatal("save draft", w.Code)
	}
	in.ExpectedRevision = 1
	in.Operation = "enable"
	if w := request(in); w.Code != 409 {
		t.Fatal("untested source enabled", w.Code)
	}
	in.Operation = "test"
	in.Code = "fresh-mini-code"
	if w := request(in); w.Code != 200 || strings.Contains(w.Body.String(), "test-only-subject") || strings.Contains(w.Body.String(), "must-not-leak") {
		t.Fatal("identity test failed or leaked identity", w.Code)
	}
	if a.identity != nil {
		t.Fatal("test linked an account")
	}
	in.Operation = "enable"
	if w := request(in); w.Code != 200 {
		t.Fatal("enable tested revision", w.Code)
	}
	active, e := m.Resolve(context.Background(), nil)
	if e != nil || len(active) != 1 {
		t.Fatal("published source not resolved", e)
	}
	oldReview := active["mini"].Config.ReviewID
	list, e := m.List(context.Background())
	if e != nil || len(list) != 1 || !list[0].SecretConfigured {
		t.Fatal("settings list", e)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "test-secret") {
		t.Fatal("list leaked secret")
	}
	in.Operation = "save"
	in.Config = cfg
	in.Config.AppSecret = ""
	in.Config.Label = "Updated draft"
	if w := request(in); w.Code != 200 {
		t.Fatal("edit", w.Code)
	}
	active, e = m.Resolve(context.Background(), nil)
	if e != nil || active["mini"].Config.Label != "Mini" {
		t.Fatal("draft replaced active revision")
	}
	in.Operation = "enable"
	if w := request(in); w.Code != 409 {
		t.Fatal("stale/unverified revision enabled")
	}
	in.ExpectedRevision = 2
	in.Operation = "disable"
	if w := request(in); w.Code != 200 {
		t.Fatal("disable", w.Code)
	}
	active, e = m.Resolve(context.Background(), nil)
	if e != nil || len(active) != 0 {
		t.Fatal("disabled source still advertised")
	}
	in.Operation = "test"
	in.Code = "second-fresh-code"
	if w := request(in); w.Code != 200 {
		t.Fatal("test edited revision", w.Code)
	}
	in.Operation = "enable"
	if w := request(in); w.Code != 200 {
		t.Fatal("reenable", w.Code)
	}
	active, _ = m.Resolve(context.Background(), nil)
	if active["mini"].Config.ReviewID == oldReview {
		t.Fatal("disable/enable revived an old transaction revision")
	}
	// Browser testing is also allowed while disabled, but must never bind/login.
	cfg = ProviderConfig{ID: "web", Label: "Website", Kind: "wechat-web", AppID: "web-app", AppSecret: "secret", RedirectURL: m.Origin + "/api/v1/auth/external/callback", TenantIDs: []string{"tenant-1"}}
	in = providerChange{Operation: "save", ID: "web", Config: cfg, Password: "correct-password"}
	if w := request(in); w.Code != 200 {
		t.Fatal("web draft", w.Code)
	}
	in.Operation = "test"
	in.ExpectedRevision = 1
	w := request(in)
	if w.Code != 200 {
		t.Fatal("browser test start", w.Code)
	}
	var start struct {
		Data struct {
			URL string `json:"url"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &start)
	target, _ := url.Parse(start.Data.URL)
	r := httptest.NewRequest("GET", "/api/v1/auth/external/callback?state="+target.Query().Get("state")+"&code=fresh-web-code", nil)
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.HasSuffix(w.Header().Get("Location"), "authTest=success") || a.identity != nil {
		t.Fatal("test callback failed or linked identity", w.Header().Get("Location"))
	}
	list, _ = m.List(context.Background())
	if list[1].TestedRevision != 1 || list[1].Enabled {
		t.Fatal("test implicitly enabled provider")
	}
	// A test started for a draft cannot approve a later edited revision.
	w = request(in)
	json.Unmarshal(w.Body.Bytes(), &start)
	target, _ = url.Parse(start.Data.URL)
	cookie := w.Result().Cookies()[0]
	in.Operation = "save"
	in.Config = cfg
	in.Config.Label = "Changed during test"
	if w := request(in); w.Code != 200 {
		t.Fatal("edit during test", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/v1/auth/external/callback?state="+target.Query().Get("state")+"&code=another-web-code", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.HasSuffix(w.Header().Get("Location"), "authTest=failed") {
		t.Fatal("stale test approved changed configuration")
	}
	a.user.Authority = "TENANT_ADMIN"
	other, e := s.Issue(context.Background(), a.user)
	if e != nil {
		t.Fatal(e)
	}
	session = other
	if w := request(in); w.Code != 403 {
		t.Fatal("tenant administrator changed platform provider", w.Code)
	}
	if _, e = m.open("mini", []byte("invalid")); !errors.Is(e, ErrUnavailable) {
		t.Fatal("invalid ciphertext did not fail closed")
	}
}
