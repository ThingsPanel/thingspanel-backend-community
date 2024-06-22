package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"project/model"
	"project/service"
)

type ServiceAccessApi struct{}

func (api *ServiceAccessApi) Create(c *gin.Context) {
	var req model.CreateAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	//var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.CreateAccess(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "create service successfully", resp)
}

func (api *ServiceAccessApi) GetList(c *gin.Context) {
	var req model.GetServiceAccessByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServiceAccess.List(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get service access list successfully", resp)
}

func (api *ServiceAccessApi) Update(c *gin.Context) {
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

func (api *ServiceAccessApi) Delete(c *gin.Context) {
	var req model.DeleteAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.ServiceAccess.Delete(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "delete service access successfully", map[string]interface{}{})
}
