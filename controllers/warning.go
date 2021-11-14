package controllers

import (
	beego "github.com/beego/beego/v2/server/web"
)

type WarningController struct {
	beego.Controller
}

// 新增告警策略
func (u *WarningController) Add() {
	u.Data["json"] = "Add success"
	u.ServeJSON()
}

// 获取所有告警策略
func (u *WarningController) Show() {
	u.Data["json"] = "Show success"
	u.ServeJSON()
}

// 获取具体某一条告警策略
func (u *WarningController) GetById() {
	u.Data["json"] = "GetById success"
	u.ServeJSON()
}

// 修改告警策略
func (u *WarningController) Edit() {
	u.Data["json"] = "Edit success"
	u.ServeJSON()
}

// 删除具体某一条告警策略
func (u *WarningController) DeleteById() {
	u.Data["json"] = "DeleteById success"
	u.ServeJSON()
}

// 获取告警日志
func (u *WarningController) Index() {
	u.Data["json"] = "Index success"
	u.ServeJSON()
}

// 分页获取告警日志
func (u *WarningController) List() {
	u.Data["json"] = "List success"
	u.ServeJSON()
}
