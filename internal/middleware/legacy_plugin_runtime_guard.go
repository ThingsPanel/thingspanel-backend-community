package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RejectUnboundPluginRuntime closes the legacy anonymous plugin runtime ports
// until their replacement verifies a current managed instance and durable
// business binding. A user's JWT, a service identifier, or an instance ID in a
// request is not that authorization. There is deliberately no environment or
// header switch that turns this guard into an allow path.
//
// This is an explicit unavailable capability, not instance authorization or a
// completed retirement. Keep the underlying owner implementation until the
// managed provider and consumer have passed the common cutover gates.
func RejectUnboundPluginRuntime() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"code":    http.StatusServiceUnavailable,
			"message": "managed plugin instance authorization is not available",
			"data": gin.H{
				"state":           "blocked",
				"errorCode":       "PLUGIN_RUNTIME_IDENTITY_REQUIRED",
				"automatic_retry": false,
			},
		})
	}
}
