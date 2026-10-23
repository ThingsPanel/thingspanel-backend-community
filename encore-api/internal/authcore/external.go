package authcore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type ProviderConfig struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Kind         string   `json:"kind"` // oidc, wechat-web or wechat-mini
	Issuer       string   `json:"issuer"`
	ClientID     string   `json:"clientId"`
	ClientSecret string   `json:"clientSecret"`
	AuthorizeURL string   `json:"authorizeUrl"`
	TokenURL     string   `json:"tokenUrl"`
	JWKSURL      string   `json:"jwksUrl"`
	RedirectURL  string   `json:"redirectUrl"`
	AppID        string   `json:"appId"`
	AppSecret    string   `json:"appSecret"`
	TenantIDs    []string `json:"tenantIds"`
	// A deployment must explicitly record its pinned upstream security review.
	ReviewID string `json:"reviewId"`
}
type Provider struct {
	Config   ProviderConfig
	OAuth    oauth2.Config
	Verifier *oidc.IDTokenVerifier
	HTTP     *http.Client
}

type boundedKeysTransport struct {
	base    http.RoundTripper
	keysURL string
	mu      sync.Mutex
	last    time.Time
}

func (t *boundedKeysTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() == t.keysURL {
		t.mu.Lock()
		if time.Since(t.last) < time.Second {
			t.mu.Unlock()
			return nil, ErrUnavailable
		}
		t.last = time.Now()
		t.mu.Unlock()
	}
	return t.base.RoundTrip(r)
}

func validHTTPS(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}
func NewProvider(c ProviderConfig) (*Provider, error) {
	if c.ID == "" || c.Label == "" || c.ReviewID == "" || len(c.TenantIDs) == 0 {
		return nil, ErrUnavailable
	}
	if c.Kind != "oidc" && c.Kind != "wechat-mini" && c.Kind != "wechat-web" {
		return nil, ErrUnavailable
	}
	if c.Kind == "wechat-mini" || c.Kind == "wechat-web" {
		if c.AppID == "" || c.AppSecret == "" || strings.ContainsAny(c.AppID, "/?# ") {
			return nil, ErrUnavailable
		}
		// Scope OpenID to its AppID. UnionID never participates in account linking.
		// Fixed HTTPS endpoint prevents sending the AppSecret to a configured URL.
		c.Issuer = "https://api.weixin.qq.com/mini/" + c.AppID
		c.ClientID = c.AppID
		c.TokenURL = "https://api.weixin.qq.com/sns/jscode2session"
		if c.Kind == "wechat-web" {
			if !validHTTPS(c.RedirectURL) {
				return nil, ErrUnavailable
			}
			c.Issuer = "https://api.weixin.qq.com/web/" + c.AppID
			c.TokenURL = "https://api.weixin.qq.com/sns/oauth2/access_token"
			c.AuthorizeURL = "https://open.weixin.qq.com/connect/qrconnect"
		}
	} else if c.ClientID == "" || !validHTTPS(c.Issuer) || !validHTTPS(c.TokenURL) || !validHTTPS(c.JWKSURL) || !validHTTPS(c.AuthorizeURL) || !validHTTPS(c.RedirectURL) || c.ClientSecret == "" {
		return nil, ErrUnavailable
	}
	h := &http.Client{Timeout: 8 * time.Second, Transport: &boundedKeysTransport{keysURL: c.JWKSURL, base: &http.Transport{Proxy: nil, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.Kind != "oidc" {
		return &Provider{Config: c, HTTP: h}, nil
	}
	ctx := oidc.ClientContext(context.Background(), h)
	keyset := oidc.NewRemoteKeySet(ctx, c.JWKSURL)
	p := &Provider{Config: c, HTTP: h, Verifier: oidc.NewVerifier(c.Issuer, keyset, &oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{"RS256"}})}
	p.OAuth = oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: c.RedirectURL, Scopes: []string{oidc.ScopeOpenID}, Endpoint: oauth2.Endpoint{AuthURL: c.AuthorizeURL, TokenURL: c.TokenURL}}
	return p, nil
}
func (p *Provider) Allows(u User) bool {
	// Personal social login never grants a platform-admin entry.
	if u.Authority == "SYS_ADMIN" {
		return false
	}
	for _, t := range p.Config.TenantIDs {
		if t == u.TenantID {
			return true
		}
	}
	return false
}
func (p *Provider) verify(ctx context.Context, raw, nonce string) (Identity, error) {
	if len(raw) > 64<<10 {
		return Identity{}, ErrUnauthorized
	}
	token, e := p.Verifier.Verify(ctx, raw)
	if e != nil {
		return Identity{}, ErrUnauthorized
	}
	if token.Subject == "" || !token.Expiry.After(time.Now()) || (nonce != "" && token.Nonce != nonce) {
		return Identity{}, ErrUnauthorized
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if token.Claims(&claims) != nil || (len(token.Audience) > 1 && claims.AuthorizedParty == "") || (claims.AuthorizedParty != "" && claims.AuthorizedParty != p.Config.ClientID) {
		return Identity{}, ErrUnauthorized
	}
	return Identity{Issuer: p.Config.Issuer, Subject: token.Subject, Provider: p.Config.ID, ClientID: p.Config.ClientID}, nil
}
func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	if p.Config.Kind == "wechat-web" {
		return p.exchangeWeChatWeb(ctx, code)
	}
	token, e := p.OAuth.Exchange(oidc.ClientContext(ctx, p.HTTP), code, oauth2.VerifierOption(verifier))
	if e != nil {
		return Identity{}, ErrUnauthorized
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return Identity{}, ErrUnauthorized
	}
	return p.verify(ctx, raw, nonce)
}

func (p *Provider) BrowserProvider() bool {
	return p.Config.Kind == "oidc" || p.Config.Kind == "wechat-web"
}

func (p *Provider) AuthorizationURL(state, verifier, nonce string) string {
	if p.Config.Kind == "wechat-web" {
		q := url.Values{"appid": {p.Config.AppID}, "redirect_uri": {p.Config.RedirectURL}, "response_type": {"code"}, "scope": {"snsapi_login"}, "state": {state}}
		return "https://open.weixin.qq.com/connect/qrconnect?" + q.Encode() + "#wechat_redirect"
	}
	return p.OAuth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("nonce", nonce))
}

