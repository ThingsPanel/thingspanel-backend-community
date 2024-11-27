package api

import (
	"errors"
	"net/http"
	"project/internal/model"
	"project/internal/service"
	common "project/pkg/common"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type SceneAutomationsApi struct{}

// 创建场景联动
// /api/v1/scene_automations
func (*SceneAutomationsApi) CreateSceneAutomations(c *gin.Context) {
	logrus.Info("创建场景联动请求")
	var req model.CreateSceneAutomationReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	id, err := service.GroupApp.SceneAutomation.CreateSceneAutomation(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "create scene automations successfully", map[string]interface{}{"scene_automation_id": id})
}

// 删除场景联动
func (*SceneAutomationsApi) DeleteSceneAutomations(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.SceneAutomation.DeleteSceneAutomation(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "delete scene automations successfully", 1)
}

// 更新场景联动
func (*SceneAutomationsApi) SwitchSceneAutomations(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.SceneAutomation.SwitchSceneAutomation(id, "")
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "switch scene successfully", nil)
}

// 更新场景联动
func (*SceneAutomationsApi) UpdateSceneAutomations(c *gin.Context) {
	var req model.UpdateSceneAutomationReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	id, err := service.GroupApp.SceneAutomation.UpdateSceneAutomation(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "update scene automations successfully", map[string]interface{}{"scene_automation_id": id})
}

// 场景联动详情查询
func (*SceneAutomationsApi) HandleSceneAutomations(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.SceneAutomation.GetSceneAutomation(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get scene detail successfully", data)
}

// 场景联动列表查询
func (*SceneAutomationsApi) HandleSceneAutomationsByPage(c *gin.Context) {
	var req model.GetSceneAutomationByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.SceneAutomation.GetSceneAutomationByPageReq(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get scene automations successfully", data)
}

// 场景联动列表查询
func (*SceneAutomationsApi) HandleSceneAutomationsWithAlarmByPage(c *gin.Context) {
	var req model.GetSceneAutomationsWithAlarmByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	if common.IsStringEmpty(req.DeviceId) && common.IsStringEmpty(req.DeviceConfigId) {
		ErrorHandler(c, http.StatusInternalServerError, errors.New("设备id和设备配置id至少有一个"))
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.SceneAutomation.GetSceneAutomationWithAlarmByPageReq(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get scene automations successfully", data)
}

// 场景联动日志查询
func (*SceneAutomationsApi) HandleSceneAutomationsLog(c *gin.Context) {
	var req model.GetSceneAutomationLogReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.SceneAutomationLog.GetSceneAutomationLog(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get scene log successfully", data)
}
