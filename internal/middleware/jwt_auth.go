package middleware

import (
	"context"
	"net/http"
	"time"

	"project/internal/dal"
	"project/pkg/global"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// 错误码常量
const (
	ErrCodeNoAuth         = 40100 // 缺少认证信息
	ErrCodeInvalidToken   = 40101 // 无效的Token
	ErrCodeTokenExpired   = 40102 // Token已过期
	ErrCodeInvalidAPIKey  = 40103 // 无效的APIKey
	ErrCodeAPIKeyDisabled = 40104 // APIKey已禁用
)

// 统一的错误响应结构
type ErrorResponse struct {
	Code      int    `json:"code"`                 // 错误码
	Message   string `json:"message"`              // 错误描述
	RequestID string `json:"request_id,omitempty"` // 请求ID，方便追踪
}

// JWTAuth 中间件，检查token和APIKey
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {

		// 1. 优先检查 JWT token
		token := c.Request.Header.Get("x-token")
		if token != "" {
			// JWT Token 存在，验证 JWT
			if isValidJWT(c, token) {
				c.Next()
				return
			}
			// JWT 验证失败，继续尝试 APIKey
		}

		// 2. 尝试 APIKey 验证
		if !OpenAPIKeyAuth(c) {
			// APIKey 验证也失败，在 OpenAPIKeyAuth 中已经设置了错误响应
			return
		}

		// APIKey 验证成功
		c.Next()
	}
}

// isValidJWT 验证JWT token
func isValidJWT(c *gin.Context, token string) bool {
	requestID := c.GetString("X-Request-ID")

	// 验证 Redis 中的 token
	if global.REDIS.Get(context.Background(), token).Val() != "1" {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Code:      ErrCodeTokenExpired,
			Message:   "token has expired",
			RequestID: requestID,
		})
		c.Abort()
		return false
	}

	// 刷新 token 过期时间
	timeout := viper.GetInt("session.timeout")
	global.REDIS.Set(context.Background(), token, "1", time.Duration(timeout)*time.Minute)

	// 验证 JWT
	key := viper.GetString("jwt.key")
	j := utils.NewJWT([]byte(key))
	claims, err := j.ParseToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Code:      ErrCodeInvalidToken,
			Message:   "invalid token format",
			RequestID: requestID,
		})
		c.Abort()
		return false
	}

	// 设置 claims 到上下文
	c.Set("claims", claims)
	return true
}

// OpenAPIKeyAuth APIKey 验证
func OpenAPIKeyAuth(c *gin.Context) bool {
	requestID := c.GetString("X-Request-ID")

	appKey := c.Request.Header.Get("x-api-key")
	if appKey == "" {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Code:      ErrCodeNoAuth,
			Message:   "missing authentication (x-token or x-api-key required)",
			RequestID: requestID,
		})
		c.Abort()
		return false
	}

	tenantID, err := dal.VerifyOpenAPIKey(context.Background(), appKey)
	if err != nil {
		errCode := ErrCodeInvalidAPIKey
		errMsg := "api key verification failed"

		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Code:      errCode,
			Message:   errMsg,
			RequestID: requestID,
		})
		c.Abort()
		return false
	}

	// 设置 claims 到上下文
	claims := utils.UserClaims{
		TenantID:  tenantID,
		Authority: "TENANT_ADMIN",
	}
	c.Set("claims", &claims)
	return true
}
