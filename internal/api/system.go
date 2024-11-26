package api

import (
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type SystemApi struct{}

func (*SystemApi) GetSystime(c *gin.Context) {
	SuccessHandler(c, "success", map[string]interface{}{"systime": utils.GetSecondTimestamp()})
}

// 健康检查
func (*SystemApi) HealthCheck(c *gin.Context) {
	SuccessOK(c)
}
