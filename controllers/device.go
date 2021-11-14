package controllers

import (
	beego "github.com/beego/beego/v2/server/web"
)

type DeviceController struct {
	beego.Controller
}

// 设备列表
func (u *DeviceController) Index() {
	u.Data["json"] = "Index success"
	u.ServeJSON()
}

// 扫码激活设备
func (u *DeviceController) Scan() {
	u.Data["json"] = "Scan success"
	u.ServeJSON()
}
