package middleware

import (
	"net/http"
	"strings"

	service "project/internal/service"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

// 采用casbin，如果资源在表中，就需要校验，不在表中不做校验
// RBAC：用户-角色-功能-资源-动作

func CasbinRBAC() gin.HandlerFunc {
	return func(c *gin.Context) {
		//需要验证的url:*api*
		if strings.Contains(c.Request.URL.Path, "api") {
			url := strings.TrimLeft(c.Request.URL.Path, "/")
			// 判断接口是否需要校验
			isVerify := service.GroupApp.Casbin.GetUrl(url)
			if isVerify {
				userClaims := c.MustGet("claims").(*utils.UserClaims)
				isSuccess := service.GroupApp.Casbin.Verify(userClaims.ID, url)
				if !isSuccess {
					c.JSON(http.StatusBadRequest, gin.H{"error": "非法访问"})
					c.Abort()
					return
				}
			}
		}
	}
}
