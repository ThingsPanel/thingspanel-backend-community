package middleware

import (
	"project/pkg/metrics"
	"time"

	"github.com/gin-gonic/gin"
)

// MetricsMiddleware 创建监控中间件
func MetricsMiddleware(m *metrics.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath() // 获取路由路径而不是实际URL

		// 记录请求
		m.RecordAPIRequest(path, c.Request.Method)

		// 使用 defer 确保在请求结束时记录指标
		defer func() {
			// 记录响应时间
			duration := time.Since(start).Seconds()
			m.RecordAPILatency(path, duration)

			// 记录响应大小
			m.RecordResponseSize(path, float64(c.Writer.Size()))

			// 处理 panic
			if err := recover(); err != nil {
				m.RecordAPIError("panic")
				m.RecordCriticalError()
				panic(err) // 重新抛出panic
			}
		}()

		c.Next()

		// 记录错误
		if len(c.Errors) > 0 {
			for _, e := range c.Errors {
				if e.IsType(gin.ErrorTypePrivate) {
					m.RecordAPIError("system")
				} else {
					m.RecordAPIError("business")
				}
			}
		}
	}
}
