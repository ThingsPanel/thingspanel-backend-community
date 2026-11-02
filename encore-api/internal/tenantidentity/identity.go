// Package tenantidentity shares the existing verified Community tenant mapping.
// The historical non-UUID mapping is preserved for compatibility; collision-safe
// migration remains a separate release gate, not a new identity scheme here.
package tenantidentity

import (
	"encoding/hex"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"fmt"
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
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return uuid.Nil.String()
	}
	if parsed, err := uuid.Parse(trimmed); err == nil {
		return parsed.String()
	}

	hexOnly := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9':
			return r
		case r >= 'a' && r <= 'f':
			return r
		case r >= 'A' && r <= 'F':
			return r + ('a' - 'A')
		default:
			return -1
		}
	}, trimmed)

	if len(hexOnly) > 0 {
		return formatHexAsUUID(padOrTrimHex(hexOnly))
	}

	return formatHexAsUUID(padOrTrimHex(hex.EncodeToString([]byte(trimmed))))
}

func padOrTrimHex(value string) string {
	if len(value) >= 32 {
		return value[:32]
	}
	return value + strings.Repeat("0", 32-len(value))
}

func formatHexAsUUID(value string) string {
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		value[0:8],
		value[8:12],
		value[12:16],
		value[16:20],
		value[20:32],
	)
}
