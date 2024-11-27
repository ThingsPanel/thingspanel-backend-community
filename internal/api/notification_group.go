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
	var req model.CreateNotificationGroupReq

	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	notificationGroup, err := service.GroupApp.NotificationGroup.CreateNotificationGroup(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	notificationGroupOs, err := utils.SerializeData(*notificationGroup, ReadNotificationGroupOutSchema{})
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create notification group successfully", notificationGroupOs)
}

// GetNotificationGroup 获取通知组详情
// @Router   /api/v1/notification_group/{id} [get]
func (*NotificationGroupApi) HandleNotificationGroupById(c *gin.Context) {
	id := c.Param("id")
	if ntfgroup, err := service.GroupApp.NotificationGroup.GetNotificationGroupById(id); err != nil {
		ErrorHandler(c, http.StatusBadRequest, err)
		return
	} else {
		notificationGroupOs, err := utils.SerializeData(*ntfgroup, ReadNotificationGroupOutSchema{})
		if err != nil {
			ErrorHandler(c, http.StatusInternalServerError, err)
			return
		}
		SuccessHandler(c, "Get notification group successfully", notificationGroupOs)
	}
}

// UpdateNotificationGroup 更新通知组
// @Router   /api/v1/notification_group/{id} [put]
func (*NotificationGroupApi) UpdateNotificationGroup(c *gin.Context) {
	id := c.Param("id")
	var req model.UpdateNotificationGroupReq
	if !BindAndValidate(c, &req) {
		return
	}

	if updated, err := service.GroupApp.NotificationGroup.UpdateNotificationGroup(id, &req); err != nil {
		ErrorHandler(c, http.StatusNotFound, err)
		return
	} else {
		updateoutput, err := utils.SerializeData(updated, UpdateNotificationGroupOutSchema{})
		if err != nil {
			ErrorHandler(c, http.StatusInternalServerError, err)
			return
		}
		SuccessHandler(c, "Update notification group successfully", updateoutput)
	}
}

// DeleteNotificationGroup 删除通知组
// @Router   /api/v1/notification_group/{id} [delete]
func (*NotificationGroupApi) DeleteNotificationGroup(c *gin.Context) {
	id := c.Param("id")
	if err := service.GroupApp.NotificationGroup.DeleteNotificationGroup(id); err != nil {
		ErrorHandler(c, http.StatusBadRequest, err)
		return
	} else {
		SuccessHandler(c, "Delete notification group successfully", nil)
	}
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
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	ntfoutput, err := utils.SerializeData(notificationList, GetNotificationGroupListByPageOutSchema{})
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get notification list successfully", ntfoutput)
}
