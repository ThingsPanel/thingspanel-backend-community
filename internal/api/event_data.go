package api

import (
	"net/http"

	model "project/internal/model"
	service "project/internal/service"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type EventDataApi struct{}

// GetEventDatasListByPage 事件数据查询（分页）
// @Router   /api/v1/event/datas [get]
func (*EventDataApi) HandleEventDatasListByPage(c *gin.Context) {
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
