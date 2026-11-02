package api

import (
	"strings"

	"project/pkg/errcode"

	"github.com/gin-gonic/gin"
)

// The platform credential and the ThingsVis public-session credential are
// independent. Missing ThingsVis authorization must not select the client's
// internal shared-token fallback.
func dashboardExportAuthorization(c *gin.Context) (string, error) {
	authorization := strings.TrimSpace(c.GetHeader("X-ThingsVis-Authorization"))
	if authorization == "" {
		// Preserve the existing explicit two-header client contract: the
		// platform authenticates x-token, while Authorization holds ThingsVis.
		// The browser's ordinary platform Authorization mirror is not that
		// explicit credential and must not accidentally enter this path.
		platformToken := strings.TrimSpace(c.GetHeader("x-token"))
		legacy := strings.TrimSpace(c.GetHeader("Authorization"))
		if platformToken != "" && legacy != "Bearer "+platformToken {
			authorization = legacy
		}
	}
	parts := strings.Fields(authorization)
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return "", errcode.WithData(errcode.CodeOpDenied, "an explicit ThingsVis user credential is required for dashboard analysis and export")
	}
	return "Bearer " + parts[1], nil
}
