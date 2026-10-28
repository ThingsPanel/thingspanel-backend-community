package authcore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// HTTP is the edge adapter for cookie-bearing browser transactions. Neither
// request bodies nor upstream responses may be recorded by ingress/Encore trace.
type HTTP struct{ Runtime *Runtime }
type input struct {
	BecomeUserID     string `json:"become_user_id"`
	PhoneNumber      string `json:"phone_number"`
	PhonePrefix      string `json:"phone_prefix"`
	VerifyCode       string `json:"verify_code"`
	ConfirmPassword  string `json:"confirm_password"`
	MarketRegistered bool   `json:"market_registered"`
	MarketEmail      string `json:"market_email"`
	MarketSource     string `json:"market_source"`

	Email     string `json:"email"`
	Password  string `json:"password"`
	Salt      string `json:"salt"`
	Provider  string `json:"provider"`
	Code      string `json:"code"`
	Ticket    string `json:"ticket"`
	BindingID string `json:"bindingId"`
}
type transaction struct {
	Provider, Verifier, Nonce, Browser, Purpose, UserID, Session, Revision string
	ExpiresAt                                                              int64
}
type resultTicket struct {
	Revision                          string
	ExpiresAt                         int64
	Identity                          Identity
	Browser, Purpose, UserID, Session string
}

