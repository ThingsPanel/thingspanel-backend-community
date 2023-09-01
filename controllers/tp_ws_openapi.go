package controllers

import (
	"ThingsPanel-Go/services"

	"github.com/beego/beego/v2/core/logs"
	beego "github.com/beego/beego/v2/server/web"
)

type TpWsOpenapiController struct {
	beego.Controller
}

// 主程序
func (c *TpWsOpenapiController) WsHandler() {
	w := c.Ctx.ResponseWriter
	r := c.Ctx.Request
	// 调用HandleConnections方法
	var TpWsOpenapi services.TpWsOpenapi
	TpWsOpenapi.HandleConnections(w, r)
	return
}

// 主程序
func (c *TpWsOpenapiController) CurrentData() {
	w := c.Ctx.ResponseWriter
	r := c.Ctx.Request
	// 调用HandleConnections方法
	var tpWsCurrentData services.TpWsCurrentData
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		logs.Error("tenant_id is missing")
		return
	}
	tpWsCurrentData.CurrentData(w, r, tenantId)
}

// 主程序
func (c *TpWsOpenapiController) EventData() {
	w := c.Ctx.ResponseWriter
	r := c.Ctx.Request
	// 调用HandleConnections方法
	var tpWsEventData services.TpWsEventData
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		logs.Error("tenant_id is missing")
		return
	}
	tpWsEventData.EventData(w, r, tenantId)
}
