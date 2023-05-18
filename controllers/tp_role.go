package controllers

import (
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"

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
func (c *TpRoleController) List() {
	reqData := valid.GetRoleValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpRoleService services.TpRoleService
	count, dd := TpRoleService.GetRoleList(reqData.PerPage, reqData.CurrentPage, tenantId)
	d := PaginateRoleList{
		CurrentPage: reqData.CurrentPage,
		Data:        dd,
		Total:       count,
		PerPage:     reqData.PerPage,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 编辑
func (c *TpRoleController) Edit() {
	reqData := valid.TpRoleValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	if reqData.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(c.Ctx))
	}
	var TpRoleService services.TpRoleService
	TpRole := models.TpRole{
		Id:           reqData.Id,
		RoleName:     reqData.RoleName,
		RoleDescribe: reqData.RoleDescribe,
	}
	isSucess := TpRoleService.EditRole(TpRole, tenantId)
	if isSucess {
		response.SuccessWithDetailed(200, "success", TpRole, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}

// 新增
func (c *TpRoleController) Add() {
	reqData := valid.TpRoleValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpRoleService services.TpRoleService
	TpRole := models.TpRole{
		Id:           reqData.Id,
		RoleName:     reqData.RoleName,
		RoleDescribe: reqData.RoleDescribe,
		TenantId:     tenantId,
	}
	isSucess, d := TpRoleService.AddRole(TpRole)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}

// 删除
func (c *TpRoleController) Delete() {
	reqData := valid.TpRoleValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	if reqData.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(c.Ctx))
		return
	}
	var CasbinService services.CasbinService
	if CasbinService.HasRole(reqData.Id) {
		response.SuccessWithMessage(1000, "不能删除与用户有绑定的角色", (*context2.Context)(c.Ctx))
		return
	}
	var TpRoleService services.TpRoleService
	TpRole := models.TpRole{
		Id:       reqData.Id,
		RoleName: reqData.RoleName,
	}
	isSucess := TpRoleService.DeleteRole(TpRole, tenantId)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}
