package authn

import (
	"encore.app/internal/authcore"
	"net/http"
)

func serveAuthentication(w http.ResponseWriter, r *http.Request) {
	runtime, e := authcore.Load()
	if e != nil {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	(&authcore.HTTP{Runtime: runtime}).ServeHTTP(w, r)
}

// Authentication is public only at the transport boundary. Each operation
// verifies its own one-time proof or human session before performing any action.
// Raw endpoints keep cookie redirects and legacy envelopes in one edge adapter.
//
//encore:api public raw path=/api/v1/auth/*path
func Authentication(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=POST path=/api/v1/login
func PasswordLogin(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=GET path=/api/v1/user/detail
func UserDetail(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=GET path=/api/v1/user/logout
func LegacyLogout(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=GET path=/api/v1/user/refresh
func LegacyRefresh(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=GET path=/api/v1/tenant/setup-state
func SetupState(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=GET path=/api/v1/tenant/has-admin
func HasAdmin(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=POST path=/api/v1/tenant/super-admin/init
func InitializeAdmin(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=POST path=/api/v1/tenant/market-register
func LegacyInitializeAdmin(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=POST path=/api/v1/tenant/email/register
func EmailSignup(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }

//encore:api public raw method=POST path=/api/v1/user/transform
func DelegateUser(w http.ResponseWriter, r *http.Request) { serveAuthentication(w, r) }