// WeChat website login is OAuth, not OIDC: no fabricated ID token or nonce.
// The one-use, browser-bound state is verified before this server-side exchange.
// Never persist or log upstream access/refresh tokens or UnionID.
func (p *Provider) exchangeWeChatWeb(ctx context.Context, code string) (Identity, error) {
	q := url.Values{"appid": {p.Config.AppID}, "secret": {p.Config.AppSecret}, "code": {code}, "grant_type": {"authorization_code"}}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.weixin.qq.com/sns/oauth2/access_token?"+q.Encode(), nil)
	if e != nil {
		return Identity{}, ErrUnavailable
	}
	res, e := p.HTTP.Do(req)
	if e != nil {
		return Identity{}, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Identity{}, ErrUnavailable
	}
	body, e := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if e != nil || len(body) > 64<<10 {
		return Identity{}, ErrUnavailable
	}
	var result struct {
		OpenID      string `json:"openid"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
		ErrorCode   int    `json:"errcode"`
	}
	if json.Unmarshal(body, &result) != nil {
		return Identity{}, ErrUnavailable
	}
	if result.ErrorCode == -1 {
		return Identity{}, ErrUnavailable
	}
	allowed := false
	for _, scope := range strings.Split(result.Scope, ",") {
		if strings.TrimSpace(scope) == "snsapi_login" {
			allowed = true
		}
	}
	if result.ErrorCode != 0 || result.OpenID == "" || len(result.OpenID) > 256 || result.AccessToken == "" || result.ExpiresIn <= 0 || !allowed {
		return Identity{}, ErrUnauthorized
	}
	return Identity{Issuer: p.Config.Issuer, Subject: result.OpenID, Provider: p.Config.ID, ClientID: p.Config.AppID}, nil
}
func (p *Provider) Mini(ctx context.Context, s *Service, code string) (Identity, error) {
	if p.Config.Kind != "wechat-mini" || len(code) < 8 || len(code) > 512 {
		return Identity{}, ErrUnauthorized
	}
	if e := s.Claim(ctx, "wechat:"+p.Config.AppID, code, 10*time.Minute); e != nil {
		return Identity{}, e
	}
	query := url.Values{"appid": {p.Config.AppID}, "secret": {p.Config.AppSecret}, "js_code": {code}, "grant_type": {"authorization_code"}}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.weixin.qq.com/sns/jscode2session?"+query.Encode(), nil)
	if e != nil {
		return Identity{}, ErrUnavailable
	}
	// Never log the request URL, provider error, response body, or session_key.
	res, e := p.HTTP.Do(req)
	if e != nil {
		return Identity{}, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Identity{}, ErrUnavailable
	}
	body, e := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if e != nil || len(body) > 64<<10 {
		return Identity{}, ErrUnavailable
	}
	var response struct {
		OpenID     string `json:"openid"`
		SessionKey string `json:"session_key"`
		ErrorCode  int    `json:"errcode"`
	}
	if json.Unmarshal(body, &response) != nil {
		return Identity{}, ErrUnavailable
	}
	if response.ErrorCode == -1 {
		return Identity{}, ErrUnavailable
	}
	if response.ErrorCode == 45011 {
		return Identity{}, ErrRateLimit
	}
	if response.ErrorCode != 0 || response.OpenID == "" || len(response.OpenID) > 256 || response.SessionKey == "" {
		return Identity{}, ErrUnauthorized
	}
	return Identity{Issuer: p.Config.Issuer, Subject: response.OpenID, Provider: p.Config.ID, ClientID: p.Config.AppID}, nil
}
