package api

import (
	"net/http"

	model "project/internal/model"
	service "project/service"
	utils "project/utils"

	"github.com/gin-gonic/gin"
)

type EventDataApi struct{}

// GetEventDatasListByPage 事件数据查询（分页）
// @Tags     事件数据
// @Summary  事件数据查询（分页）
// @Description 事件数据查询（分页）
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetEventDatasListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/event/datas [get]
func (api *EventDataApi) GetEventDatasListByPage(c *gin.Context) {
	var req model.GetEventDatasListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.EventData.GetEventDatasListByPage(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get list successfully", data)
}
