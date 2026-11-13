package authcore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ProviderSettings shares the existing account database and its migration owner.
// Drafts never replace published configuration until explicitly tested/enabled.
type ProviderSettings struct {
	DB      *sql.DB
	Key     []byte
	Origin  string
	Build   func(ProviderConfig) (*Provider, error)
	cacheMu sync.Mutex
	cache   map[string]*Provider
}

type ProviderSetting struct {
	Config            ProviderConfig `json:"config"`
	Revision          int64          `json:"revision"`
	ValidatedRevision int64          `json:"validatedRevision"`
	TestedRevision    int64          `json:"testedRevision"`
	TestedAt          *time.Time     `json:"testedAt"`
	PublishedRevision int64          `json:"publishedRevision"`
	Enabled           bool           `json:"enabled"`
	SecretConfigured  bool           `json:"secretConfigured"`
}

type TenantOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (m *ProviderSettings) TenantOptions(ctx context.Context) ([]TenantOption, error) {
	rows, e := m.DB.QueryContext(ctx, `SELECT DISTINCT ON (tenant_id) tenant_id,COALESCE(NULLIF(to_jsonb(u)->>'name',''),NULLIF(to_jsonb(u)->>'email',''),tenant_id) FROM users u WHERE authority='TENANT_ADMIN' AND COALESCE(tenant_id,'')<>'' ORDER BY tenant_id,id`)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	out := []TenantOption{}
	for rows.Next() {
		var v TenantOption
		if rows.Scan(&v.ID, &v.Name) != nil {
			return nil, ErrUnavailable
		}
		out = append(out, v)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}

type providerChange struct {
	Operation        string         `json:"operation"`
	ID               string         `json:"id"`
	ExpectedRevision int64          `json:"expectedRevision"`
	Config           ProviderConfig `json:"config"`
	Password         string         `json:"password"`
	Salt             string         `json:"salt"`
	Code             string         `json:"code"`
}

var providerID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type ProviderConfigError struct{ Fields []string }

func (e *ProviderConfigError) Error() string { return "provider configuration invalid" }
func (e *ProviderConfigError) Unwrap() error { return ErrForbidden }
func providerConfigFields(c ProviderConfig, origin string) []string {
	out := []string{}
	if !providerID.MatchString(c.ID) {
		out = append(out, "id")
	}
	if strings.TrimSpace(c.Label) == "" {
		out = append(out, "label")
	}
	if len(c.TenantIDs) == 0 {
		out = append(out, "tenantIds")
	}
	if c.Kind == "oidc" {
		if c.ClientID == "" {
			out = append(out, "clientId")
		}
		if c.ClientSecret == "" {
			out = append(out, "clientSecret")
		}
		for _, v := range []struct{ name, value string }{{"issuer", c.Issuer}, {"authorizeUrl", c.AuthorizeURL}, {"tokenUrl", c.TokenURL}, {"jwksUrl", c.JWKSURL}} {
			if !validHTTPS(v.value) {
				out = append(out, v.name)
			}
		}
	} else {
		if c.AppID == "" || strings.ContainsAny(c.AppID, "/?# ") {
			out = append(out, "appId")
		}
		if c.AppSecret == "" {
			out = append(out, "appSecret")
		}
	}
	if c.Kind != "wechat-mini" && c.RedirectURL != origin+"/api/v1/auth/external/callback" {
		out = append(out, "redirectUrl")
	}
	return out
}

func (m *ProviderSettings) cipher() (cipher.AEAD, error) {
	if len(m.Key) != 32 {
		return nil, ErrUnavailable
	}
	b, e := aes.NewCipher(m.Key)
	if e != nil {
		return nil, ErrUnavailable
	}
	return cipher.NewGCM(b)
}

func (m *ProviderSettings) seal(id, value string) ([]byte, error) {
	a, e := m.cipher()
	if e != nil {
		return nil, e
	}
	n := make([]byte, a.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return nil, ErrUnavailable
	}
	return a.Seal(n, n, []byte(value), []byte("auth-provider:"+id)), nil
}

func (m *ProviderSettings) open(id string, value []byte) (string, error) {
	a, e := m.cipher()
	if e != nil || len(value) < a.NonceSize() {
		return "", ErrUnavailable
	}
	v, e := a.Open(nil, value[:a.NonceSize()], value[a.NonceSize():], []byte("auth-provider:"+id))
	if e != nil {
		return "", ErrUnavailable
	}
	return string(v), nil
}

func providerSecret(c ProviderConfig) string {
	if c.Kind == "oidc" {
		return c.ClientSecret
	}
	return c.AppSecret
}
func clearProviderSecret(c ProviderConfig) ProviderConfig {
	c.AppSecret, c.ClientSecret = "", ""
	return c
}
func setProviderSecret(c *ProviderConfig, value string) {
	if c.Kind == "oidc" {
		c.ClientSecret = value
	} else {
		c.AppSecret = value
	}
}
func providerRevision(id string, revision, generation int64) string {
	return fmt.Sprintf("managed:%s:%d:%d", id, revision, generation)
}
func (m *ProviderSettings) build(c ProviderConfig) (*Provider, error) {
	if c.Kind != "wechat-mini" && c.RedirectURL != m.Origin+"/api/v1/auth/external/callback" {
		return nil, ErrForbidden
	}
	if m.Build != nil {
		return m.Build(c)
	}
	return NewProvider(c)
}

func (m *ProviderSettings) List(ctx context.Context) ([]ProviderSetting, error) {
	rows, e := m.DB.QueryContext(ctx, `SELECT draft,revision,validated_revision,tested_revision,tested_at,published_revision,enabled,draft_secret FROM auth_provider_settings ORDER BY id`)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	out := []ProviderSetting{}
	for rows.Next() {
		var v ProviderSetting
		var raw, encrypted []byte
		if rows.Scan(&raw, &v.Revision, &v.ValidatedRevision, &v.TestedRevision, &v.TestedAt, &v.PublishedRevision, &v.Enabled, &encrypted) != nil || json.Unmarshal(raw, &v.Config) != nil {
			return nil, ErrUnavailable
		}
		v.Config = clearProviderSecret(v.Config)
		secret, e := m.open(v.Config.ID, encrypted)
		if e != nil {
			return nil, e
		}
		v.SecretConfigured = secret != ""
		out = append(out, v)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}

func (m *ProviderSettings) draft(ctx context.Context, id string) (ProviderConfig, int64, error) {
	var raw, secret []byte
	var revision int64
	var c ProviderConfig
	if m.DB.QueryRowContext(ctx, `SELECT draft,draft_secret,revision FROM auth_provider_settings WHERE id=$1`, id).Scan(&raw, &secret, &revision) != nil || json.Unmarshal(raw, &c) != nil {
		return c, 0, ErrUnavailable
	}
	v, e := m.open(id, secret)
	if e != nil {
		return c, 0, e
	}
	setProviderSecret(&c, v)
	c.ReviewID = providerRevision(id, revision, 0)
	return c, revision, nil
}

// Resolve snapshots only active published revisions. A saved bad draft cannot
// take down password login or replace an already working provider.
func (m *ProviderSettings) Resolve(ctx context.Context, base map[string]*Provider) (map[string]*Provider, error) {
	out := make(map[string]*Provider, len(base))
	for id, p := range base {
		out[id] = p
	}
	rows, e := m.DB.QueryContext(ctx, `SELECT id,enabled,published,published_secret,published_revision,generation FROM auth_provider_settings ORDER BY id`)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var enabled bool
		var raw, secret []byte
		var revision, generation int64
		if rows.Scan(&id, &enabled, &raw, &secret, &revision, &generation) != nil {
			return nil, ErrUnavailable
		}
		delete(out, id) // An explicitly disabled managed setting overrides a file entry.
		if !enabled {
			m.cacheMu.Lock()
			delete(m.cache, id)
			m.cacheMu.Unlock()
			continue
		}
		review := providerRevision(id, revision, generation)
		m.cacheMu.Lock()
		cached := m.cache[id]
		m.cacheMu.Unlock()
		if cached != nil && cached.Config.ReviewID == review {
			out[id] = cached
			continue
		}
		var c ProviderConfig
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		value, e := m.open(id, secret)
		if e != nil {
			continue
		}
		setProviderSecret(&c, value)
		c.ReviewID = review
		p, e := m.build(c)
		if e == nil {
			out[id] = p
			m.cacheMu.Lock()
			if m.cache == nil {
				m.cache = map[string]*Provider{}
			}
			m.cache[id] = p
			m.cacheMu.Unlock()
		}
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}

func (m *ProviderSettings) Save(ctx context.Context, u User, in providerChange) error {
	c := in.Config
	if !providerID.MatchString(c.ID) || in.ID != c.ID || len(c.Label) > 80 || strings.TrimSpace(c.Label) == "" || (c.Kind != "oidc" && c.Kind != "wechat-mini" && c.Kind != "wechat-web") || len(c.TenantIDs) > 100 || len(providerSecret(c)) > 4096 {
		return ErrForbidden
	}
	c.Enabled = false
	c.ReviewID = ""
	c.ClientSecret = strings.TrimSpace(c.ClientSecret)
	c.AppSecret = strings.TrimSpace(c.AppSecret)
	tx, e := m.DB.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	var raw, secret []byte
	var rev int64
	e = tx.QueryRowContext(ctx, `SELECT draft,draft_secret,revision FROM auth_provider_settings WHERE id=$1 FOR UPDATE`, c.ID).Scan(&raw, &secret, &rev)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return ErrUnavailable
	}
	if rev != in.ExpectedRevision {
		return ErrConflict
	}
	value := providerSecret(c)
	if value == "" && rev > 0 {
		var old ProviderConfig
		if json.Unmarshal(raw, &old) != nil {
			return ErrUnavailable
		}
		// A secret must be supplied again when its identity or destination changes.
		if old.Kind == c.Kind && old.Issuer == c.Issuer && old.TokenURL == c.TokenURL && old.AppID == c.AppID && old.ClientID == c.ClientID {
			value, e = m.open(c.ID, secret)
			if e != nil {
				return e
			}
		}
	}
	secret, e = m.seal(c.ID, value)
	if e != nil {
		return e
	}
	c = clearProviderSecret(c)
	raw, e = json.Marshal(c)
	if e != nil {
		return ErrForbidden
	}
	if rev == 0 {
		_, e = tx.ExecContext(ctx, `INSERT INTO auth_provider_settings(id,draft,draft_secret) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, c.ID, raw, secret)
		if e == nil {
			var stored []byte
			e = tx.QueryRowContext(ctx, `SELECT draft_secret FROM auth_provider_settings WHERE id=$1`, c.ID).Scan(&stored)
			if e == nil && string(stored) != string(secret) {
				return ErrConflict
			}
		}
	} else {
		_, e = tx.ExecContext(ctx, `UPDATE auth_provider_settings SET draft=$2,draft_secret=$3,revision=revision+1,validated_revision=0,tested_revision=0,tested_at=NULL,updated_at=now() WHERE id=$1`, c.ID, raw, secret)
	}
	if e != nil {
		return ErrUnavailable
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO auth_audit_events(actor_id,user_id,event,provider) VALUES($1,$1,'provider_draft_saved',$2)`, u.ID, c.ID); e != nil {
		return ErrUnavailable
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}

func (m *ProviderSettings) Validate(ctx context.Context, id string, revision int64) (*Provider, error) {
	c, rev, e := m.draft(ctx, id)
	if e != nil {
		return nil, e
	}
	if rev != revision {
		return nil, ErrConflict
	}
	if fields := providerConfigFields(c, m.Origin); len(fields) > 0 {
		return nil, &ProviderConfigError{Fields: fields}
	}
	for _, tenant := range c.TenantIDs {
		var found bool
		if m.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE tenant_id=$1 AND authority='TENANT_ADMIN')`, tenant).Scan(&found) != nil {
			return nil, ErrUnavailable
		}
		if !found {
			return nil, &ProviderConfigError{Fields: []string{"tenantIds"}}
		}
	}
	p, e := m.build(c)
	if e != nil {
		return nil, ErrForbidden
	}
	r, e := m.DB.ExecContext(ctx, `UPDATE auth_provider_settings SET validated_revision=revision WHERE id=$1 AND revision=$2`, id, revision)
	if e != nil {
		return nil, ErrUnavailable
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return nil, ErrConflict
	}
	return p, nil
}

func (m *ProviderSettings) markTested(ctx context.Context, u User, id string, revision int64) error {
	tx, e := m.DB.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, `UPDATE auth_provider_settings SET tested_revision=revision,tested_at=now() WHERE id=$1 AND revision=$2 AND validated_revision=revision`, id, revision)
	if e != nil {
		return ErrUnavailable
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrConflict
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO auth_audit_events(actor_id,user_id,event,provider) VALUES($1,$1,'provider_identity_tested',$2)`, u.ID, id); e != nil {
		return ErrUnavailable
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}

func (m *ProviderSettings) SetEnabled(ctx context.Context, u User, id string, revision int64, enabled bool) error {
	if enabled {
		if _, e := m.Validate(ctx, id, revision); e != nil {
			return e
		}
	}
	tx, e := m.DB.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	query := `UPDATE auth_provider_settings SET enabled=false,generation=generation+1,updated_at=now() WHERE id=$1 AND revision=$2`
	if enabled {
		query = `UPDATE auth_provider_settings SET enabled=true,published=draft,published_secret=draft_secret,published_revision=revision,generation=generation+1,updated_at=now() WHERE id=$1 AND revision=$2 AND tested_revision=revision AND validated_revision=revision`
	}
	r, e := tx.ExecContext(ctx, query, id, revision)
	if e != nil {
		return ErrUnavailable
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrConflict
	}
	event := "provider_disabled"
	if enabled {
		event = "provider_enabled"
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO auth_audit_events(actor_id,user_id,event,provider) VALUES($1,$1,$2,$3)`, u.ID, event, id); e != nil {
		return ErrUnavailable
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}

func (h *HTTP) providerAdmin(w http.ResponseWriter, r *http.Request) {
	s := h.Runtime.Core
	raw, e := Bearer(r)
	if e != nil {
		reply(w, nil, e)
		return
	}
	u, e := s.Check(r.Context(), raw)
	if e != nil {
		reply(w, nil, e)
		return
	}
	if u.Delegated || u.Authority != "SYS_ADMIN" {
		reply(w, nil, ErrForbidden)
		return
	}
	if e = s.Limit(r.Context(), "provider-settings:"+u.ID, 60); e != nil {
		reply(w, nil, e)
		return
	}
	m := h.Runtime.Settings
	if r.Method == "GET" {
		if m == nil {
			reply(w, map[string]any{"editable": false, "reason": "provider_key_not_configured", "providers": []ProviderSetting{}, "diagnostics": h.Runtime.Diagnostics, "callbackUrl": h.Runtime.Origin + "/api/v1/auth/external/callback"}, nil)
			return
		}
		list, e := m.List(r.Context())
		if e != nil {
			reply(w, nil, e)
			return
		}
		tenants, e := m.TenantOptions(r.Context())
		reply(w, map[string]any{"editable": true, "providers": list, "tenants": tenants, "diagnostics": h.Runtime.Diagnostics, "callbackUrl": m.Origin + "/api/v1/auth/external/callback"}, e)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if m == nil {
		reply(w, nil, ErrUnavailable)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		reply(w, nil, ErrForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var in providerChange
	var extra any
	if d.Decode(&in) != nil || d.Decode(&extra) != io.EOF || !providerID.MatchString(in.ID) {
		reply(w, nil, ErrForbidden)
		return
	}
	proof, e := s.VerifyPassword(r.Context(), u.Email, in.Password, in.Salt)
	if e != nil || proof.ID != u.ID || proof.Version != u.Version || proof.Authority != "SYS_ADMIN" {
		if e == nil {
			e = ErrUnauthorized
		}
		reply(w, nil, e)
		return
	}
	switch in.Operation {
	case "save":
		e = m.Save(r.Context(), u, in)
	case "validate":
		_, e = m.Validate(r.Context(), in.ID, in.ExpectedRevision)
	case "enable":
		e = m.SetEnabled(r.Context(), u, in.ID, in.ExpectedRevision, true)
	case "disable":
		e = m.SetEnabled(r.Context(), u, in.ID, in.ExpectedRevision, false)
	case "test":
		var p *Provider
		p, e = m.Validate(r.Context(), in.ID, in.ExpectedRevision)
		if e == nil {
			if p.Config.Kind == "wechat-mini" {
				_, e = p.Mini(r.Context(), s, in.Code)
				if e == nil {
					e = m.markTested(r.Context(), u, in.ID, in.ExpectedRevision)
				}
			} else {
				h.startProviderTest(w, r, p, u, raw)
				return
			}
		}
	default:
		e = ErrForbidden
	}
	reply(w, map[string]string{"status": "saved"}, e)
}

func (h *HTTP) startProviderTest(w http.ResponseWriter, r *http.Request, p *Provider, u User, raw string) {
	t := transaction{Provider: p.Config.ID, Purpose: "provider-test", Revision: p.Config.ReviewID, UserID: u.ID, Session: raw, ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}
	state, e := Random()
	if e != nil {
		reply(w, nil, e)
		return
	}
	t.Verifier, e = Random()
	if e != nil {
		reply(w, nil, e)
		return
	}
	t.Nonce, e = Random()
	if e != nil {
		reply(w, nil, e)
		return
	}
	b := browser(r)
	if b == "" {
		b, e = Random()
		if e != nil {
			reply(w, nil, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "__Host-tp-auth", Value: b, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	}
	t.Browser = Digest(b)
	if e = h.Runtime.Core.Put(r.Context(), "transaction", state, t, 5*time.Minute); e != nil {
		reply(w, nil, e)
		return
	}
	reply(w, map[string]string{"url": p.AuthorizationURL(state, t.Verifier, t.Nonce)}, nil)
}

func (h *HTTP) finishProviderTest(r *http.Request, t transaction) error {
	m := h.Runtime.Settings
	if m == nil {
		return ErrUnavailable
	}
	u, e := h.Runtime.Core.Check(r.Context(), t.Session)
	if e != nil {
		return e
	}
	if u.Delegated || u.Authority != "SYS_ADMIN" || u.ID != t.UserID {
		return ErrForbidden
	}
	c, revision, e := m.draft(r.Context(), t.Provider)
	if e != nil {
		return e
	}
	if c.ReviewID != t.Revision {
		return ErrConflict
	}
	p, e := m.Validate(r.Context(), t.Provider, revision)
	if e != nil {
		return e
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 4096 {
		return ErrUnauthorized
	}
	if _, e = p.Exchange(r.Context(), code, t.Verifier, t.Nonce); e != nil {
		return e
	}
	return m.markTested(r.Context(), u, t.Provider, revision)
}
