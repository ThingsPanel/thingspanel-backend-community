package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/mail"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/utils"
	"strings"
)

type platformIdentity struct {
	ID       string  `json:"id"`
	Email    string  `json:"email"`
	Name     *string `json:"name"`
	TenantID string  `json:"tenantId"`
	Role     string  `json:"role"`
}

func currentPlatformIdentity(claims *utils.UserClaims, user *model.User) (platformIdentity, bool) {
	if claims == nil || claims.Delegated || user == nil || claims.ID == "" || user.ID != claims.ID || user.Authority == nil || *user.Authority != claims.Authority || user.Status == nil || *user.Status != "N" {
		return platformIdentity{}, false
	}
	if *user.Authority != "SYS_ADMIN" && (user.TenantID == nil || *user.TenantID == "" || *user.TenantID != claims.TenantID) {
		return platformIdentity{}, false
	}
	email, err := mail.ParseAddress(user.Email)
	if err != nil || email.Address != user.Email {
		return platformIdentity{}, false
	}
	role := map[string]string{"SYS_ADMIN": "SUPER_ADMIN", "TENANT_ADMIN": "TENANT_ADMIN", "TENANT_USER": "VIEWER"}[*user.Authority]
	if role == "" {
		return platformIdentity{}, false
	}
	space := "thingspanel-sys-admin"
	if role != "SUPER_ADMIN" {
		space = *user.TenantID
	}
	return platformIdentity{user.ID, user.Email, user.Name, space, role}, true
}

// PlatformSSOIdentity is mounted behind JWTAuth, outside request-body logging.
// Only a current interactive session may assert identity; never an API key.
func PlatformSSOIdentity(c *gin.Context) {
	if len(c.Request.Header.Values("x-token")) != 1 || strings.TrimSpace(c.GetHeader("x-token")) == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "session unavailable"})
		return
	}
	v, _ := c.Get("claims")
	claims, ok := v.(*utils.UserClaims)
	if !ok || claims == nil {
		c.AbortWithStatusJSON(401, gin.H{"code": 401, "message": "session unavailable"})
		return
	}
	user, err := dal.GetUsersById(claims.ID)
	identity, valid := currentPlatformIdentity(claims, user)
	if err != nil || !valid {
		c.AbortWithStatusJSON(401, gin.H{"code": 401, "message": "session unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"code": 200, "data": identity})
}
