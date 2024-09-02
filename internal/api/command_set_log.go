package api

import (
	"net/http"
	"project/common"
	"project/constant"
	"project/utils"
	"strconv"

	model "project/internal/model"
	service "project/service"

	"github.com/gin-gonic/gin"
)

type CommandSetLogApi struct{}

// GetSetLogsDataListByPage 命令下发记录查询（分页）
// @Tags     命令下发
// @Summary  命令下发记录查询（分页）
// @Description 命令下发记录查询（分页）
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetCommandSetLogsListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/command/datas/set/logs [get]
func (a *CommandSetLogApi) GetSetLogsDataListByPage(c *gin.Context) {
	var req model.GetCommandSetLogsListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	date, err := service.GroupApp.CommandData.GetCommandSetLogsDataListByPage(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get data successfully", date)
}

// /api/v1/command/datas/pub
func (a *CommandSetLogApi) CommandPutMessage(c *gin.Context) {
	var req model.PutMessageForCommand
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.CommandData.CommandPutMessage(c, userClaims.ID, &req, strconv.Itoa(constant.Manual))
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessOK(c)
}

// /api/v1/command/datas/:id
func (a *CommandSetLogApi) GetCommandList(c *gin.Context) {
	id := c.Param("id")

	data, err := service.GroupApp.CommandData.GetCommonList(c, id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, common.SUCCESS, data)
}
