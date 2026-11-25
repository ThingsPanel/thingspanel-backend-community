package middleware

import (
	"context"
	"errors"
	"net/http"
	"os"

	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

// CommunitySessionStatus is the legacy JWT session verifier used by Encore.
func CommunitySessionStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	token := c.GetHeader("x-token")
	claims, err := utils.NewJWT([]byte(viper.GetString("jwt.key"))).ParseToken(token)
	if err != nil || claims.ID == "" || claims.Authority == "" || (claims.TenantID == "" && claims.Authority != "SYS_ADMIN") {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Code: ErrCodeInvalidToken, Message: "invalid or expired token"})
		return
	}

	active, err := communitySessionActive(c.Request.Context(), token, claims)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{Code: http.StatusServiceUnavailable, Message: "session verification unavailable"})
		return
	}
	if !active {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Code: ErrCodeTokenExpired, Message: "invalid or expired token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": gin.H{
		"id": claims.ID, "tenant_id": claims.TenantID, "authority": claims.Authority, "status": "N",
	}})
}

func communitySessionActive(ctx context.Context, token string, claims *utils.UserClaims) (bool, error) {
	if global.REDIS == nil {
		return false, errors.New("session store unavailable")
	}
	value, err := global.REDIS.Get(ctx, token).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil || value != "1" {
		return false, err
	}
	if os.Getenv("TP_AUTH_OWNER") == "encore" {
		return nativeAccountCurrent(ctx, claims)
	}
	if global.DB == nil {
		return false, errors.New("account database unavailable")
	}
	var count int64
	err = global.DB.WithContext(ctx).Table("users").
		Where("id = ? AND status = 'N' AND authority = ? AND COALESCE(tenant_id, '') = ?", claims.ID, claims.Authority, claims.TenantID).
		Count(&count).Error
	if err != nil || count != 1 {
		return false, err
	}
	return refreshJWTSession(ctx, token, claims.ExpiresAt)
}
