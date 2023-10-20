package controllers

import (
	"ThingsPanel-Go/services"

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
	tpWsCurrentData.CurrentData(w, r)
}

// 主程序
func (c *TpWsOpenapiController) EventData() {
	w := c.Ctx.ResponseWriter
	r := c.Ctx.Request
	// 调用HandleConnections方法
	var tpWsEventData services.TpWsEventData
	tpWsEventData.EventData(w, r)
}
