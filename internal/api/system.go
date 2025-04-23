package api

import (
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type SystemApi struct{}

// /api/v1/systime
func (*SystemApi) HandleSystime(c *gin.Context) {
	c.Set("data", map[string]interface{}{"systime": utils.GetSecondTimestamp()})
}

// 健康检查 /health
func (*SystemApi) HealthCheck(c *gin.Context) {
	c.Set("data", nil)
}

// 获取系统版本 /api/v1/sys_version
func (*SystemApi) HandleSysVersion(c *gin.Context) {
	c.Set("data", map[string]interface{}{"version": global.SYSTEM_VERSION})
}
