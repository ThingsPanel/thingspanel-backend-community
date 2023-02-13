package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpAutomationController struct {
	beego.Controller
}

// 列表
func (TpAutomationController *TpAutomationController) List() {
	PaginationValidate := valid.TpAutomationPaginationValidate{}
	err := json.Unmarshal(TpAutomationController.Ctx.Input.RequestBody, &PaginationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(PaginationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(PaginationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpAutomationController.Ctx))
			break
		}
		return
	}
	var TpAutomationService services.TpAutomationService
	isSuccess, d, t := TpAutomationService.GetTpAutomationList(PaginationValidate)

	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpAutomationController.Ctx))
		return
	}
	dd := valid.RspTpAutomationPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpAutomationController.Ctx))
}

// 编辑
func (TpAutomationController *TpAutomationController) Edit() {
	TpAutomationValidate := valid.TpAutomationValidate{}
	err := json.Unmarshal(TpAutomationController.Ctx.Input.RequestBody, &TpAutomationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpAutomationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpAutomationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpAutomationController.Ctx))
			break
		}
		return
	}
	if TpAutomationValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpAutomationController.Ctx))
	}
	var TpAutomationService services.TpAutomationService
	isSucess := TpAutomationService.EditTpAutomation(TpAutomationValidate)
	if isSucess {
		d, _ := TpAutomationService.GetTpAutomationDetail(TpAutomationValidate.Id)
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpAutomationController.Ctx))
	} else {
		utils.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpAutomationController.Ctx))
	}
}

// 新增
func (TpAutomationController *TpAutomationController) Add() {
	AddTpAutomationValidate := valid.AddTpAutomationValidate{}
	err := json.Unmarshal(TpAutomationController.Ctx.Input.RequestBody, &AddTpAutomationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpAutomationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpAutomationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpAutomationController.Ctx))
			break
		}
		return
	}
	var TpAutomationService services.TpAutomationService
	d, rsp_err := TpAutomationService.AddTpAutomation(AddTpAutomationValidate)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpAutomationController.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpAutomationController.Ctx))
	}
}

// 删除
func (TpAutomationController *TpAutomationController) Delete() {
	TpAutomationIdValidate := valid.TpAutomationIdValidate{}
	err := json.Unmarshal(TpAutomationController.Ctx.Input.RequestBody, &TpAutomationIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpAutomationIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpAutomationIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpAutomationController.Ctx))
			break
		}
		return
	}
	if TpAutomationIdValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpAutomationController.Ctx))
	}
	var TpAutomationService services.TpAutomationService
	TpAutomation := models.TpAutomation{
		Id: TpAutomationIdValidate.Id,
	}
	req_err := TpAutomationService.DeleteTpAutomation(TpAutomation)
	if req_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(TpAutomationController.Ctx))
	} else {
		utils.SuccessWithMessage(400, "删除失败", (*context2.Context)(TpAutomationController.Ctx))
	}
}
