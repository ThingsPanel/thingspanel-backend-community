package sseapi

import (
	"fmt"
	"net/http"
	"project/internal/api"
	"project/pkg/global"
	"project/pkg/utils"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type SSEApi struct{}

// api/v1/events

func (*SSEApi) HandleSystemEvents(c *gin.Context) {
	userClaims, ok := c.MustGet("claims").(*utils.UserClaims)
	if !ok {
		api.ErrorHandler(c, http.StatusUnauthorized, fmt.Errorf("unauthorized"))
		return
	}

	logrus.WithFields(logrus.Fields{
		"tenantID":  userClaims.TenantID,
		"userEmail": userClaims.Email,
	}).Info("User connected to SSE")

	// Set headers for SSE
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")

	clientID := global.TPSSEManager.AddClient(userClaims.TenantID, userClaims.ID, c.Writer)
	defer global.TPSSEManager.RemoveClient(userClaims.TenantID, clientID)

	// 发送初始成功消息
	c.SSEvent("message", "Connected to system events")
	c.Writer.Flush()

	// 创建一个用于发送心跳的计时器
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	// 创建一个用于检查客户端是否仍然连接的通道
	done := make(chan bool)
	go func() {
		<-c.Request.Context().Done()
		done <- true
	}()

	for {
		select {
		case <-heartbeatTicker.C:
			// 发送心跳消息
			c.SSEvent("heartbeat", time.Now().Unix())
			c.Writer.Flush()
		case <-done:
			logrus.WithFields(logrus.Fields{
				"tenantID":  userClaims.TenantID,
				"userEmail": userClaims.Email,
			}).Info("User disconnected from SSE")
			return
		}
	}
}
