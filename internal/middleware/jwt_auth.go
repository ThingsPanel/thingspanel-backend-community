package middleware

import (
	"net/http"
	"time"

	"project/global"
	utils "project/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// jwt鉴权取头部信息 x-token 登录时回返回token信息 这里前端需要把token存储到cookie或者本地localStorage中 不过需要跟后端协商过期时间 可以约定刷新令牌或者重新登录
		token := c.Request.Header.Get("x-token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录或非法访问"})
			c.Abort()
			return
		}

		if global.REDIS.Get(token).Val() != "1" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录或非法访问"})
			c.Abort()
			return
		} else {
			timeout := viper.GetInt("session.timeout")
			// 刷新时间
			global.REDIS.Set(token, "1", time.Duration(timeout)*time.Minute)
		}

		// 获取key
		key := viper.GetString("jwt.key")
		j := utils.NewJWT([]byte(key))
		// parseToken 解析token包含的信息
		claims, err := j.ParseToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}

		c.Set("claims", claims)
		c.Next()
	}
}
