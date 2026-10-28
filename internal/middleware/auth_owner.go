package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
	"project/pkg/global"
	"project/pkg/utils"
)

// RetireAuthWriters is installed before all routes during the explicit cutover.
// It refuses direct access to old writers instead of proxying credentials.
func RetireAuthWriters() gin.HandlerFunc {
	return func(c *gin.Context) {
		if os.Getenv("TP_AUTH_OWNER") != "encore" {
			c.Next()
			return
		}
		switch c.Request.URL.Path {
		case "/api/v1/login", "/api/v1/user/refresh", "/api/v1/user/transform",
			"/api/v1/tenant/email/register", "/api/v1/tenant/super-admin/init", "/api/v1/tenant/market-register":
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "authentication route has moved"})
			return
		}
		c.Next()
	}
}

// This is the remaining Gin consumer's read-only compatibility check. The
// account schema is owned by this repository, not a separate service DB.
func NativeAccountCurrent(ctx context.Context, claims *utils.UserClaims) bool {
	if os.Getenv("TP_AUTH_OWNER") != "encore" {
		return true
	}
	var count int64
	if global.DB == nil {
		return false
	}
	e := global.DB.WithContext(ctx).Table("users").Where("id = ? AND status = 'N' AND auth_version = ? AND authority = ? AND COALESCE(tenant_id,'') = ?", claims.ID, claims.AuthVersion, claims.Authority, claims.TenantID).Count(&count).Error
	if e != nil || count != 1 {
		return false
	}
	if claims.Delegated {
		if claims.ActorID == "" {
			return false
		}
		e = global.DB.WithContext(ctx).Table("users").Where("id=? AND status='N' AND auth_version=? AND (authority='SYS_ADMIN' OR (authority='TENANT_ADMIN' AND tenant_id=? AND ? <> 'SYS_ADMIN'))", claims.ActorID, claims.ActorVersion, claims.TenantID, claims.Authority).Count(&count).Error
	}
	return e == nil && count == 1
}
