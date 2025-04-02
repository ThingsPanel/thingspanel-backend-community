package api

import (
	model "project/internal/model"
	service "project/internal/service"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type NotificationHistoryApi struct{}

// GetNotificationHistoryListByPage 获取通知组列表并分页
// @Router   /api/v1/notification_history/list [get]
func (*NotificationHistoryApi) HandleNotificationHistoryListByPage(c *gin.Context) {
	var req model.GetNotificationHistoryListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID
	notificationList, err := service.GroupApp.NotificationHisory.GetNotificationHistoryListByPage(&req)
	if err != nil {
		c.Error(err)
		return
	}
	ntfoutput, err := utils.SerializeData(notificationList, GetNotificationHistoryListByPageOutSchema{})
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", ntfoutput)
}
