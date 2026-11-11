// Package tenantidentity shares the established ThingModel identity namespace
// across native owners. Existing padded/truncated identities require an offline
// ownership migration; they must never be tried as a fallback during a request.
package tenantidentity

import (
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"github.com/google/uuid"
	"strings"
)

func RequiredTenantID(data *authn.Data) (string, error) {
	if data == nil {
		return "", &errs.Error{Code: errs.Unauthenticated, Message: "missing tenant context"}
	}
	isTenantAdmin := false
	for _, role := range data.Roles {
		switch strings.ToUpper(strings.TrimSpace(role)) {
		case "SYS_ADMIN":
			return "", &errs.Error{Code: errs.PermissionDenied, Message: "platform admin requires an audited tenant delegation"}
		case "TENANT_ADMIN":
			isTenantAdmin = true
		}
	}
	if !isTenantAdmin {
		return "", &errs.Error{Code: errs.PermissionDenied, Message: "tenant admin role required"}
	}
	if strings.TrimSpace(data.TenantID) == "" {
		return "", &errs.Error{Code: errs.Unauthenticated, Message: "missing tenant context"}
	}
	return strings.TrimSpace(data.TenantID), nil
}

func NormalizePrincipalID(raw string) string {
	return CanonicalIdentity("tenant", raw)
}

// CanonicalIdentity retains the whole source identity and separates tenant and
// subject namespaces. This is the same mapping already used by ThingModel v2.
// Callers must validate the authenticated identity before resolving it.
func CanonicalIdentity(kind, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil.String()
	}
	if parsed, err := uuid.Parse(raw); err == nil {
		return parsed.String()
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("thingspanel/community/"+kind+"/"+raw)).String()
}
