package controllers

import (
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"

	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpMenuController struct {
	beego.Controller
}

// 列表
func (TpMenuController *TpMenuController) Tree() {
	var TpMenuService services.TpMenuService
	isSuccess, d := TpMenuService.GetMenuTree()
	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpMenuController.Ctx))
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpMenuController.Ctx))
}