func Bearer(r *http.Request) (string, error) {
	a, b := r.Header.Get("Authorization"), r.Header.Get("x-token")
	if a != "" {
		if !strings.HasPrefix(a, "Bearer ") {
			return "", ErrUnauthorized
		}
		a = strings.TrimPrefix(a, "Bearer ")
	}
	if a != "" && b != "" && a != b {
		return "", ErrUnauthorized
	}
	if a == "" {
		a = b
	}
	if strings.ContainsAny(a, " \t\r\n") || len(a) > 8192 {
		return "", ErrUnauthorized
	}
	return a, nil
}
func reply(w http.ResponseWriter, data any, e error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	status := 200
	message := "success"
	if e != nil {
		switch {
		case errors.Is(e, ErrUnauthorized):
			status = 401
		case errors.Is(e, ErrForbidden):
			status = 403
		case errors.Is(e, ErrConflict):
			status = 409
		case errors.Is(e, ErrRateLimit):
			status = 429
		default:
			status = 503
		}
		message = http.StatusText(status)
		var configError *ProviderConfigError
		if errors.As(e, &configError) {
			data = map[string]any{"fields": configError.Fields}
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "message": message, "data": data})
}
func readInput(w http.ResponseWriter, r *http.Request) (input, error) {
	var in input
	if strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])) != "application/json" {
		return in, ErrUnauthorized
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		return in, ErrUnauthorized
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return in, ErrUnauthorized
	}
	return in, nil
}
func browser(r *http.Request) string {
	c, e := r.Cookie("__Host-tp-auth")
	if e != nil || len(c.Value) != 43 {
		return ""
	}
	return c.Value
}
func (h *HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if h.Runtime == nil {
		reply(w, nil, ErrUnavailable)
		return
	}
	s := h.Runtime.Core
	if origin := r.Header.Get("Origin"); origin != "" && origin != h.Runtime.Origin {
		reply(w, nil, ErrForbidden)
		return
	}
	path := r.URL.Path
	if path == "/api/v1/auth/admin/providers" {
		h.providerAdmin(w, r)
		return
	}
	// Resolve optional providers without changing the base password/session path.
	if strings.HasPrefix(path, "/api/v1/auth/external/") || strings.HasPrefix(path, "/api/v1/auth/wechat-mini/") || path == "/api/v1/auth/login-options" {
		snapshot, e := h.Runtime.providerSnapshot(r.Context())
		if e != nil {
			reply(w, nil, e)
			return
		}
		h = &HTTP{Runtime: snapshot}
	}
	if path == "/api/v1/auth/login-options" && r.Method == "GET" {
		channel := r.URL.Query().Get("channel")
		if channel != "web" && channel != "mp-weixin" && channel != "app" {
			reply(w, nil, ErrForbidden)
			return
		}
		providers := h.publicProviders(channel)
		reply(w, map[string]any{"password": map[string]bool{"enabled": true}, "external": map[string]any{"enabled": len(providers) > 0, "providers": providers}}, nil)
		return
	}
	if path == "/api/v1/auth/external/providers" && r.Method == "GET" {
		reply(w, h.publicProviders(""), nil)
		return
	}
	// Do not trust forwarded IP headers here. Deployment must pass client identity
	// through a trusted, rate-limited ingress; this bound also protects direct calls.
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if e := s.Limit(r.Context(), "edge:"+host, 120); e != nil {
		reply(w, nil, e)
		return
	}
	if path == "/api/v1/auth/external/start" && r.Method == "GET" {
		h.start(w, r, input{Provider: r.URL.Query().Get("provider")}, false)
		return
	}
	if path == "/api/v1/auth/external/callback" && r.Method == "GET" {
		h.callback(w, r)
		return
	}
	raw, e := Bearer(r)
	if e != nil {
		reply(w, nil, e)
		return
	}
	if path == "/api/v1/user/detail" && r.Method == "GET" {
		u, e := s.Check(r.Context(), raw)
		if e != nil {
			reply(w, nil, e)
			return
		}
		accounts, ok := s.Accounts.(CompatibilityAccounts)
		if !ok {
			reply(w, nil, ErrUnavailable)
			return
		}
		detail, e := accounts.Detail(r.Context(), u.ID)
		reply(w, detail, e)
		return
	}
	if (path == "/api/v1/auth/logout" && r.Method == "POST") || (path == "/api/v1/user/logout" && r.Method == "GET") {
		reply(w, nil, s.Logout(r.Context(), raw))
		return
	}
	if path == "/api/v1/auth/bindings" && r.Method == "GET" {
		u, e := s.Check(r.Context(), raw)
		if e != nil {
			reply(w, nil, e)
			return
		}
		if targetID := r.URL.Query().Get("user_id"); targetID != "" && targetID != u.ID {
			if u.Delegated || (u.Authority != "SYS_ADMIN" && u.Authority != "TENANT_ADMIN") {
				reply(w, nil, ErrForbidden)
				return
			}
			target, e := s.Accounts.User(r.Context(), targetID, false)
			if e != nil {
				reply(w, nil, ErrForbidden)
				return
			}
			if u.Authority != "SYS_ADMIN" && (u.TenantID != target.TenantID || target.Authority != "TENANT_USER") {
				reply(w, nil, ErrForbidden)
				return
			}
			u = target
		}
		b, e := s.Accounts.Bindings(r.Context(), u)
		reply(w, b, e)
		return
	}
	if path == "/api/v1/user/refresh" && r.Method == "GET" {
		session, e := s.Refresh(r.Context(), raw)
		reply(w, session, e)
		return
	}
	if (path == "/api/v1/tenant/setup-state" || path == "/api/v1/tenant/has-admin") && r.Method == "GET" {
		a, ok := s.Accounts.(CompatibilityAccounts)
		if !ok {
			reply(w, nil, ErrUnavailable)
			return
		}
		yes, e := a.HasAdmin(r.Context())
		entry := "login"
		if !yes {
			entry = "register"
		}
		reply(w, map[string]any{"has_admin": yes, "entry": entry}, e)
		return
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	in, e := readInput(w, r)
	if e != nil {
		reply(w, nil, e)
		return
	}
	switch path {
	case "/api/v1/tenant/super-admin/init", "/api/v1/tenant/market-register":
		a, ok := s.Accounts.(CompatibilityAccounts)
		if !ok {
			reply(w, nil, ErrUnavailable)
			return
		}
		if in.ConfirmPassword != "" && in.ConfirmPassword != in.Password {
			reply(w, nil, ErrUnauthorized)
			return
		}
		u, e := a.Initialize(r.Context(), in.Email, in.Password)
		if e != nil {
			reply(w, nil, e)
			return
		}
		session, e := s.Issue(r.Context(), u)
		reply(w, session, e)
	case "/api/v1/tenant/email/register":
		u, e := s.Signup(r.Context(), in)
		if e != nil {
			reply(w, nil, e)
			return
		}
		session, e := s.Issue(r.Context(), u)
		reply(w, session, e)
	case "/api/v1/user/transform":
		actor, e := s.Check(r.Context(), raw)
		if e != nil {
			reply(w, nil, e)
			return
		}
		if actor.Delegated || (actor.Authority != "SYS_ADMIN" && actor.Authority != "TENANT_ADMIN") {
			reply(w, nil, ErrForbidden)
			return
		}
		target, e := s.Accounts.User(r.Context(), in.BecomeUserID, false)
		if e != nil {
			reply(w, nil, e)
			return
		}
		if actor.Authority != "SYS_ADMIN" && (actor.TenantID != target.TenantID || target.Authority == "SYS_ADMIN") {
			reply(w, nil, ErrForbidden)
			return
		}
		accounts, ok := s.Accounts.(*SQLAccounts)
		if !ok {
			reply(w, nil, ErrUnavailable)
			return
		}
		if _, e = accounts.DB.ExecContext(r.Context(), `INSERT INTO auth_audit_events(actor_id,user_id,event,provider) VALUES($1,$2,'delegation_requested','local')`, actor.ID, target.ID); e != nil {
			reply(w, nil, ErrUnavailable)
			return
		}
		target.Delegated = true
		target.ActorID = actor.ID
		target.ActorVersion = actor.Version
		session, e := s.Issue(r.Context(), target)
		reply(w, session, e)
	case "/api/v1/login":
		u, e := s.VerifyPassword(r.Context(), in.Email, in.Password, in.Salt)
		if e != nil {
			reply(w, nil, e)
			return
		}
		session, e := s.Issue(r.Context(), u)
		reply(w, session, e)
	case "/api/v1/auth/external/bind/start":
		h.start(w, r, in, true)
	case "/api/v1/auth/external/exchange", "/api/v1/auth/external/bind/complete":
		h.exchange(w, r, in, raw)
	case "/api/v1/auth/wechat-mini/login", "/api/v1/auth/wechat-mini/bind":
		p := h.Runtime.Providers[in.Provider]
		if p == nil || p.Config.Kind != "wechat-mini" {
			reply(w, nil, ErrForbidden)
			return
		}
		if e := s.Limit(r.Context(), "provider:"+p.Config.ID, 300); e != nil {
			reply(w, nil, e)
			return
		}
		var u User
		if strings.HasSuffix(path, "/bind") {
			u, e = s.Check(r.Context(), raw)
			if e != nil {
				reply(w, nil, e)
				return
			}
			if u.Delegated || !p.Allows(u) {
				reply(w, nil, ErrForbidden)
				return
			}
			if u.Delegated {
				reply(w, nil, ErrForbidden)
				return
			}
			proof, e := s.VerifyPassword(r.Context(), u.Email, in.Password, in.Salt)
			if e != nil || proof.ID != u.ID || proof.Version != u.Version {
				if e == nil {
					e = ErrUnauthorized
				}
				reply(w, nil, e)
				return
			}
		}
		id, e := p.Mini(r.Context(), s, in.Code)
		if e != nil {
			reply(w, nil, e)
			return
		}
		if strings.HasSuffix(path, "/bind") {
			if _, e = s.Check(r.Context(), raw); e == nil {
				e = s.Accounts.Bind(r.Context(), u, id)
			}
			reply(w, map[string]string{"status": "bound"}, e)
			return
		}
		h.loginIdentity(w, r, p, id)
	case "/api/v1/auth/bindings/disable", "/api/v1/auth/bindings/allow-rebind", "/api/v1/auth/bindings/unlink":
		u, e := s.Check(r.Context(), raw)
		if e != nil {
			reply(w, nil, e)
			return
		}
		if u.Delegated {
			reply(w, nil, ErrForbidden)
			return
		}
		proof, e := s.VerifyPassword(r.Context(), u.Email, in.Password, in.Salt)
		if e != nil || proof.ID != u.ID || proof.Version != u.Version {
			if e == nil {
				e = ErrUnauthorized
			}
			reply(w, nil, e)
			return
		}
		if _, e = s.Check(r.Context(), raw); e != nil {
			reply(w, nil, e)
			return
		}
		if path == "/api/v1/auth/bindings/unlink" {
			a, ok := s.Accounts.(interface {
				Unlink(context.Context, User, string) error
			})
			if !ok {
				reply(w, nil, ErrUnavailable)
				return
			}
			reply(w, nil, a.Unlink(r.Context(), u, in.BindingID))
			return
		}
		if path == "/api/v1/auth/bindings/allow-rebind" {
			a, ok := s.Accounts.(interface {
				AllowRebind(context.Context, User, string) error
			})
			if !ok {
				reply(w, nil, ErrUnavailable)
				return
			}
			reply(w, nil, a.AllowRebind(r.Context(), u, in.BindingID))
			return
		}
		reply(w, nil, s.Accounts.Disable(r.Context(), u, in.BindingID))
	default:
		http.NotFound(w, r)
	}
}
func (h *HTTP) start(w http.ResponseWriter, r *http.Request, in input, bind bool) {
	p := h.Runtime.Providers[in.Provider]
	if p == nil || !p.BrowserProvider() {
		reply(w, nil, ErrForbidden)
		return
	}
	s := h.Runtime.Core
	t := transaction{Provider: p.Config.ID, Purpose: "login", Revision: p.Config.ReviewID, ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}
	if bind {
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
		if u.Delegated || !p.Allows(u) {
			reply(w, nil, ErrForbidden)
			return
		}
		if u.Delegated {
			reply(w, nil, ErrForbidden)
			return
		}
		proof, e := s.VerifyPassword(r.Context(), u.Email, in.Password, in.Salt)
		if e != nil || proof.ID != u.ID || proof.Version != u.Version {
			if e == nil {
				e = ErrUnauthorized
			}
			reply(w, nil, e)
			return
		}
		t.Purpose = "bind"
		t.UserID = u.ID
		t.Session = Digest(raw)
	}
	state, e := Random()
	if e != nil {
		reply(w, nil, ErrUnavailable)
		return
	}
	t.Verifier, e = Random()
	if e != nil {
		reply(w, nil, ErrUnavailable)
		return
	}
	t.Nonce, e = Random()
	if e != nil {
		reply(w, nil, ErrUnavailable)
		return
	}
	b := browser(r)
	if b == "" {
		b, e = Random()
		if e != nil {
			reply(w, nil, ErrUnavailable)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "__Host-tp-auth", Value: b, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	}
	t.Browser = Digest(b)
	if e = s.Put(r.Context(), "transaction", state, t, 5*time.Minute); e != nil {
		reply(w, nil, e)
		return
	}
	target := p.AuthorizationURL(state, t.Verifier, t.Nonce)
	if bind {
		reply(w, map[string]string{"url": target}, nil)
	} else {
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}
func (h *HTTP) callback(w http.ResponseWriter, r *http.Request) {
	var t transaction
	s := h.Runtime.Core
	if e := s.Take(r.Context(), "transaction", r.URL.Query().Get("state"), &t); e != nil {
		http.Redirect(w, r, h.Runtime.Origin+"/auth-callback?authError=failed", http.StatusSeeOther)
		return
	}
	if time.Now().Unix() >= t.ExpiresAt || browser(r) == "" || t.Browser != Digest(browser(r)) || r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, h.Runtime.Origin+"/auth-callback?authError=failed", http.StatusSeeOther)
		return
	}
	if t.Purpose == "provider-test" {
		result := "success"
		if h.finishProviderTest(r, t) != nil {
			result = "failed"
		}
		http.Redirect(w, r, h.Runtime.Origin+"/management/setting?authTest="+result, http.StatusSeeOther)
		return
	}
	p := h.Runtime.Providers[t.Provider]
	if p == nil || !p.BrowserProvider() || p.Config.ReviewID != t.Revision {
		http.Redirect(w, r, h.Runtime.Origin+"/auth-callback?authError=failed", http.StatusSeeOther)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 4096 {
		http.Redirect(w, r, h.Runtime.Origin+"/auth-callback?authError=failed", http.StatusSeeOther)
		return
	}
	id, e := p.Exchange(r.Context(), code, t.Verifier, t.Nonce)
	if e != nil {
		http.Redirect(w, r, h.Runtime.Origin+"/auth-callback?authError=failed", http.StatusSeeOther)
		return
	}
	ticket, e := Random()
	if e != nil {
		http.Redirect(w, r, h.Runtime.Origin+"/auth-callback?authError=failed", http.StatusSeeOther)
		return
	}
	result := resultTicket{Revision: t.Revision, ExpiresAt: t.ExpiresAt, Identity: id, Browser: t.Browser, Purpose: t.Purpose, UserID: t.UserID, Session: t.Session}
	if e = s.Put(r.Context(), "ticket", ticket, result, time.Minute); e != nil {
		http.Redirect(w, r, h.Runtime.Origin+"/auth-callback?authError=failed", http.StatusSeeOther)
		return
	}
	// Fragment avoids proxy/referrer query logging. The client clears it before exchange.
	http.Redirect(w, r, h.Runtime.Origin+"/auth-callback#ticket="+url.QueryEscape(ticket)+"&purpose="+t.Purpose, http.StatusSeeOther)
}
func (h *HTTP) exchange(w http.ResponseWriter, r *http.Request, in input, raw string) {
	var t resultTicket
	s := h.Runtime.Core
	if e := s.Take(r.Context(), "ticket", in.Ticket, &t); e != nil {
		reply(w, nil, e)
		return
	}
	purpose := "login"
	if strings.HasSuffix(r.URL.Path, "/bind/complete") {
		purpose = "bind"
	}
	if time.Now().Unix() >= t.ExpiresAt || browser(r) == "" || t.Browser != Digest(browser(r)) || t.Purpose != purpose {
		reply(w, nil, ErrUnauthorized)
		return
	}
	p := h.Runtime.Providers[t.Identity.Provider]
	if p == nil || p.Config.ReviewID != t.Revision || p.Config.ClientID != t.Identity.ClientID || p.Config.Issuer != t.Identity.Issuer {
		reply(w, nil, ErrUnauthorized)
		return
	}
	if purpose == "bind" {
		u, e := s.Check(r.Context(), raw)
		if e != nil {
			reply(w, nil, e)
			return
		}
		if u.ID != t.UserID || Digest(raw) != t.Session || !p.Allows(u) {
			reply(w, nil, ErrForbidden)
			return
		}
		reply(w, map[string]string{"status": "bound"}, s.Accounts.Bind(r.Context(), u, t.Identity))
		return
	}
	h.loginIdentity(w, r, p, t.Identity)
}
func (h *HTTP) loginIdentity(w http.ResponseWriter, r *http.Request, p *Provider, id Identity) {
	u, e := h.Runtime.Core.Accounts.FindIdentity(r.Context(), id)
	if errors.Is(e, ErrUnauthorized) {
		reply(w, map[string]string{"status": "binding_required", "provider": p.Config.ID}, nil)
		return
	}
	if e != nil {
		reply(w, nil, e)
		return
	}
	if u.Delegated || !p.Allows(u) {
		reply(w, nil, ErrForbidden)
		return
	}
	session, e := h.Runtime.Core.Issue(r.Context(), u)
	reply(w, session, e)
}

func (h *HTTP) publicProviders(channel string) []map[string]string {
	out := []map[string]string{}
	for _, p := range h.Runtime.Providers {
		if channel == "app" || (channel == "web" && p.Config.Kind == "wechat-mini") || (channel == "mp-weixin" && p.Config.Kind != "wechat-mini") {
			continue
		}
		out = append(out, map[string]string{"id": p.Config.ID, "label": p.Config.Label, "kind": p.Config.Kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"] < out[j]["id"] })
	return out
}
