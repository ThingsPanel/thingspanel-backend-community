package controllers

import (
	beego "github.com/beego/beego/v2/server/web"
)

type AuthController struct {
	beego.Controller
}

// 小程序登录
func (u *AuthController) Login() {
	u.Data["json"] = "Login success"
	u.ServeJSON()
}

// 退出登录
func (u *AuthController) Logout() {
	u.Data["json"] = "logout success"
	u.ServeJSON()
}

// 个人信息
func (u *AuthController) Me() {
	u.Data["json"] = "me success"
	u.ServeJSON()
}
