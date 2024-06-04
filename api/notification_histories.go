package api

import (
	"net/http"

	model "project/model"
	service "project/service"
	utils "project/utils"

	"github.com/gin-gonic/gin"
)

type NotificationHistoryApi struct{}

// GetNotificationHistoryListByPage 获取通知组列表并分页
// @Tags     通知历史
// @Summary  获取通知历史列表
// @Description 获取通知历史列表
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetNotificationHistoryListByPageReq true "见下方JSON，时间字段格式为 2006-01-02 15:04:05"
// @Success  200  {object}  GetNotificationHistoryListByPageResponse  "获取通知历史列表成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/notification_history/list [get]
func (api *NotificationHistoryApi) GetNotificationHistoryListByPage(c *gin.Context) {
	var req model.GetNotificationHistoryListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID
	notificationList, err := service.GroupApp.NotificationHisory.GetNotificationHistoryListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	ntfoutput, err := utils.SerializeData(notificationList, GetNotificationHistoryListByPageOutSchema{})
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get notification list successfully", ntfoutput)
}
