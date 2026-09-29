package api

import (
	"project/pkg/constant"
	"project/pkg/utils"
	"strconv"

	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/errcode"

	"github.com/gin-gonic/gin"
)

type CommandSetLogApi struct{}

// ServeSetLogsDataListByPage 命令下发记录查询（分页）
// @Router   /api/v1/command/datas/set/logs [get]
func (CommandSetLogApi) ServeSetLogsDataListByPage(c *gin.Context) {
	var req model.GetCommandSetLogsListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	date, err := service.GroupApp.CommandData.GetCommandSetLogsDataListByPage(req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", date)
}

// /api/v1/command/datas/pub [post]
func (CommandSetLogApi) CommandPutMessage(c *gin.Context) {
	var req model.PutMessageForCommand
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	messageID, err := service.GroupApp.CommandData.CommandPutMessageWithResult(c, userClaims.ID, &req, strconv.Itoa(constant.Manual), userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", gin.H{"message_id": messageID, "status": "accepted"})
}

// GetCommandStatus returns the latest recorded command receipt by message_id.
// @Router /api/v1/command/datas/status/{message_id} [get]
func (CommandSetLogApi) GetCommandStatus(c *gin.Context) {
	claims := c.MustGet("claims").(*utils.UserClaims)
	messageID := c.Param("message_id")
	if messageID == "" {
		c.Error(errcode.NewWithMessage(errcode.CodeParamError, "message_id is required"))
		return
	}
	data, err := service.GroupApp.CommandData.GetCommandStatus(c, messageID, claims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/command/datas/{id}
func (CommandSetLogApi) HandleCommandList(c *gin.Context) {
	id := c.Param("id")

	data, err := service.GroupApp.CommandData.GetCommonList(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}
