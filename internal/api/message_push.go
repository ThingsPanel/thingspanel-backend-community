package api

import (
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
