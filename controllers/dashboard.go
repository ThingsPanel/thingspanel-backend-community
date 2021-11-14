package controllers

import (
	beego "github.com/beego/beego/v2/server/web"
)

type DashboardController struct {
	beego.Controller
}

// 视图列表
func (u *DashboardController) Index() {
	u.Data["json"] = "Index success"
	u.ServeJSON()
}

// 添加视图
func (u *DashboardController) Paneladd() {
	u.Data["json"] = "Paneladd success"
	u.ServeJSON()
}
