package notification

import (
	"context"
	"encore.app/internal/authcore"
)

type nativeSessionChecker struct{}

func (nativeSessionChecker) CheckUserSession(ctx context.Context, raw string, expected UserPrincipal) error {
	runtime, e := authcore.Load()
	if e != nil {
		return e
	}
	u, e := runtime.Core.Check(ctx, raw)
	if e != nil {
		return e
	}
	if u.ID != expected.UserID || u.TenantID != expected.TenantID || !authorityMatches(expected.Roles, u.Authority) {
		return ErrLegacySessionRejected
	}
	return nil
}

func NewUserSessionChecker(endpoint string, options LegacySessionOptions) (UserSessionChecker, error) {
	if authcore.Enabled() {
		return nativeSessionChecker{}, nil
	}
	return NewLegacySessionChecker(endpoint, options)
}
