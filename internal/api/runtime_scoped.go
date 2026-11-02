package api

import (
	"github.com/gin-gonic/gin"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/constant"
	"project/pkg/errcode"
	"project/pkg/utils"
	"strconv"
)

func RuntimeUpdateDevice(c *gin.Context) {
	var req model.UpdateDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.Device.RuntimeUpdateDevice(req, c.MustGet("claims").(*utils.UserClaims))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}
func RuntimeUpdateVoucher(c *gin.Context) {
	var req model.UpdateDeviceVoucherReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.Device.RuntimeUpdateVoucher(c.Request.Context(), &req, c.MustGet("claims").(*utils.UserClaims))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}
func RuntimeRemoveSubDevice(c *gin.Context) {
	var req model.RemoveSonDeviceReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.Device.RuntimeRemoveSubDevice(c.Request.Context(), req.SubDeviceId, c.MustGet("claims").(*utils.UserClaims))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}
func RuntimeDeleteExpected(c *gin.Context) {
	err := service.GroupApp.ExpectedData.RuntimeDelete(c.Request.Context(), c.Param("id"), c.MustGet("claims").(*utils.UserClaims))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

func RuntimeTelemetryCurrent(c *gin.Context) {
	data, err := service.GroupApp.TelemetryData.GetCurrentTelemetrData(c.Param("id"), c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}
func RuntimeTelemetryHistory(c *gin.Context) {
	var req model.GetTelemetryHistoryDataByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	// The internal runtime capability is a bounded page read, never an export
	// job or an unbounded query. Preserve the ordinary public export API.
	if req.ExportExcel != nil && *req.ExportExcel {
		c.Error(errcode.New(errcode.CodeParamError))
		return
	}
	page, size := 1, 100
	if req.Page == nil {
		req.Page = &page
	}
	if req.PageSize == nil {
		req.PageSize = &size
	}
	if *req.Page < 1 || *req.PageSize < 1 || *req.PageSize > 1000 || *req.Page > 1000000 {
		c.Error(errcode.New(errcode.CodeParamError))
		return
	}
	data, err := service.GroupApp.TelemetryData.GetTelemetrHistoryDataByPageV2(&req, c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

func RuntimeCommandStatus(c *gin.Context) {
	claims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.CommandData.GetCommandStatus(c.Request.Context(), c.Param("message_id"), claims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

func RuntimeCommandPublish(c *gin.Context) {
	var req model.PutMessageForCommand
	if !BindAndValidate(c, &req) {
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	id, err := service.GroupApp.CommandData.CommandPutMessageWithResult(c.Request.Context(), claims.ID, &req, strconv.Itoa(constant.Manual), claims.TenantID, communitySessionAuthorization(c))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", gin.H{"message_id": id, "status": "accepted"})
}

func runtimeDebugHandler(c *gin.Context, handler gin.HandlerFunc) {
	ctx, err := service.RuntimeDebugContext(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.Request = c.Request.WithContext(ctx)
	handler(c)
}
func RuntimeDeviceDebugSet(c *gin.Context) {
	runtimeDebugHandler(c, Controllers.DeviceDebugApi.SetDeviceDebug)
}
func RuntimeDeviceDebugStatus(c *gin.Context) {
	runtimeDebugHandler(c, Controllers.DeviceDebugApi.GetDeviceDebugStatus)
}
func RuntimeDeviceDebugLogs(c *gin.Context) {
	runtimeDebugHandler(c, Controllers.DeviceDebugApi.GetDeviceDebugLogs)
}
