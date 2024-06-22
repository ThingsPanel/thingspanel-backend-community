package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"project/model"
	"project/service"
)

type ServicePluginApi struct{}

func (api *ServicePluginApi) Create(c *gin.Context) {
	var req model.CreateServicePluginReq
	if !BindAndValidate(c, &req) {
		return
	}
	//var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServicePlugin.Create(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "create service successfully", resp)
}

func (api *ServicePluginApi) GetList(c *gin.Context) {
	var req model.GetServicePluginByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServicePlugin.List(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get service list successfully", resp)
}

func (api *ServicePluginApi) Get(c *gin.Context) {
	var req model.GetServicePluginReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServicePlugin.Get(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get service list successfully", resp)
}

func (api *ServicePluginApi) Update(c *gin.Context) {
	var req model.UpdateServicePluginReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.ServicePlugin.Update(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "update service successfully", map[string]interface{}{})
}

func (api *ServicePluginApi) Delete(c *gin.Context) {
	var req model.DeleteServicePluginReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.ServicePlugin.Delete(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "delete service successfully", map[string]interface{}{})
}
