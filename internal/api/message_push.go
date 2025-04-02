package api

import (
	"github.com/gin-gonic/gin"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/utils"
)

type MessagePushApi struct {
}

// /api/v1/alarm/config [post]
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

func (*MessagePushApi) GetMessagePushConfig(c *gin.Context) {
	res, err := service.GroupApp.MessagePush.GetMessagePushConfig()
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", res)
}

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
