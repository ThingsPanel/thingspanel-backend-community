package middleware

import (
	"context"
)

// ContextHelpers provides convenient access to context values
type ContextHelpers interface {
	TenantID(ctx context.Context) (string, bool)
	UserID(ctx context.Context) (string, bool)
	Roles(ctx context.Context) ([]string, bool)
	ActorID(ctx context.Context) (string, bool)
	Locale(ctx context.Context) string
	RequestID(ctx context.Context) (string, bool)

	MustTenantID(ctx context.Context) string
	MustUserID(ctx context.Context) string
}

// DefaultContextHelpers provides the default implementation
type DefaultContextHelpers struct{}

// TenantID extracts tenant_id from context
func (h *DefaultContextHelpers) TenantID(ctx context.Context) (string, bool) {
	return TenantIDFromCtx(ctx)
}

// UserID extracts user_id from context
func (h *DefaultContextHelpers) UserID(ctx context.Context) (string, bool) {
	return UserIDFromCtx(ctx)
}

// Roles extracts roles from context
func (h *DefaultContextHelpers) Roles(ctx context.Context) ([]string, bool) {
	return RolesFromCtx(ctx)
}

// ActorID extracts actor_id from context
func (h *DefaultContextHelpers) ActorID(ctx context.Context) (string, bool) {
	return ActorIDFromCtx(ctx)
}

// Locale extracts locale from context with fallback
func (h *DefaultContextHelpers) Locale(ctx context.Context) string {
	return LocaleFromCtx(ctx)
}

// RequestID extracts request_id from context
func (h *DefaultContextHelpers) RequestID(ctx context.Context) (string, bool) {
	return RequestIDFromCtx(ctx)
}

// MustTenantID extracts tenant_id or panics
func (h *DefaultContextHelpers) MustTenantID(ctx context.Context) string {
	return MustTenantID(ctx)
}

// MustUserID extracts user_id or panics
func (h *DefaultContextHelpers) MustUserID(ctx context.Context) string {
	return MustUserID(ctx)
}

// NewContextHelpers returns a new instance of DefaultContextHelpers
func NewContextHelpers() ContextHelpers {
	return &DefaultContextHelpers{}
}

// ContextValue represents a typed context value
type ContextValue struct {
	TenantID  string
	UserID    string
	Roles     []string
	ActorID   string
	Locale    string
	RequestID string
}

// FromContext extracts all context values into a single struct
func FromContext(ctx context.Context) ContextValue {
	tenantID, _ := TenantIDFromCtx(ctx)
	userID, _ := UserIDFromCtx(ctx)
	roles, _ := RolesFromCtx(ctx)
	actorID, _ := ActorIDFromCtx(ctx)
	requestID, _ := RequestIDFromCtx(ctx)

	return ContextValue{
		TenantID:  tenantID,
		UserID:    userID,
		Roles:     roles,
		ActorID:   actorID,
		Locale:    LocaleFromCtx(ctx),
		RequestID: requestID,
	}
}
