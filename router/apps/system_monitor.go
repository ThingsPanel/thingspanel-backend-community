package apps

import (
	"project/internal/api"
	"project/pkg/metrics"

	"github.com/gin-gonic/gin"
)

// SystemMonitor 系统监控模块
type SystemMonitor struct{}

// InitSystemMonitor 初始化系统监控相关路由
func (m *SystemMonitor) InitSystemMonitor(r *gin.RouterGroup, metricsManager *metrics.Metrics) {
	// 注册路由
	r.GET("system/metrics/current", api.Controllers.SystemMonitorApi.GetCurrentSystemMetrics)
	r.GET("system/metrics/history", api.Controllers.SystemMonitorApi.GetHistorySystemMetrics)
}
