package authcore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWeChatWebBrowserTransaction(t *testing.T) {
	s, a, redis := fixture(t)
	p, e := NewProvider(ProviderConfig{ID: "web", Label: "Website WeChat", Kind: "wechat-web", AppID: "web-app", AppSecret: "fixture", RedirectURL: "https://panel.example/api/v1/auth/external/callback", ReviewID: "fixture", TenantIDs: []string{"tenant-1"}})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	payload := `{"openid":"web-subject","access_token":"must-not-leak","expires_in":7200,"scope":"snsapi_login"}`
	p.HTTP.Transport = miniTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "api.weixin.qq.com" || r.URL.Path != "/sns/oauth2/access_token" || r.URL.Query().Get("appid") != "web-app" || r.URL.Query().Get("secret") != "fixture" {
			t.Error("wrong fixed website exchange")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
	})
	h := &HTTP{Runtime: &Runtime{Core: s, Providers: map[string]*Provider{"web": p}, Origin: "https://panel.example"}}
	start := func() (string, *http.Cookie) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/auth/external/start?provider=web", nil))
		target, _ := url.Parse(w.Header().Get("Location"))
		q := target.Query()
		if w.Code != 303 || target.Host != "open.weixin.qq.com" || q.Get("scope") != "snsapi_login" || q.Get("appid") != "web-app" || q.Has("nonce") || q.Has("code_challenge") {
			t.Fatal("wrong QR authorization request")
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
			t.Fatal("unsafe transaction cookie")
		}
		return q.Get("state"), cookies[0]
	}
	callback := func(state string, cookie *http.Cookie, extra string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/auth/external/callback?state="+url.QueryEscape(state)+extra, nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	state, cookie := start()
	w := callback(state, cookie, "&code=realistic-fixture-code")
	target, _ := url.Parse(w.Header().Get("Location"))
	fragment, _ := url.ParseQuery(target.Fragment)
	ticket := fragment.Get("ticket")
	if ticket == "" || target.RawQuery != "" || calls != 1 {
		t.Fatal("callback failed or secret in query")
	}
	exchange := func(ticket string, cookie *http.Cookie) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"ticket": ticket})
		r := httptest.NewRequest("POST", "/api/v1/auth/external/exchange", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w = exchange(ticket, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "binding_required") || strings.Contains(w.Body.String(), "must-not-leak") {
		t.Fatal("unbound response leaked credentials")
	}
	if exchange(ticket, cookie).Code != 401 {
		t.Fatal("ticket replay allowed")
	}
	if !strings.Contains(callback(state, cookie, "&code=replay").Header().Get("Location"), "authError") || calls != 1 {
		t.Fatal("state replay reached upstream")
	}
	for _, tc := range []string{"wrong-browser", "cancelled", "expired"} {
		t.Run(tc, func(t *testing.T) {
			state, cookie := start()
			extra := "&code=next"
			if tc == "wrong-browser" {
				cookie.Value = strings.Repeat("x", 43)
			}
			if tc == "cancelled" {
				extra = "&error=access_denied"
			}
			if tc == "expired" {
				redis.FastForward(6 * time.Minute)
			}
			if !strings.Contains(callback(state, cookie, extra).Header().Get("Location"), "authError") {
				t.Fatal("invalid transaction accepted")
			}
		})
	}
	if calls != 1 {
		t.Fatal("invalid callbacks reached provider")
	}
	// A source is rechecked after callback, before the final platform session.
	state, cookie = start()
	w = callback(state, cookie, "&code=next-code")
	target, _ = url.Parse(w.Header().Get("Location"))
	fragment, _ = url.ParseQuery(target.Fragment)
	a.Bind(context.Background(), a.user, Identity{Issuer: p.Config.Issuer, Subject: "web-subject", Provider: "web", ClientID: "web-app"})
	p.Config.TenantIDs = []string{"other-tenant"}
	if exchange(fragment.Get("ticket"), cookie).Code != 403 {
		t.Fatal("tenant restriction bypass")
	}
	payload = `{"openid":"web-subject","access_token":"ignored","expires_in":7200,"scope":"snsapi_base"}`
	if _, e = p.Exchange(context.Background(), "wrong-scope", "", ""); e == nil {
		t.Fatal("wrong website scope accepted")
	}
}
