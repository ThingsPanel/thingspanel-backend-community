package api

import (
	"net/http"
	"strings"

	"project/internal/dal"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

// NotificationIdentityApi exposes the minimal identity context needed by the
// Encore notification service to re-check the original JWT against Redis.
// Routing must apply middleware.JWTAuth and must omit OperationLogs.
type NotificationIdentityApi struct{}

// SessionContext returns only claims already verified by JWTAuth. Requiring
// x-token explicitly prevents the middleware's API-key alternative from being
// used as a user-session assertion.
func (*NotificationIdentityApi) SessionContext(c *gin.Context) {
	if strings.TrimSpace(c.GetHeader("x-token")) == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	value, ok := c.Get("claims")
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	claims, ok := value.(*utils.UserClaims)
	if !ok || claims == nil || strings.TrimSpace(claims.ID) == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	authority := strings.ToUpper(strings.TrimSpace(claims.Authority))
	platformActor := authority == "SYS_ADMIN"
	if (strings.TrimSpace(claims.TenantID) == "" && !platformActor) || (authority != "SYS_ADMIN" && authority != "TENANT_ADMIN" && authority != "TENANT_USER") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if global.REDIS == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	session, err := global.REDIS.Get(c.Request.Context(), strings.TrimSpace(c.GetHeader("x-token"))).Result()
	if err != nil || session != "1" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	user, err := dal.GetUsersById(claims.ID)
	databaseTenant := ""
	if user != nil && user.TenantID != nil {
		databaseTenant = strings.TrimSpace(*user.TenantID)
	}
	tenantMatches := databaseTenant == strings.TrimSpace(claims.TenantID) && (databaseTenant != "" || platformActor)
	if err != nil || user == nil || user.Status == nil || *user.Status != "N" || !tenantMatches || user.Authority == nil || !strings.EqualFold(strings.TrimSpace(*user.Authority), authority) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"userId":    claims.ID,
		"tenantId":  claims.TenantID,
		"authority": authority,
	})
}
