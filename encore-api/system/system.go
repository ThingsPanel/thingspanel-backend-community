// Package system is the platform's system-management API (control plane).
//
// It only reports state; it never restarts, backs up or upgrades anything
// itself. Those actions belong to the host-side manager (tp-agent system role).
package system

import (
	"context"
	"os"
	"strings"
	"time"

	"encore.app/l1-access/authn"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
)

var startedAt = time.Now().UTC()

// InfoResponse describes this deployment. It carries no secrets.
type InfoResponse struct {
	PlatformVersion string    `json:"platform_version"`
	StartedAt       time.Time `json:"started_at"`
}

// Info returns the platform version. Super administrators only.
//
//encore:api auth method=GET path=/api/system/info
func Info(ctx context.Context) (*InfoResponse, error) {
	data, _ := auth.Data().(*authn.Data)
	if !isSystemAdmin(data) {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "super administrator required"}
	}
	return &InfoResponse{PlatformVersion: platformVersion(), StartedAt: startedAt}, nil
}

func platformVersion() string {
	if v := strings.TrimSpace(os.Getenv("TP_PLATFORM_VERSION")); v != "" {
		return v
	}
	return "dev"
}

func isSystemAdmin(data *authn.Data) bool {
	if data == nil {
		return false
	}
	for _, role := range data.Roles {
		if strings.EqualFold(strings.TrimSpace(role), "SYS_ADMIN") {
			return true
		}
	}
	return false
}
