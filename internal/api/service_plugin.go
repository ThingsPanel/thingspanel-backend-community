package api

import (
	"project/internal/model"
	"project/internal/service"

	"github.com/gin-gonic/gin"
)

type ServicePluginApi struct{}

// /api/v1/service POST
func (*ServicePluginApi) Create(c *gin.Context) {
	var req model.CreateServicePluginReq
	if !BindAndValidate(c, &req) {
		return
	}
	//var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServicePlugin.Create(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/service/list GET
func (*ServicePluginApi) HandleList(c *gin.Context) {
	var req model.GetServicePluginByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServicePlugin.List(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/service/detail/{id} GET
func (*ServicePluginApi) Handle(c *gin.Context) {
	id := c.Param("id")
	resp, err := service.GroupApp.ServicePlugin.Get(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/service PUT
func (*ServicePluginApi) Update(c *gin.Context) {
	var req model.UpdateServicePluginReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.ServicePlugin.Update(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", map[string]interface{}{})
}

// /api/v1/service/{id} DELETE
func (*ServicePluginApi) Delete(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.ServicePlugin.Delete(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", map[string]interface{}{})
}

// /api/v1/plugin/heartbeat
func (*ServicePluginApi) Heartbeat(c *gin.Context) {
	var req model.HeartbeatReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.ServicePlugin.Heartbeat(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", map[string]interface{}{})
}

// GetServiceSelect
// /api/v1/service/plugin/select GET
func (*ServicePluginApi) HandleServiceSelect(c *gin.Context) {
	var req model.GetServiceSelectReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServicePlugin.GetServiceSelect(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/service/plugin/info GET
// 根据ServiceIdentifier获取服务插件信息
func (*ServicePluginApi) HandleServicePluginByServiceIdentifier(c *gin.Context) {
	var req model.GetServicePluginByServiceIdentifierReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.ServicePlugin.GetServicePluginByServiceIdentifier(req.ServiceIdentifier)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}
