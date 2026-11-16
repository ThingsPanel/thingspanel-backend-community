package api

import (
	"github.com/gin-gonic/gin"
	"project/internal/service"
	"project/pkg/utils"
)

func (*DeviceApi) ConnectionTopics(c *gin.Context) {
	v, _ := c.Get("claims")
	claims, _ := v.(*utils.UserClaims)
	data, err := service.GroupApp.Device.ConnectionTopics(c.Request.Context(), claims, c.Param("id"))
	c.Header("Cache-Control", "no-store")
	if err != nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "connection_topics_unavailable"})
		return
	}
	c.Set("data", data)
}
