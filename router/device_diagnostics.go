package router

import (
	"github.com/gin-gonic/gin"
	"project/internal/api"
	"project/internal/middleware"
)

// Keep authentication attached to this registration even if its position in
// RouterInit changes. Device ownership is checked by the handler before Redis.
func registerDeviceDiagnosticsRoute(v1 *gin.RouterGroup) {
	v1.GET("/devices/:device_id/diagnostics", middleware.JWTAuth(), (&api.DeviceApi{}).GetDeviceDiagnostics)
}
