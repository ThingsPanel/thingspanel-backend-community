package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpFunctionController struct {
	beego.Controller
}

// 列表
func (TpFunctionController *TpFunctionController) List() {
	FunctionPaginationValidate := valid.FunctionPaginationValidate{}
	err := json.Unmarshal(TpFunctionController.Ctx.Input.RequestBody, &FunctionPaginationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(FunctionPaginationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(FunctionPaginationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpFunctionController.Ctx))
			break
		}
		return
	}
	var TpFunctionService services.TpFunctionService
	isSuccess, d, t := TpFunctionService.GetFunctionList(FunctionPaginationValidate)
	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpFunctionController.Ctx))
		return
	}
	dd := valid.FunctionPaginationValidate{
		CurrentPage: FunctionPaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     FunctionPaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpFunctionController.Ctx))
}

// 编辑
func (TpFunctionController *TpFunctionController) Edit() {
	TpFunctionValidate := valid.TpFunctionValidate{}
	err := json.Unmarshal(TpFunctionController.Ctx.Input.RequestBody, &TpFunctionValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpFunctionValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpFunctionValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpFunctionController.Ctx))
			break
		}
		return
	}
	if TpFunctionValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpFunctionController.Ctx))
	}
	var TpFunctionService services.TpFunctionService
	TpFunction := models.TpFunction{
		Id:           TpFunctionValidate.Id,
		FunctionName: TpFunctionValidate.FunctionName,
		Path:         TpFunctionValidate.Path,
		Name:         TpFunctionValidate.Name,
		Component:    TpFunctionValidate.Component,
		Title:        TpFunctionValidate.Title,
		Icon:         TpFunctionValidate.Icon,
		Type:         TpFunctionValidate.Type,
		FunctionCode: TpFunctionValidate.FunctionCode,
		ParentId:     TpFunctionValidate.ParentId,
		Sort:         TpFunctionValidate.Sort,
	}
	isSucess := TpFunctionService.EditFunction(TpFunction)
	if isSucess {
		response.SuccessWithDetailed(200, "success", TpFunction, map[string]string{}, (*context2.Context)(TpFunctionController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpFunctionController.Ctx))
	}
}

// 新增
func (TpFunctionController *TpFunctionController) Add() {
	TpFunctionValidate := valid.TpFunctionValidate{}
	err := json.Unmarshal(TpFunctionController.Ctx.Input.RequestBody, &TpFunctionValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpFunctionValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpFunctionValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpFunctionController.Ctx))
			break
		}
		return
	}
	var TpFunctionService services.TpFunctionService
	function_id := uuid.GetUuid()
	TpFunction := models.TpFunction{
		Id:           function_id,
		FunctionName: TpFunctionValidate.FunctionName,
		Path:         TpFunctionValidate.Path,
		Name:         TpFunctionValidate.Name,
		Component:    TpFunctionValidate.Component,
		Title:        TpFunctionValidate.Title,
		Icon:         TpFunctionValidate.Icon,
		Type:         TpFunctionValidate.Type,
		FunctionCode: TpFunctionValidate.FunctionCode,
		ParentId:     TpFunctionValidate.ParentId,
		Sort:         TpFunctionValidate.Sort,
	}
	isSucess, d := TpFunctionService.AddFunction(TpFunction)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpFunctionController.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(TpFunctionController.Ctx))
	}
}

// 删除
func (TpFunctionController *TpFunctionController) Delete() {
	TpFunctionValidate := valid.TpFunctionValidate{}
	err := json.Unmarshal(TpFunctionController.Ctx.Input.RequestBody, &TpFunctionValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpFunctionValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpFunctionValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpFunctionController.Ctx))
			break
		}
		return
	}
	if TpFunctionValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpFunctionController.Ctx))
	}
	var TpFunctionService services.TpFunctionService
	TpFunction := models.TpFunction{
		Id:           TpFunctionValidate.Id,
		FunctionName: TpFunctionValidate.FunctionName,
	}
	isSucess := TpFunctionService.DeleteFunction(TpFunction)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(TpFunctionController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpFunctionController.Ctx))
	}
}

// 功能下拉列表
func (c *TpFunctionController) FunctionPullDownList() {
	// 获取用户权限
	authority, ok := c.Ctx.Input.GetData("authority").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpFunctionService services.TpFunctionService
	d := TpFunctionService.FunctionPullDownList(authority)
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	return
}

// 通过用户邮箱获取用户菜单
func (TpFunctionController *TpFunctionController) UserAuth() {
	authorization := TpFunctionController.Ctx.Request.Header["Authorization"][0]
	userToken := authorization[7:]
	userClaims, _ := response.ParseCliamsToken(userToken)
	var TpFunctionService services.TpFunctionService
	MenuTree, FunctionList, pageTree := TpFunctionService.Authority(userClaims.Name)
	d := map[string]interface{}{
		"menu_tree":  MenuTree,
		"page_tree":  pageTree,
		"other_list": FunctionList,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpFunctionController.Ctx))
}

// 获取权限树
func (TpFunctionController *TpFunctionController) AuthorityList() {
	var TpFunctionService services.TpFunctionService
	d := TpFunctionService.AuthorityList()
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpFunctionController.Ctx))
}
