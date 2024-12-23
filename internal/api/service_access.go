package api

import (
	"net/http"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type ServiceAccessApi struct{}

// /api/v1/service/access [post]
func (*ServiceAccessApi) Create(c *gin.Context) {
	var req model.CreateAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.CreateAccess(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/service/access/list
func (*ServiceAccessApi) HandleList(c *gin.Context) {
	var req model.GetServiceAccessByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.List(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/service/access [put]
func (*ServiceAccessApi) Update(c *gin.Context) {
	var req model.UpdateAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.ServiceAccess.Update(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/service/access/:id [delete]
func (*ServiceAccessApi) Delete(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.ServiceAccess.Delete(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/service/access/voucher/form
// 服务接入点凭证表单查询
func (*ServiceAccessApi) HandleVoucherForm(c *gin.Context) {
	var req model.GetServiceAccessVoucherFormReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServiceAccess.GetVoucherForm(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("resp", resp)
}

// /api/v1/service/access/device/list
// 三方服务设备列表查询
func (*ServiceAccessApi) HandleDeviceList(c *gin.Context) {
	var req model.ServiceAccessDeviceListReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.GetServiceAccessDeviceList(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("resp", resp)
}

// /api/v1/plugin/service/access/list
// 服务接入点插件列表查询
func (*ServiceAccessApi) HandlePluginServiceAccessList(c *gin.Context) {
	logrus.Info("get plugin list")
	var req model.GetPluginServiceAccessListReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServiceAccess.GetPluginServiceAccessList(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get plugin list successfully", resp)
}

// /api/v1/pugin/service/access
func (*ServiceAccessApi) HandlePluginServiceAccess(c *gin.Context) {
	var req model.GetPluginServiceAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServiceAccess.GetPluginServiceAccess(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get plugin list successfully", resp)
}
