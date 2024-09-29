package api

import (
	"project/utils"

	"github.com/gin-gonic/gin"
)

type SystemApi struct{}

func (s *SystemApi) GetSystime(c *gin.Context) {
	SuccessHandler(c, "success", map[string]interface{}{"systime": utils.GetSecondTimestamp()})
}

// 健康检查
func (s *SystemApi) HealthCheck(c *gin.Context) {
	SuccessOK(c)
}
