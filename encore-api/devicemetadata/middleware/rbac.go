package middleware

import (
	"strings"
)

// Permission represents a resource:action permission
type Permission struct {
	Resource string
	Action   string
}

// ParsePermission parses "resource:action" format
func ParsePermission(perm string) Permission {
	parts := strings.SplitN(perm, ":", 2)
	if len(parts) == 2 {
		return Permission{Resource: parts[0], Action: parts[1]}
	}
	return Permission{Resource: parts[0]}
}

// Role represents a user role with associated permissions
type Role string

const (
	RoleViewer    Role = "viewer"
	RoleEditor    Role = "editor"
	RolePublisher Role = "publisher"
	RoleAdmin     Role = "admin"
)

// PermissionMatrix defines role to permissions mapping
var PermissionMatrix = map[Role][]string{
	RoleViewer: {
		"thing_model:read",
		"device:read",
		"product:read",
	},
	RoleEditor: {
		"thing_model:read",
		"thing_model:write",
		"device:read",
		"device:write",
		"product:read",
		"product:write",
	},
	RolePublisher: {
		"thing_model:read",
		"thing_model:write",
		"thing_model:publish",
		"device:read",
		"device:write",
		"product:read",
		"product:write",
		"product:publish",
	},
	RoleAdmin: {
		"*", // Admin has all permissions
	},
}

// HasPermission checks if a role has a specific permission
func HasPermission(role Role, requiredPerm string) bool {
	perms, ok := PermissionMatrix[role]
	if !ok {
		return false
	}

	for _, perm := range perms {
		if perm == "*" {
			return true
		}
		if perm == requiredPerm {
			return true
		}
	}
	return false
}

// HasAnyPermission checks if any of the user's roles have the required permission
func HasAnyPermission(roles []string, requiredPerm string) bool {
	if requiredPerm == "" {
		return true // No permission required
	}

	for _, roleStr := range roles {
		role := Role(roleStr)
		if HasPermission(role, requiredPerm) {
			return true
		}
	}
	return false
}

// IsValidRole checks if a role string is valid
func IsValidRole(roleStr string) bool {
	role := Role(roleStr)
	_, ok := PermissionMatrix[role]
	return ok
}
