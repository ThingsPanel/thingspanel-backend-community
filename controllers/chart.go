package controllers

import (
	beego "github.com/beego/beego/v2/server/web"
)

type ChartController struct {
	beego.Controller
}

// 获取图表
func (u *ChartController) Dashboard() {
	u.Data["json"] = "Dashboard success"
	u.ServeJSON()
}

// 遥测数据
func (u *ChartController) Ws() {
	u.Data["json"] = "ws success"
	u.ServeJSON()
}
