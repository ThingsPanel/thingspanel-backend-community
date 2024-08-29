package middleware

import (
	"net/http"
	"project/internal/errors"

	"github.com/gin-gonic/gin"
)

func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) > 0 {
			err := c.Errors.Last().Err
			if e, ok := err.(*errors.ErrorCode); ok {
				c.JSON(e.HTTPStatus, gin.H{
					"code":    e.Code,
					"message": e.Message,
				})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    errors.ErrSystemInternal,
					"message": "内部系统错误",
				})
			}
		}
	}
}
