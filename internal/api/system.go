package api

import (
	"context"
	"net/http"
	"time"

	"project/internal/health"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type SystemApi struct{}

// /api/v1/systime
func (*SystemApi) HandleSystime(c *gin.Context) {
	c.Set("data", map[string]interface{}{"systime": utils.GetSecondTimestamp()})
}

const healthProbeTimeout = 2 * time.Second

// healthProbes builds the readiness probes from the process globals. The
// schema ledger (sys_version) exists only on PostgreSQL, so it is checked there.
func healthProbes() []health.Probe {
	probes := []health.Probe{
		{Name: "database", Run: func(ctx context.Context) (bool, string) {
			if global.DB == nil {
				return false, health.ReasonNotConfigured
			}
			sqlDB, err := global.DB.DB()
			if err != nil || sqlDB.PingContext(ctx) != nil {
				return false, health.ReasonUnreachable
			}
			return true, ""
		}},
		{Name: "redis", Run: func(ctx context.Context) (bool, string) {
			if global.REDIS == nil {
				return false, health.ReasonNotConfigured
			}
			if global.REDIS.Ping(ctx).Err() != nil {
				return false, health.ReasonUnreachable
			}
			return true, ""
		}},
	}
	if global.DB != nil && global.DB.Dialector != nil && global.DB.Dialector.Name() == "postgres" {
		probes = append(probes, health.SchemaProbe(func(ctx context.Context) (int, error) {
			var n int
			err := global.DB.WithContext(ctx).Raw("SELECT version_number FROM sys_version ORDER BY version_number DESC LIMIT 1").Scan(&n).Error
			return n, err
		}, global.VERSION_NUMBER))
	}
	return probes
}

// 健康检查 /health
//
// Ready only when the database answers, Redis answers and (PostgreSQL) the
// schema ledger equals the version this program was built for. A failing
// check returns HTTP 503 with the component names, so the host-side manager can
// say which one is down. No error text or address is exposed.
func (*SystemApi) HealthCheck(c *gin.Context) {
	report := health.Run(c.Request.Context(), healthProbeTimeout, global.VERSION_NUMBER, healthProbes()...)
	if report.Ready {
		c.Set("data", report)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "not ready", "data": report})
}

// 获取系统版本 /api/v1/sys_version
func (*SystemApi) HandleSysVersion(c *gin.Context) {
	c.Set("data", map[string]interface{}{"version": global.SystemVersion()})
}
