package sseapi

import (
	"net/http"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type SSEApi struct{}

// /api/v1/events

func (*SSEApi) HandleSystemEvents(c *gin.Context) {
	userClaims, ok := c.MustGet("claims").(*utils.UserClaims)
	if !ok {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "UserClaims not found",
		}))
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
	c.Writer.Header().Set("X-Accel-Buffering", "no") // 禁用 nginx 缓冲,防止连接过早关闭

	// http.Server 的 ReadTimeout/WriteTimeout 是"整条请求/响应"的绝对截止时间(见
	// internal/app/http_service.go),对普通接口合适,但对 SSE 这种长连接流式响应是致命的:
	// 连接建立满 60s 就会被服务端无条件掐断,客户端表现为每 60s 稳定重连一次。
	// 这里只清除本次响应的读写截止时间,其余接口仍然受全局超时保护。
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		logrus.WithError(err).Warn("SSE: 无法清除写截止时间,连接仍会被 WriteTimeout 掐断")
	}
	if err := rc.SetReadDeadline(time.Time{}); err != nil {
		logrus.WithError(err).Warn("SSE: 无法清除读截止时间,连接仍会被 ReadTimeout 掐断")
	}

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
