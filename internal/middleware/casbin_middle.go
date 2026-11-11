package middleware

import (
	"net/http"
	"strings"

	service "project/internal/service"
	"project/pkg/errcode"
	"project/pkg/global"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

// 采用casbin，如果资源在表中，就需要校验，不在表中不做校验
// RBAC：用户-角色-功能-资源-动作

func CasbinRBAC() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Receipt keys are dynamic path values. Authorize every query against
		// the existing access-creation resource, never the unregistered key URL.
		// This narrow new capability is fail-closed without changing legacy
		// routes whose historical policy is to skip unregistered resources.
		if c.Request.Method == http.MethodGet && c.FullPath() == "/api/v1/service/access/operations/:key" {
			const resource = "api/v1/service/access"
			claimsValue, _ := c.Get("claims")
			claims, _ := claimsValue.(*utils.UserClaims)
			if global.CasbinEnforcer == nil || claims == nil || strings.TrimSpace(claims.ID) == "" || strings.TrimSpace(claims.TenantID) == "" || !service.GroupApp.Casbin.GetUrl(resource) || !service.GroupApp.Casbin.Verify(claims.ID, resource) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "非法访问", "code": errcode.CodeNoPermission})
				c.Abort()
			}
			return
		}
		//需要验证的url:*api*
		if strings.Contains(c.Request.URL.Path, "api") {
			url := strings.TrimLeft(c.Request.URL.Path, "/")
			// 判断接口是否需要校验
			isVerify := service.GroupApp.Casbin.GetUrl(url)
			if isVerify {
				userClaims := c.MustGet("claims").(*utils.UserClaims)
				subject := userClaims.ID
				// This endpoint is governed by the signed authority claim. The
				// legacy userID->custom-roleID mapping cannot express a platform
				// tenant-admin role consistently across tenants.
				if url == "api/v1/notification-default-policy" {
					subject = strings.ToUpper(strings.TrimSpace(userClaims.Authority))
				}
				isSuccess := service.GroupApp.Casbin.Verify(subject, url)
				if !isSuccess {
					c.JSON(http.StatusBadRequest, gin.H{"error": "非法访问"})
					c.Abort()
					return
				}
			}
		}
	}
}
