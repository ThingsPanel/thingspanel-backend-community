package api

import (
	"net/http"
	"project/internal/model"
	"project/internal/service"

	"github.com/gin-gonic/gin"
)

type ServicePluginApi struct{}

func (*ServicePluginApi) Create(c *gin.Context) {
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

// /api/v1/service/list
func (*ServicePluginApi) GetList(c *gin.Context) {
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

func (*ServicePluginApi) Get(c *gin.Context) {
	id := c.Param("id")
	resp, err := service.GroupApp.ServicePlugin.Get(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get service list successfully", resp)
}

func (*ServicePluginApi) Update(c *gin.Context) {
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

func (*ServicePluginApi) Delete(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.ServicePlugin.Delete(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "delete service successfully", map[string]interface{}{})
}

// /api/v1/plugin/heartbeat
func (*ServicePluginApi) Heartbeat(c *gin.Context) {
	var req model.HeartbeatReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.ServicePlugin.Heartbeat(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "heartbeat service successfully", map[string]interface{}{})
}

// GetServiceSelect
// /api/v1/service/plugin/select
func (*ServicePluginApi) GetServiceSelect(c *gin.Context) {
	var req model.GetServiceSelectReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServicePlugin.GetServiceSelect(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get service select successfully", resp)
}

// /api/v1/service/plugin/info
// 根据ServiceIdentifier获取服务插件信息
func (*ServicePluginApi) GetServicePluginByServiceIdentifier(c *gin.Context) {
	var req model.GetServicePluginByServiceIdentifierReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.ServicePlugin.GetServicePluginByServiceIdentifier(req.ServiceIdentifier)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "success", data)
}
