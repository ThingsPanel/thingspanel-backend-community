package middleware

import (
	"fmt"

	"project/pkg/logredact"

	"github.com/gin-gonic/gin"
)

// AccessLogger is gin's default request logger with the query string redacted,
// so GET endpoints that carry voucher/token/password/secret in the URL do not
// write them into the access log.
func AccessLogger() gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{Formatter: func(p gin.LogFormatterParams) string {
		p.Path = logredact.URL(p.Path)
		return fmt.Sprintf("[GIN] %v | %3d | %13v | %15s | %-7s %#v\n%s",
			p.TimeStamp.Format("2006/01/02 - 15:04:05"), p.StatusCode, p.Latency,
			p.ClientIP, p.Method, p.Path, logredact.Text(p.ErrorMessage))
	}})
}
