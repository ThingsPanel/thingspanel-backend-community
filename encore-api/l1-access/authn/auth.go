package authn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encore.app/internal/authcore"
	"errors"
	"flag"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
)

var (
	devModeWarnOnce         sync.Once
	passthroughModeWarnOnce sync.Once
)

// Data is injected into request context by Encore auth handler.
// It is used by service-level authorization checks.
type Data struct {
	UserID              string   `json:"user_id"`
	TenantID            string   `json:"tenant_id"`
	Roles               []string `json:"roles"`
	Scopes              []string `json:"scopes"`
	AuthorizedDeviceIDs []string `json:"authorized_device_ids"`
}

type AuthParams struct {
	Authorization string `header:"Authorization"`
	LegacyToken   string `header:"x-token"`
}

//encore:authhandler
func AuthHandler(ctx context.Context, params *AuthParams) (auth.UID, *Data, error) {
	if params == nil {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "missing token"}
	}
	token := strings.TrimSpace(params.Authorization)
	if token != "" {
		if !strings.HasPrefix(token, "Bearer ") {
			return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authorization"}
		}
		token = strings.TrimPrefix(token, "Bearer ")
	}
	if token != "" && params.LegacyToken != "" && token != params.LegacyToken {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "conflicting credentials"}
	}
	if token == "" {
		token = params.LegacyToken
	}
	return authenticate(ctx, token)
}
func authenticate(ctx context.Context, token string) (auth.UID, *Data, error) {
	t := strings.TrimSpace(token)
	if t == "" {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "missing token"}
	}

	// Accept "Bearer <token>" and raw token.
	if strings.HasPrefix(strings.ToLower(t), "bearer ") {
		t = strings.TrimSpace(t[7:])
	}

	mode := strings.TrimSpace(os.Getenv("TP_AUTH_MODE"))
	if mode == "" {
		mode = "legacy-jwt"
	}

	switch mode {
	case "native":
		runtime, e := authcore.Load()
		if e != nil {
			return "", nil, &errs.Error{Code: errs.Unavailable, Message: "authentication unavailable"}
		}
		u, e := runtime.Core.Check(ctx, t)
		if e != nil {
			code := errs.Unauthenticated
			if errors.Is(e, authcore.ErrUnavailable) {
				code = errs.Unavailable
			}
			return "", nil, &errs.Error{Code: code, Message: "session unavailable"}
		}
		role, e := authcore.Role(u.Authority)
		if e != nil {
			return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid role"}
		}
		return auth.UID(u.ID), &Data{UserID: u.ID, TenantID: u.TenantID, Roles: []string{role}, Scopes: []string{"*"}}, nil
	case "legacy-jwt":
		uid, data, err := parseLegacyJwtToken(t, os.Getenv("TP_JWT_KEY"))
		if err != nil {
			return "", nil, err
		}
		if err := checkCommunitySession(ctx, os.Getenv("TP_COMMUNITY_SESSION_BASE_URL"), t, data); err != nil {
			return "", nil, err
		}
		return uid, data, nil
	case "dev":
		devModeWarnOnce.Do(func() {
			log.Println("[WARN] TP_AUTH_MODE=dev: no signature verification — do NOT use in production")
		})
		uid, data, err := parseDevToken(t)
		if err != nil {
			return "", nil, err
		}
		return uid, data, nil
	case "passthrough":
		passthroughModeWarnOnce.Do(func() {
			log.Println("[WARN] TP_AUTH_MODE=passthrough: every request granted tenant_admin — do NOT use in production")
		})
		return auth.UID(t), &Data{UserID: t, TenantID: "default", Roles: []string{"tenant_admin"}, Scopes: []string{"*"}}, nil
	default:
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "unsupported auth mode: " + mode}
	}
}

type legacyJwtClaims struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Authority string `json:"authority"`
	ExpiresAt int64  `json:"exp"`
}

// parseLegacyJwtToken verifies a ThingsPanel-issued HS256 JWT using TP_JWT_KEY
// and extracts the caller's identity. This is the production-safe auth path.
//
// Legacy JWT authentication also checks the Community session endpoint so
// logout, kick-out, and session expiry invalidate otherwise valid signatures.
func parseLegacyJwtToken(token, secretKey string) (auth.UID, *Data, error) {
	if secretKey == "" {
		return "", nil, &errs.Error{Code: errs.Internal, Message: "TP_JWT_KEY is not configured for legacy-jwt mode"}
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid token format"}
	}

	// Verify HS256 signature: HMAC-SHA256(header.payload, key)
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expectedSig := mac.Sum(nil)

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sigBytes, expectedSig) {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid token signature"}
	}

	// Verify algorithm claim in header
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid token header"}
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil || strings.ToUpper(header.Alg) != "HS256" {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "unsupported token algorithm, expected HS256"}
	}

	// Decode payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid token payload encoding"}
	}

	var claims legacyJwtClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid token claims"}
	}

	if claims.ExpiresAt > 0 && time.Now().Unix() > claims.ExpiresAt {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "token expired"}
	}

	if claims.ID == "" || claims.TenantID == "" {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "missing required claims: id or tenant_id"}
	}

	roles := []string{"TENANT_USER"}
	if claims.Authority != "" {
		roles = []string{claims.Authority}
	}

	data := &Data{
		UserID:   claims.ID,
		TenantID: claims.TenantID,
		Roles:    roles,
		Scopes:   []string{"*"},
	}
	return auth.UID(claims.ID), data, nil
}

// Dev token format:
//
//	user_id:tenant_id:role1,role2:scope1,scope2:device1|device2
//
// The trailing fields are optional.
func parseDevToken(token string) (auth.UID, *Data, error) {
	parts := strings.Split(token, ":")
	if len(parts) < 2 {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid dev token format"}
	}
	uid := strings.TrimSpace(parts[0])
	tenantID := strings.TrimSpace(parts[1])
	if uid == "" || tenantID == "" {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid dev token identity"}
	}

	roles := []string{"tenant_viewer"}
	scopes := []string{"device:view"}
	devices := []string{}

	if len(parts) >= 3 && strings.TrimSpace(parts[2]) != "" {
		roles = splitCSV(parts[2])
	}
	if len(parts) >= 4 && strings.TrimSpace(parts[3]) != "" {
		scopes = splitCSV(parts[3])
	}
	if len(parts) >= 5 && strings.TrimSpace(parts[4]) != "" {
		devices = splitBy(parts[4], "|")
	}

	data := &Data{
		UserID:              uid,
		TenantID:            tenantID,
		Roles:               roles,
		Scopes:              scopes,
		AuthorizedDeviceIDs: devices,
	}
	return auth.UID(uid), data, nil
}

func splitCSV(s string) []string { return splitBy(s, ",") }

func splitBy(s string, sep string) []string {
	in := strings.Split(s, sep)
	out := make([]string, 0, len(in))
	for _, it := range in {
		v := strings.TrimSpace(it)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// CurrentData returns auth data from Encore runtime.
// In plain Go unit tests (without Encore runtime), it returns a local admin principal
// so service unit tests can exercise business logic without runtime panics.
func CurrentData() *Data {
	var d *Data
	ok := false
	func() {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		if v, castOK := auth.Data().(*Data); castOK && v != nil {
			d = v
		}
		ok = true
	}()
	if ok && d != nil {
		return d
	}
	if flag.Lookup("test.v") != nil || strings.HasSuffix(os.Args[0], ".test") {
		return &Data{
			UserID:   "unit-test",
			TenantID: "test-tenant",
			Roles:    []string{"platform_admin"},
			Scopes:   []string{"*"},
		}
	}
	return nil
}
