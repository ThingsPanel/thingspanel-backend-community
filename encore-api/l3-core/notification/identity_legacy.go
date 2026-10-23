package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encore.app/internal/authcore"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"encore.app/l1-access/authn"
	"encore.app/l3-core/notification/contract"
)

var ErrLegacySessionRejected = errors.New("legacy user session is invalid")

type LegacySessionChecker struct {
	endpoint string
	client   *http.Client
}

type LegacySessionOptions struct {
	AllowHTTPTestEndpoint bool
	TestMode              bool
	Timeout               time.Duration
}

func NewLegacySessionChecker(endpoint string, options LegacySessionOptions) (*LegacySessionChecker, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed == nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" || parsed.Path != "" {
		return nil, ErrLegacySessionRejected
	}
	if parsed.Scheme != "https" && !(options.TestMode && options.AllowHTTPTestEndpoint && parsed.Scheme == "http" && legacyLoopback(parsed.Hostname())) {
		return nil, ErrLegacySessionRejected
	}
	if options.Timeout <= 0 || options.Timeout > 10*time.Second {
		options.Timeout = 3 * time.Second
	}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: options.Timeout, ResponseHeaderTimeout: options.Timeout}
	return &LegacySessionChecker{
		endpoint: strings.TrimRight(parsed.String(), "/") + "/api/v1/notification/session-context",
		client:   &http.Client{Transport: transport, Timeout: options.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *LegacySessionChecker) CheckUserSession(ctx context.Context, bearer string, expected UserPrincipal) error {
	platformActor := containsRole(expected.Roles, "SYS_ADMIN")
	if c == nil || c.client == nil || bearer == "" || strings.ContainsAny(bearer, "\r\n\x00 \t") || len(bearer) > 8192 || expected.UserID == "" || (expected.TenantID == "" && !platformActor) {
		return ErrLegacySessionRejected
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return ErrLegacySessionRejected
	}
	request.Header.Set("x-token", bearer)
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return ErrLegacySessionRejected
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrLegacySessionRejected
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	if err != nil || len(body) > 16<<10 {
		return ErrLegacySessionRejected
	}
	var result struct {
		UserID    string `json:"userId"`
		TenantID  string `json:"tenantId"`
		Authority string `json:"authority"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.UserID != expected.UserID || result.TenantID != expected.TenantID || !authorityMatches(expected.Roles, result.Authority) {
		return ErrLegacySessionRejected
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrLegacySessionRejected
	}
	return nil
}

func authorityMatches(roles []string, authority string) bool {
	want := canonicalAuthority(authority)
	if want != "SYS_ADMIN" && want != "TENANT_ADMIN" && want != "TENANT_USER" {
		return false
	}
	for _, role := range roles {
		if canonicalAuthority(role) == want {
			return true
		}
	}
	return false
}

func legacyLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	parsed, err := netip.ParseAddr(host)
	return err == nil && parsed.IsLoopback()
}

func verifyLegacyUserToken(raw, key string, now time.Time) (UserPrincipal, error) {
	token := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	parts := strings.Split(token, ".")
	if key == "" || len(parts) != 3 || strings.ContainsAny(token, "\r\n\x00 \t") {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	var header map[string]json.RawMessage
	if contract.DecodeStrictJSON(headerBytes, &header) != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	var algorithm string
	if json.Unmarshal(header["alg"], &algorithm) != nil || algorithm != "HS256" {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	var claims map[string]json.RawMessage
	if contract.DecodeStrictJSON(claimsBytes, &claims) != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	var userID, tenantID, authority string
	var expiresAt int64
	if json.Unmarshal(claims["id"], &userID) != nil || json.Unmarshal(claims["tenant_id"], &tenantID) != nil || json.Unmarshal(claims["authority"], &authority) != nil || json.Unmarshal(claims["exp"], &expiresAt) != nil || expiresAt <= now.Unix() {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	authority = strings.ToUpper(strings.TrimSpace(authority))
	if strings.TrimSpace(userID) == "" || len(userID) > 128 || strings.ContainsAny(userID, "\r\n\x00") || (strings.TrimSpace(tenantID) == "" && authority != "SYS_ADMIN") || (authority != "SYS_ADMIN" && authority != "TENANT_ADMIN" && authority != "TENANT_USER") {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	return UserPrincipal{UserID: userID, TenantID: tenantID, Roles: []string{authority}, Scopes: scopesForAuthority(authority)}, nil
}

func (s *ManagementService) verifiedPrincipal(ctx context.Context, data *authn.Data, bearer string) (UserPrincipal, error) {
	if principal, ok := ctx.Value(verifiedUserPrincipalContextKey{}).(UserPrincipal); ok {
		if data != nil && (data.UserID != principal.UserID || data.TenantID != principal.TenantID || !containsRole(data.Roles, principal.Roles[0])) {
			return UserPrincipal{}, ErrIdentityUnauthorized
		}
		return principal, nil
	}
	principal, err := s.VerifyUser(ctx, bearer)
	if err != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	if data != nil && (data.UserID != principal.UserID || data.TenantID != principal.TenantID || !containsRole(data.Roles, principal.Roles[0])) {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	return principal, nil
}

type verifiedUserPrincipalContextKey struct{}

func withVerifiedUserPrincipal(ctx context.Context, principal UserPrincipal) context.Context {
	return context.WithValue(ctx, verifiedUserPrincipalContextKey{}, principal)
}

// VerifyUser implements the principal verifier used by public raw Encore
// routes. The original bearer is verified here and rechecked against the
// community JWT + Redis session-context endpoint on every call.
func (s *ManagementService) VerifyUser(ctx context.Context, bearer string) (UserPrincipal, error) {
	if s == nil || s.sessions == nil || s.privacy == nil || s.privacy.Check() != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	if authcore.Enabled() {
		raw, e := normalizeBearer(bearer)
		if e != nil {
			return UserPrincipal{}, ErrIdentityUnauthorized
		}
		runtime, e := authcore.Load()
		if e != nil {
			return UserPrincipal{}, ErrIdentityUnauthorized
		}
		u, e := runtime.Core.Check(ctx, raw)
		if e != nil || u.Delegated {
			return UserPrincipal{}, ErrIdentityUnauthorized
		}
		return UserPrincipal{UserID: u.ID, TenantID: u.TenantID, Roles: []string{u.Authority}, Scopes: scopesForAuthority(u.Authority)}, nil
	}
	mode := strings.TrimSpace(os.Getenv("TP_AUTH_MODE"))
	if mode != "" && mode != "legacy-jwt" {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	principal, err := verifyLegacyUserToken(bearer, os.Getenv("TP_JWT_KEY"), s.clock())
	if err != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	token, err := normalizeBearer(bearer)
	if err != nil || s.sessions.CheckUserSession(ctx, token, principal) != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	return principal, nil
}

func containsRole(roles []string, target string) bool {
	for _, role := range roles {
		if canonicalAuthority(role) == canonicalAuthority(target) {
			return true
		}
	}
	return false
}

func scopesForAuthority(authority string) []string {
	switch strings.ToUpper(strings.TrimSpace(authority)) {
	case "SYS_ADMIN":
		return []string{"notification:read", "notification:manage", "notification:send", "notification:send_test", "notification:plugin:manage"}
	case "TENANT_ADMIN":
		return []string{"notification:read", "notification:manage", "notification:send", "notification:send_test"}
	case "TENANT_USER":
		return []string{"notification:read"}
	default:
		return nil
	}
}

func canonicalAuthority(role string) string {
	switch strings.ToUpper(strings.TrimSpace(role)) {
	case "PLATFORM_ADMIN":
		return "SYS_ADMIN"
	case "TENANT_VIEWER":
		return "TENANT_USER"
	default:
		return strings.ToUpper(strings.TrimSpace(role))
	}
}
