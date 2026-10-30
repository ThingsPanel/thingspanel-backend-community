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

	"encore.dev"
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
	meta := encore.Meta()
	var environmentType encore.EnvironmentType
	var cloud encore.CloudProvider
	if meta != nil {
		environmentType = meta.Environment.Type
		cloud = meta.Environment.Cloud
	}
	if err := validateAuthModeEnvironment(mode, environmentType, cloud); err != nil {
		return "", nil, err
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

// validateAuthModeEnvironment keeps the unauthenticated modes off shared
// infrastructure. `encore run` reports type "development" with cloud "local",
// while an Encore Cloud development environment runs on a real cloud provider,
// so the local cloud (never production) counts as a local machine.
func validateAuthModeEnvironment(mode string, environmentType encore.EnvironmentType, cloud encore.CloudProvider) error {
	if mode != "dev" && mode != "passthrough" {
		return nil
	}
	if environmentType == encore.EnvLocal || environmentType == encore.EnvTest {
		return nil
	}
	if cloud == encore.CloudLocal && environmentType == encore.EnvDevelopment {
		return nil
	}
	return &errs.Error{Code: errs.Internal, Message: "insecure auth mode is only available in local or test environments"}
}

type legacyJwtClaims struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	Authority string          `json:"authority"`
	ExpiresAt int64           `json:"exp"`
	NotBefore json.RawMessage `json:"nbf"`
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
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Alg != "HS256" {
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

	if claims.ExpiresAt <= 0 {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "missing or invalid token expiration"}
	}
	if time.Now().Unix() >= claims.ExpiresAt {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "token expired"}
	}
	if len(claims.NotBefore) > 0 {
		var notBefore int64
		if strings.TrimSpace(string(claims.NotBefore)) == "null" || json.Unmarshal(claims.NotBefore, &notBefore) != nil || notBefore < 0 {
			return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid token not-before claim"}
		}
		if time.Now().Unix() < notBefore {
			return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "token not yet valid"}
		}
	}

	if strings.TrimSpace(claims.ID) == "" || strings.TrimSpace(claims.ID) != claims.ID {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "missing or invalid user identity"}
	}
	if strings.TrimSpace(claims.TenantID) != claims.TenantID {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid tenant identity"}
	}
	authority := strings.TrimSpace(claims.Authority)
	if authority != "SYS_ADMIN" && authority != "TENANT_ADMIN" {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "unsupported user authority"}
	}
	if claims.TenantID == "" && authority != "SYS_ADMIN" {
		return "", nil, &errs.Error{Code: errs.Unauthenticated, Message: "missing tenant identity"}
	}

	data := &Data{
		UserID:   claims.ID,
		TenantID: claims.TenantID,
		Roles:    []string{authority},
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
			Roles:    []string{"TENANT_ADMIN"},
		}
	}
	return nil
}
