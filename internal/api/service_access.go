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

func (*ServiceAccessApi) Create(c *gin.Context) {
	var req model.CreateAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.CreateAccess(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "create service successfully", resp)
}

// /api/v1/service/access/list
func (*ServiceAccessApi) GetList(c *gin.Context) {
	var req model.GetServiceAccessByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.List(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get service access list successfully", resp)
}

// /api/v1/service/access
func (*ServiceAccessApi) Update(c *gin.Context) {
	var req model.UpdateAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.ServiceAccess.Update(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "update service access successfully", map[string]interface{}{})
}

func (*ServiceAccessApi) Delete(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.ServiceAccess.Delete(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "delete service access successfully", map[string]interface{}{})
}

// /api/v1/service/access/voucher/form
// 服务接入点凭证表单查询
func (*ServiceAccessApi) GetVoucherForm(c *gin.Context) {
	var req model.GetServiceAccessVoucherFormReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServiceAccess.GetVoucherForm(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get service access config form successfully", resp)
}

// /api/v1/service/access/device/list
// 三方服务设备列表查询
func (*ServiceAccessApi) GetDeviceList(c *gin.Context) {
	var req model.ServiceAccessDeviceListReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.GetServiceAccessDeviceList(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get device list successfully", resp)
}

// /api/v1/plugin/service/access/list
// 服务接入点插件列表查询
func (*ServiceAccessApi) GetPluginServiceAccessList(c *gin.Context) {
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
func (*ServiceAccessApi) GetPluginServiceAccess(c *gin.Context) {
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
