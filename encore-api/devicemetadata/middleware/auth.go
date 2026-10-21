package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type contextKey string

const (
	tenantIDKey contextKey = "tenant_id"
	userIDKey   contextKey = "user_id"
	rolesKey    contextKey = "roles"
	actorIDKey  contextKey = "actor_id"
	requestIDKey contextKey = "request_id"
)

// Claims represents the JWT claims structure
type Claims struct {
	TenantID string   `json:"tenant_id"`
	UserID   string   `json:"user_id"`
	Roles    []string `json:"roles"`
	Subject  string   `json:"sub,omitempty"`
	Issuer   string   `json:"iss,omitempty"`
	IssuedAt int64    `json:"iat,omitempty"`
}

// ParseJWT extracts and parses the JWT from the Authorization header
func ParseJWT(authHeader string) (*Claims, error) {
	if authHeader == "" {
		return nil, errors.New("missing authorization header")
	}

	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return nil, errors.New("invalid authorization header format")
	}

	tokenString := strings.TrimPrefix(authHeader, bearerPrefix)

	// JWT format: header.payload.signature
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT format")
	}

	// Decode payload (second part)
	payload := parts[1]
	// Add padding if needed
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid JWT payload encoding: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("invalid JWT claims: %w", err)
	}

	if claims.TenantID == "" {
		return nil, errors.New("missing tenant_id in JWT claims")
	}

	if claims.UserID == "" {
		return nil, errors.New("missing user_id in JWT claims")
	}

	return &claims, nil
}

// WithTenantID injects tenant_id into context
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// WithUserID injects user_id into context
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// WithRoles injects roles into context
func WithRoles(ctx context.Context, roles []string) context.Context {
	return context.WithValue(ctx, rolesKey, roles)
}

// WithActorID injects actor_id into context
func WithActorID(ctx context.Context, actorID string) context.Context {
	return context.WithValue(ctx, actorIDKey, actorID)
}

// TenantIDFromCtx extracts tenant_id from context
func TenantIDFromCtx(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(tenantIDKey).(string)
	return tenantID, ok
}

// UserIDFromCtx extracts user_id from context
func UserIDFromCtx(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey).(string)
	return userID, ok
}

// RolesFromCtx extracts roles from context
func RolesFromCtx(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(rolesKey).([]string)
	return roles, ok
}

// ActorIDFromCtx extracts actor_id from context
func ActorIDFromCtx(ctx context.Context) (string, bool) {
	actorID, ok := ctx.Value(actorIDKey).(string)
	return actorID, ok
}

// MustTenantID extracts tenant_id or panics if missing
func MustTenantID(ctx context.Context) string {
	tenantID, ok := TenantIDFromCtx(ctx)
	if !ok {
		panic("tenant_id not found in context")
	}
	return tenantID
}

// MustUserID extracts user_id or panics if missing
func MustUserID(ctx context.Context) string {
	userID, ok := UserIDFromCtx(ctx)
	if !ok {
		panic("user_id not found in context")
	}
	return userID
}

// DecodeTokenClaims decodes a JSON-encoded claims string
func DecodeTokenClaims(claimsJSON string) (*Claims, error) {
	var claims Claims
	if err := json.Unmarshal([]byte(claimsJSON), &claims); err != nil {
		return nil, fmt.Errorf("failed to decode token claims: %w", err)
	}
	return &claims, nil
}
