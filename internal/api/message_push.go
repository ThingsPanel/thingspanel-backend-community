package api

import (
	"net/http"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type MessagePushApi struct {
}

// /api/v1/message_push [post]
func (*MessagePushApi) CreateMessagePush(c *gin.Context) {
	var req model.CreateMessagePushReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	if req.InstallationID != "" || req.AppID != "" {
		device, err := service.GroupApp.MessagePush.BindDevice(c.Request.Context(), &req, userClaims, c.GetHeader("x-token"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 400, "message": "App device registration rejected"})
			return
		}
		c.Set("data", gin.H{"bindingId": device.ID, "userId": device.UserID, "tenantId": device.TenantID})
		return
	}
	err := service.GroupApp.MessagePush.CreateMessagePush(&req, userClaims.ID)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// /api/v1/message_push/logout [post]
func (*MessagePushApi) MessagePushMangeLogout(c *gin.Context) {
	var req model.MessagePushMangeLogoutReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	if err := service.GroupApp.MessagePush.UnbindDevice(c.Request.Context(), userClaims.ID, req.PushId, c.GetHeader("x-token")); err != nil {
		c.Error(err)
		return
	}
	err := service.GroupApp.MessagePush.MessagePushMangeLogout(&req, userClaims.ID)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// /api/v1/message_push/config [get]
func (*MessagePushApi) GetMessagePushConfig(c *gin.Context) {
	res, err := service.GroupApp.MessagePush.GetMessagePushConfig()
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", res)
}

// /api/v1/message_push/config [post]
func (*MessagePushApi) SetMessagePushConfig(c *gin.Context) {
	var req model.MessagePushConfigReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.MessagePush.SetMessagePushConfig(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}
