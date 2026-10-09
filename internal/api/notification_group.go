package api

import (
	"net/http"
	model "project/internal/model"
	service "project/internal/service"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type NotificationGroupApi struct{}

// CreateNotificationGroup 创建消息通知组
// @Router   /api/v1/notification_group [post]
func (*NotificationGroupApi) CreateNotificationGroup(c *gin.Context) {
	retireLegacyNotificationGroupWrite(c)
}

// GetNotificationGroup 获取通知组详情
// @Router   /api/v1/notification_group/{id} [get]
func (*NotificationGroupApi) HandleNotificationGroupById(c *gin.Context) {
	id := c.Param("id")
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	if ntfgroup, err := service.GroupApp.NotificationGroup.GetNotificationGroupById(id, userClaims.TenantID); err != nil {
		c.Error(err)
		return
	} else {
		notificationGroupOs, err := utils.SerializeData(*ntfgroup, ReadNotificationGroupOutSchema{})
		if err != nil {
			c.Error(err)
			return
		}
		c.Set("data", notificationGroupOs)
	}
}

// UpdateNotificationGroup 更新通知组
// @Router   /api/v1/notification_group/{id} [put]
func (*NotificationGroupApi) UpdateNotificationGroup(c *gin.Context) {
	retireLegacyNotificationGroupWrite(c)
}

// DeleteNotificationGroup 删除通知组
// @Router   /api/v1/notification_group/{id} [delete]
func (*NotificationGroupApi) DeleteNotificationGroup(c *gin.Context) {
	retireLegacyNotificationGroupWrite(c)
}

// GetNotificationGroupListByPage 获取通知组列表并分页
// @Router   /api/v1/notification_group/list [get]
func (*NotificationGroupApi) HandleNotificationGroupListByPage(c *gin.Context) {
	var req model.GetNotificationGroupListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	notificationList, err := service.GroupApp.NotificationGroup.GetNotificationGroupListByPage(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	ntfoutput, err := utils.SerializeData(notificationList, GetNotificationGroupListByPageOutSchema{})
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", ntfoutput)
}

// Only legacy public writes are retired. Native publication and compatibility
// reads keep their existing authenticated, tenant-scoped routes.
func retireLegacyNotificationGroupWrite(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusGone, gin.H{
		"code":    http.StatusGone,
		"message": "legacy notification group management is retired; use notification policies",
		"reason":  "legacy_notification_group_retired",
	})
}
