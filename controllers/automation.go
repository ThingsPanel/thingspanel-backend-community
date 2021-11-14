package controllers

import (
	beego "github.com/beego/beego/v2/server/web"
)

type AutomationController struct {
	beego.Controller
}

// 新增控制策略
func (u *AutomationController) Add() {
	u.Data["json"] = "Add success"
	u.ServeJSON()
}

// 获取某个控制策略
func (u *AutomationController) GetById() {
	u.Data["json"] = "GetById success"
	u.ServeJSON()
}

// 修改某个控制策略
func (u *AutomationController) Edit() {
	u.Data["json"] = "Edit success"
	u.ServeJSON()
}

// 删除某个控制策略
func (u *AutomationController) DeleteById() {
	u.Data["json"] = "DeleteById success"
	u.ServeJSON()
}

// 获取定时执行参数
func (u *AutomationController) Status() {
	u.Data["json"] = "Status success"
	u.ServeJSON()
}

// 【触发条件】获取设备属性及单位
func (u *AutomationController) Show() {
	u.Data["json"] = "Show success"
	u.ServeJSON()
}

// 【控制指令】获取设备属性及单位
func (u *AutomationController) Instruct() {
	u.Data["json"] = "Instruct success"
	u.ServeJSON()
}

// 获取资产信息
func (u *AutomationController) Property() {
	u.Data["json"] = "Property success"
	u.ServeJSON()
}

// 获取所有控制策略
func (u *AutomationController) Index() {
	u.Data["json"] = "Index success"
	u.ServeJSON()
}
