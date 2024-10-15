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
// @Tags     通知服务配置
// @Summary  通知服务配置
// @Description 当notice_type=EMAIL时，参数中email_config不得为空，当notice_type=SME时，参数中sme_config不得为空。
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.SaveNotificationServicesConfigReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/notification/services/config [post]
func (n *NotificationServicesConfigApi) SaveNotificationServicesConfig(c *gin.Context) {
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
// @Tags     通知服务配置
// @Summary  通知服务配置
// @Description 根据通知类型获取配置，但是支持的类型仅有 EMAIL 注意一定是大写
// @accept    application/json
// @Produce   application/json
// @Param    type  path      string     true  "type"
// @Success  200  {object}  ApiResponse  "更新设备配置成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/notification/services/config/{type} [get]
func (n *NotificationServicesConfigApi) GetNotificationServicesConfig(c *gin.Context) {
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
// @Tags     通知服务配置
// @Summary  发送测试邮件
// @Description 查找已配置的的邮箱参数，发送测试邮件
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.SendTestEmailReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/notification/services/config/e-mail/test [post]
func (n *NotificationServicesConfigApi) SendTestEmail(c *gin.Context) {
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
