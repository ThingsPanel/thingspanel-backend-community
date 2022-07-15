package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpRoleMenuController struct {
	beego.Controller
}

// 给角色添加菜单
func (TpRoleMenuController *TpRoleMenuController) Add() {
	TpRoleMenuValidate := valid.TpRoleMenuValidate{}
	err := json.Unmarshal(TpRoleMenuController.Ctx.Input.RequestBody, &TpRoleMenuValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpRoleMenuValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpRoleMenuValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpRoleMenuController.Ctx))
			break
		}
		return
	}
	var TpRoleMenuService services.TpRoleMenuService
	isSucess := TpRoleMenuService.AddRoleMenu(TpRoleMenuValidate.RoleId, TpRoleMenuValidate.MenuIds)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(TpRoleMenuController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpRoleMenuController.Ctx))
	}
}

// 修改角色的菜单
func (TpRoleMenuController *TpRoleMenuController) Edit() {
	TpRoleMenuValidate := valid.TpRoleMenuValidate{}
	err := json.Unmarshal(TpRoleMenuController.Ctx.Input.RequestBody, &TpRoleMenuValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpRoleMenuValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpRoleMenuValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpRoleMenuController.Ctx))
			break
		}
		return
	}
	var TpRoleMenuService services.TpRoleMenuService
	isSucess := TpRoleMenuService.EditRoleMenu(TpRoleMenuValidate.RoleId, TpRoleMenuValidate.MenuIds)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(TpRoleMenuController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpRoleMenuController.Ctx))
	}
}

// 获取角色的菜单
func (TpRoleMenuController *TpRoleMenuController) Index() {
	TpRoleIdValidate := valid.TpRoleIdValidate{}
	err := json.Unmarshal(TpRoleMenuController.Ctx.Input.RequestBody, &TpRoleIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpRoleIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpRoleIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpRoleMenuController.Ctx))
			break
		}
		return
	}
	var TpRoleMenuService services.TpRoleMenuService
	MenuList := TpRoleMenuService.GetRoleMenu(TpRoleIdValidate.RoleId)
	response.SuccessWithDetailed(200, "success", MenuList, map[string]string{}, (*context2.Context)(TpRoleMenuController.Ctx))

}

// 通过用户邮箱获取用户菜单
func (TpRoleMenuController *TpRoleMenuController) UserMenus() {
	authorization := TpRoleMenuController.Ctx.Request.Header["Authorization"][0]
	userToken := authorization[7:]
	userClaims, _ := response.ParseCliamsToken(userToken)
	var TpRoleMenuService services.TpRoleMenuService
	_, MenuList := TpRoleMenuService.GetRoleMenuListByUser(userClaims.Name)
	response.SuccessWithDetailed(200, "success", MenuList, map[string]string{}, (*context2.Context)(TpRoleMenuController.Ctx))

}
