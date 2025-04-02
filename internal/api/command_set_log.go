package api

import (
	"project/pkg/constant"
	"project/pkg/utils"
	"strconv"

	model "project/internal/model"
	service "project/internal/service"

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
	err := service.GroupApp.CommandData.CommandPutMessage(c, userClaims.ID, &req, strconv.Itoa(constant.Manual))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
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
