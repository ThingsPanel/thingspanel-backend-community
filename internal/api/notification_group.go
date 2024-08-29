package api

import (
	"net/http"

	model "project/internal/model"
	service "project/service"
	utils "project/utils"

	"github.com/gin-gonic/gin"
)

type NotificationGroupApi struct{}

// CreateNotificationGroup 创建消息通知组
// @Tags     通知组
// @Summary  创建通知组
// @Description 创建通知组
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateNotificationGroupReq   true  "见下方JSON"
// @Success  200  {object}  CreateNotificationGroupResponse  "创建通知组成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/notification_group [post]
func (api *NotificationGroupApi) CreateNotificationGroup(c *gin.Context) {
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
// @Tags     通知组
// @Summary  获取通知组详情
// @Description 获取通知组详情
// @accept    application/json
// @Produce   application/json
// @Param     id   path      string  true  "通知组ID"
// @Success  200  {object}  GetNotificationGroupResponse  "获取通知组详情成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  404  {object}  ApiResponse  "通知组不存在"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/notification_group/{id} [get]
func (api *NotificationGroupApi) GetNotificationGroupById(c *gin.Context) {
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
// @Tags     通知组
// @Summary  更新通知组
// @Description 更新通知组
// @accept    application/json
// @Produce   application/json
// @Param     id   path      string  true  "通知组ID"
// @Param     data  body      model.UpdateNotificationGroupReq   true  "见下方JSON"
// @Success  200  {object}  UpdateNotificationGroupResponse  "更新通知组成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  404  {object}  ApiResponse  "通知组不存在"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/notification_group/{id} [put]
func (api *NotificationGroupApi) UpdateNotificationGroup(c *gin.Context) {
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
// @Tags     通知组
// @Summary  删除通知组
// @Description 删除通知组
// @accept    application/json
// @Produce   application/json
// @Param     id   path      string  true  "通知组ID"
// @Success  200  {object}  DeleteNotificationGroupResponse  "删除通知组成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  404  {object}  ApiResponse  "通知组不存在"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/notification_group/{id} [delete]
func (api *NotificationGroupApi) DeleteNotificationGroup(c *gin.Context) {
	id := c.Param("id")
	if err := service.GroupApp.NotificationGroup.DeleteNotificationGroup(id); err != nil {
		ErrorHandler(c, http.StatusBadRequest, err)
		return
	} else {
		SuccessHandler(c, "Delete notification group successfully", nil)
	}
}

// GetNotificationGroupListByPage 获取通知组列表并分页
// @Tags     通知组
// @Summary  获取通知组列表
// @Description 获取通知组列表
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetNotificationGroupListByPageReq true "见下方JSON"
// @Success  200  {object}  GetNotificationGroupListByPageResponse  "获取通知组列表成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/notification_group/list [get]
func (api *NotificationGroupApi) GetNotificationGroupListByPage(c *gin.Context) {
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
