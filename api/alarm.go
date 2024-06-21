package api

import (
	"fmt"
	"net/http"
	"project/model"
	"project/service"
	"project/utils"

	"github.com/gin-gonic/gin"
)

type AlarmApi struct{}

// api/v1/alarm/config [post]
func (a *AlarmApi) CreateAlarmConfig(c *gin.Context) {
	var req model.CreateAlarmConfigReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID
	data, err := service.GroupApp.Alarm.CreateAlarmConfig(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create successfully", data)
}

// api/v1/alarm/config/{id} [Delete]
func (a *AlarmApi) DeleteAlarmConfig(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		ErrorHandler(c, http.StatusBadRequest, fmt.Errorf("id is required"))
		return
	}

	err := service.GroupApp.Alarm.DeleteAlarmConfig(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete successfully", nil)
}

// api/v1/alarm/config [PUT]
func (a *AlarmApi) UpdateAlarmConfig(c *gin.Context) {
	var req model.UpdateAlarmConfigReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = &userClaims.TenantID
	data, err := service.GroupApp.Alarm.UpdateAlarmConfig(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Update successfully", data)
}

// api/v1/alarm/config [GET]
func (a *AlarmApi) GetAlarmConfigListByPage(c *gin.Context) {
	var req model.GetAlarmConfigListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID

	data, err := service.GroupApp.Alarm.GetAlarmConfigListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get successfully", data)
}

// /api/v1/alarm/info [put]
func (a *AlarmApi) UpdateAlarmInfo(c *gin.Context) {
	var req model.UpdateAlarmInfoReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	data, err := service.GroupApp.Alarm.UpdateAlarmInfo(&req, userClaims.ID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Update successfully", data)

}

// api/v1/alarm/info/batch [put]
func (a *AlarmApi) BatchUpdateAlarmInfo(c *gin.Context) {
	var req model.UpdateAlarmInfoBatchReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.Alarm.UpdateAlarmInfoBatch(&req, userClaims.ID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Update successfully", nil)
}

func (a *AlarmApi) GetAlarmInfoListByPage(c *gin.Context) {
	var req model.GetAlarmInfoListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID

	data, err := service.GroupApp.Alarm.GetAlarmInfoListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get successfully", data)
}

func (a *AlarmApi) GetAlarmHisttoryListByPage(c *gin.Context) {
	//
	var req model.GetAlarmHisttoryListByPage
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	data, err := service.GroupApp.Alarm.GetAlarmHisttoryListByPage(&req, userClaims.TenantID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get successfully", data)
}

func (a *AlarmApi) AlarmHistoryDescUpdate(c *gin.Context) {
	//
	var req model.AlarmHistoryDescUpdateReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.Alarm.AlarmHistoryDescUpdate(&req, userClaims.TenantID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get successfully", nil)
}

func (a *AlarmApi) GetDeviceAlarmStatus(c *gin.Context) {
	//
	var req model.GetDeviceAlarmStatusReq
	if !BindAndValidate(c, &req) {
		return
	}
	//var userClaims = c.MustGet("claims").(*utils.UserClaims)

	ok := service.GroupApp.Alarm.GetDeviceAlarmStatus(&req)

	SuccessHandler(c, "Get successfully", map[string]bool{
		"alarm": ok,
	})
}

func (a *AlarmApi) GetConfigByDevice(c *gin.Context) {
	//
	var req model.GetDeviceAlarmStatusReq
	if !BindAndValidate(c, &req) {
		return
	}
	//var userClaims = c.MustGet("claims").(*utils.UserClaims)

	ok := service.GroupApp.Alarm.GetConfigByDevice(&req)

	SuccessHandler(c, "Get successfully", map[string]bool{
		"alarm": ok,
	})
}
