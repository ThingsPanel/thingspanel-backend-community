package api

import (
	"fmt"
	"net/http"

	dal "project/internal/dal"
	model "project/internal/model"
	service "project/internal/service"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type NotificationServicesConfigApi struct{}

// SaveNotificationServicesConfig 创建/修改通知服务配置（2合1接口）
// @Router   /api/v1/notification/services/config [post]
func (*NotificationServicesConfigApi) SaveNotificationServicesConfig(c *gin.Context) {
	var req model.SaveNotificationServicesConfigReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	// 验证SYS_ADMIN
	if userClaims.Authority != dal.SYS_ADMIN {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("权限不足"))
		return
	}

	// 验证通知类型，暂支持邮件和短信
	if req.NoticeType != model.NoticeType_Email && req.NoticeType != model.NoticeType_SME {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("notice type 不正确"))
		return
	}

	// 开关枚举验证
	if req.Status != model.OPEN && req.Status != model.CLOSE {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("status 不正确"))
		return
	}

	data, err := service.GroupApp.NotificationServicesConfig.SaveNotificationServicesConfig(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)
}

// GetNotificationServicesConfig 根据通知类型获取配置
// @Router   /api/v1/notification/services/config/{type} [get]
func (*NotificationServicesConfigApi) HandleNotificationServicesConfig(c *gin.Context) {
	noticeType := c.Param("type")
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	// 验证SYS_ADMIN
	if userClaims.Authority != dal.SYS_ADMIN {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("权限不足,无法获取"))
		return
	}
	data, err := service.GroupApp.NotificationServicesConfig.GetNotificationServicesConfig(noticeType)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)
}

// SendTestEmail 发送测试邮件
// @Router   /api/v1/notification/services/config/e-mail/test [post]
func (*NotificationServicesConfigApi) SendTestEmail(c *gin.Context) {
	var req model.SendTestEmailReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.NotificationServicesConfig.SendTestEmail(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", nil)
}
