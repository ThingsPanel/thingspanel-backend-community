package api

import (
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
