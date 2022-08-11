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

type CasbinController struct {
	beego.Controller
}

// 角色添加多个功能
func (CasbinController *CasbinController) AddFunctionToRole() {
	FunctionsRoleValidate := valid.FunctionsRoleValidate{}
	err := json.Unmarshal(CasbinController.Ctx.Input.RequestBody, &FunctionsRoleValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(FunctionsRoleValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(FunctionsRoleValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(CasbinController.Ctx))
			break
		}
		return
	}
	var CasbinService services.CasbinService
	isSuccess := CasbinService.AddFunctionToRole(FunctionsRoleValidate.Role, FunctionsRoleValidate.Functions)
	if isSuccess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(CasbinController.Ctx))
	}
	response.SuccessWithMessage(1000, "failed", (*context2.Context)(CasbinController.Ctx))
}

// 查询角色的功能
func (CasbinController *CasbinController) GetFunctionFromRole() {
	RoleValidate := valid.RoleValidate{}
	err := json.Unmarshal(CasbinController.Ctx.Input.RequestBody, &RoleValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(RoleValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(RoleValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(CasbinController.Ctx))
			break
		}
		return
	}
	var CasbinService services.CasbinService
	roles, isSuccess := CasbinService.GetFunctionFromRole(RoleValidate.Role)
	if isSuccess {
		response.SuccessWithDetailed(200, "success", roles, map[string]string{}, (*context2.Context)(CasbinController.Ctx))
	}
	response.SuccessWithMessage(1000, "failed", (*context2.Context)(CasbinController.Ctx))
}

// 修改角色的功能
func (CasbinController *CasbinController) UpdateFunctionFromRole() {
	FunctionsRoleValidate := valid.FunctionsRoleValidate{}
	err := json.Unmarshal(CasbinController.Ctx.Input.RequestBody, &FunctionsRoleValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(FunctionsRoleValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(FunctionsRoleValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(CasbinController.Ctx))
			break
		}
		return
	}
	var CasbinService services.CasbinService
	f, _ := CasbinService.GetFunctionFromRole(FunctionsRoleValidate.Role)
	if len(f) > 0 {
		//没有记录删除会返回false
		isSuccess := CasbinService.RemoveRoleAndFunction(FunctionsRoleValidate.Role)
		if !isSuccess {
			response.SuccessWithMessage(1000, "failed", (*context2.Context)(CasbinController.Ctx))
			return
		}
	}
	isSuccess := CasbinService.AddFunctionToRole(FunctionsRoleValidate.Role, FunctionsRoleValidate.Functions)
	if isSuccess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(CasbinController.Ctx))
	} else {
		response.SuccessWithMessage(1000, "failed", (*context2.Context)(CasbinController.Ctx))
	}
}

// 删除角色的功能
func (CasbinController *CasbinController) DeleteFunctionFromRole() {
	RoleValidate := valid.RoleValidate{}
	err := json.Unmarshal(CasbinController.Ctx.Input.RequestBody, &RoleValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(RoleValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(RoleValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(CasbinController.Ctx))
			break
		}
		return
	}
	var CasbinService services.CasbinService
	isSuccess := CasbinService.RemoveRoleAndFunction(RoleValidate.Role)
	if isSuccess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(CasbinController.Ctx))
	}
	response.SuccessWithMessage(1000, "failed", (*context2.Context)(CasbinController.Ctx))
}

// 用户添加多个角色
func (CasbinController *CasbinController) AddRoleToUser() {
	RolesUserValidate := valid.RolesUserValidate{}
	err := json.Unmarshal(CasbinController.Ctx.Input.RequestBody, &RolesUserValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(RolesUserValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(RolesUserValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(CasbinController.Ctx))
			break
		}
		return
	}
	var CasbinService services.CasbinService
	isSuccess := CasbinService.AddRolesToUser(RolesUserValidate.User, RolesUserValidate.Roles)
	if isSuccess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(CasbinController.Ctx))
	}
	response.SuccessWithMessage(1000, "failed", (*context2.Context)(CasbinController.Ctx))
}

// 查询用户的角色
func (CasbinController *CasbinController) GetRolesFromUser() {
	UserValidate := valid.UserValidate{}
	err := json.Unmarshal(CasbinController.Ctx.Input.RequestBody, &UserValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(UserValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(UserValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(CasbinController.Ctx))
			break
		}
		return
	}
	var CasbinService services.CasbinService
	roles, isSuccess := CasbinService.GetRoleFromUser(UserValidate.User)
	if isSuccess {
		response.SuccessWithDetailed(200, "success", roles, map[string]string{}, (*context2.Context)(CasbinController.Ctx))
	}
	response.SuccessWithMessage(1000, "failed", (*context2.Context)(CasbinController.Ctx))
}

// 修改用户的角色
func (CasbinController *CasbinController) UpdateRolesFromUser() {
	RolesUserValidate := valid.RolesUserValidate{}
	err := json.Unmarshal(CasbinController.Ctx.Input.RequestBody, &RolesUserValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(RolesUserValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(RolesUserValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(CasbinController.Ctx))
			break
		}
		return
	}
	var CasbinService services.CasbinService
	CasbinService.RemoveUserAndRole(RolesUserValidate.User)
	isSuccess := CasbinService.AddRolesToUser(RolesUserValidate.User, RolesUserValidate.Roles)
	if isSuccess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(CasbinController.Ctx))
	}
	response.SuccessWithMessage(1000, "failed", (*context2.Context)(CasbinController.Ctx))
}

// 删除角色的功能
func (CasbinController *CasbinController) DeleteRolesFromUser() {
	UserValidate := valid.UserValidate{}
	err := json.Unmarshal(CasbinController.Ctx.Input.RequestBody, &UserValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(UserValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(UserValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(CasbinController.Ctx))
			break
		}
		return
	}
	var CasbinService services.CasbinService
	isSuccess := CasbinService.RemoveUserAndRole(UserValidate.User)
	if isSuccess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(CasbinController.Ctx))
	}
	response.SuccessWithMessage(1000, "failed", (*context2.Context)(CasbinController.Ctx))
}
