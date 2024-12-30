package api

import (
	"fmt"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type AlarmApi struct{}

// /api/v1/alarm/config [post]
func (*AlarmApi) CreateAlarmConfig(c *gin.Context) {
	var req model.CreateAlarmConfigReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID
	data, err := service.GroupApp.Alarm.CreateAlarmConfig(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// /api/v1/alarm/config/{id} [Delete]
func (*AlarmApi) DeleteAlarmConfig(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": fmt.Sprintf("id is %s", id),
		}))
		return
	}

	err := service.GroupApp.Alarm.DeleteAlarmConfig(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/alarm/config [PUT]
func (*AlarmApi) UpdateAlarmConfig(c *gin.Context) {
	var req model.UpdateAlarmConfigReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = &userClaims.TenantID
	data, err := service.GroupApp.Alarm.UpdateAlarmConfig(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/alarm/config [GET]
func (*AlarmApi) ServeAlarmConfigListByPage(c *gin.Context) {
	var req model.GetAlarmConfigListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID

	data, err := service.GroupApp.Alarm.GetAlarmConfigListByPage(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/alarm/info [put]
func (*AlarmApi) UpdateAlarmInfo(c *gin.Context) {
	var req model.UpdateAlarmInfoReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	data, err := service.GroupApp.Alarm.UpdateAlarmInfo(&req, userClaims.ID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)

}

// /api/v1/alarm/info/batch [put]
func (*AlarmApi) BatchUpdateAlarmInfo(c *gin.Context) {
	var req model.UpdateAlarmInfoBatchReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.Alarm.UpdateAlarmInfoBatch(&req, userClaims.ID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/alarm/info [get]
func (*AlarmApi) HandleAlarmInfoListByPage(c *gin.Context) {
	var req model.GetAlarmInfoListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID

	data, err := service.GroupApp.Alarm.GetAlarmInfoListByPage(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/alarm/info/history [get]
func (*AlarmApi) HandleAlarmHisttoryListByPage(c *gin.Context) {
	//
	var req model.GetAlarmHisttoryListByPage
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	data, err := service.GroupApp.Alarm.GetAlarmHisttoryListByPage(&req, userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/alarm/info/history [put]
func (*AlarmApi) AlarmHistoryDescUpdate(c *gin.Context) {
	//
	var req model.AlarmHistoryDescUpdateReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.Alarm.AlarmHistoryDescUpdate(&req, userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

func (*AlarmApi) HandleDeviceAlarmStatus(c *gin.Context) {
	//
	var req model.GetDeviceAlarmStatusReq
	if !BindAndValidate(c, &req) {
		return
	}
	//var userClaims = c.MustGet("claims").(*utils.UserClaims)

	ok := service.GroupApp.Alarm.GetDeviceAlarmStatus(&req)
	c.Set("data", map[string]bool{
		"alarm": ok,
	})
}

// /api/v1/alarm/info/config/device [get]
func (*AlarmApi) HandleConfigByDevice(c *gin.Context) {
	//
	var req model.GetDeviceAlarmStatusReq
	if !BindAndValidate(c, &req) {
		return
	}
	//var userClaims = c.MustGet("claims").(*utils.UserClaims)

	list, err := service.GroupApp.Alarm.GetConfigByDevice(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}
