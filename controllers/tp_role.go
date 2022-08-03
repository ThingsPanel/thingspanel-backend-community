package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
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

type TpRoleController struct {
	beego.Controller
}
type PaginateRoleList struct {
	CurrentPage int             `json:"current_page"`
	Data        []models.TpRole `json:"data"`
	Total       int64           `json:"total"`
	PerPage     int             `json:"per_page"`
}

// 列表
func (TpRoleController *TpRoleController) List() {
	GetRoleValidate := valid.GetRoleValidate{}
	err := json.Unmarshal(TpRoleController.Ctx.Input.RequestBody, &GetRoleValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(GetRoleValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(GetRoleValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpRoleController.Ctx))
			break
		}
		return
	}
	var TpRoleService services.TpRoleService
	count, dd := TpRoleService.GetRoleList(GetRoleValidate.PerPage, GetRoleValidate.CurrentPage)
	d := PaginateRoleList{
		CurrentPage: GetRoleValidate.CurrentPage,
		Data:        dd,
		Total:       count,
		PerPage:     GetRoleValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpRoleController.Ctx))
}

// 编辑
func (TpRoleController *TpRoleController) Edit() {
	tpRoleValidate := valid.TpRoleValidate{}
	err := json.Unmarshal(TpRoleController.Ctx.Input.RequestBody, &tpRoleValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(tpRoleValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(tpRoleValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpRoleController.Ctx))
			break
		}
		return
	}
	if tpRoleValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpRoleController.Ctx))
	}
	var TpRoleService services.TpRoleService
	TpRole := models.TpRole{
		Id:           tpRoleValidate.Id,
		RoleName:     tpRoleValidate.RoleName,
		RoleDescribe: tpRoleValidate.RoleDescribe,
	}
	isSucess := TpRoleService.EditRole(TpRole)
	if isSucess {
		response.SuccessWithDetailed(200, "success", TpRole, map[string]string{}, (*context2.Context)(TpRoleController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpRoleController.Ctx))
	}
}

// 新增
func (TpRoleController *TpRoleController) Add() {
	tpRoleValidate := valid.TpRoleValidate{}
	err := json.Unmarshal(TpRoleController.Ctx.Input.RequestBody, &tpRoleValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(tpRoleValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(tpRoleValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpRoleController.Ctx))
			break
		}
		return
	}
	var TpRoleService services.TpRoleService
	TpRole := models.TpRole{
		Id:           tpRoleValidate.Id,
		RoleName:     tpRoleValidate.RoleName,
		RoleDescribe: tpRoleValidate.RoleDescribe,
	}
	isSucess, d := TpRoleService.AddRole(TpRole)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpRoleController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpRoleController.Ctx))
	}
}

// 删除
func (TpRoleController *TpRoleController) Delete() {
	tpRoleValidate := valid.TpRoleValidate{}
	err := json.Unmarshal(TpRoleController.Ctx.Input.RequestBody, &tpRoleValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(tpRoleValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(tpRoleValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpRoleController.Ctx))
			break
		}
		return
	}
	if tpRoleValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpRoleController.Ctx))
		return
	}
	var CasbinService services.CasbinService
	if CasbinService.HasRole(tpRoleValidate.Id) {
		response.SuccessWithMessage(1000, "不能删除与用户有绑定的角色", (*context2.Context)(TpRoleController.Ctx))
		return
	}
	var TpRoleService services.TpRoleService
	TpRole := models.TpRole{
		Id:       tpRoleValidate.Id,
		RoleName: tpRoleValidate.RoleName,
	}
	isSucess := TpRoleService.DeleteRole(TpRole)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(TpRoleController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpRoleController.Ctx))
	}
	return
}
