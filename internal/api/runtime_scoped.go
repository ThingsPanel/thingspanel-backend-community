package api

import (
	"github.com/gin-gonic/gin"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/utils"
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
