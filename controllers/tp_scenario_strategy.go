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

type TpScenarioStrategyController struct {
	beego.Controller
}

// 列表
func (TpScenarioStrategyController *TpScenarioStrategyController) List() {
	PaginationValidate := valid.TpScenarioStrategyPaginationValidate{}
	err := json.Unmarshal(TpScenarioStrategyController.Ctx.Input.RequestBody, &PaginationValidate)
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
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpScenarioStrategyController.Ctx))
			break
		}
		return
	}
	var TpScenarioStrategyService services.TpScenarioStrategyService
	d, t, err := TpScenarioStrategyService.GetTpScenarioStrategyList(PaginationValidate)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(TpScenarioStrategyController.Ctx))
		return
	}
	dd := valid.RspTpScenarioStrategyPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpScenarioStrategyController.Ctx))
}

// 详情
func (TpScenarioStrategyController *TpScenarioStrategyController) Detail() {
	TpScenarioStrategyIdValidate := valid.TpScenarioStrategyIdValidate{}
	err := json.Unmarshal(TpScenarioStrategyController.Ctx.Input.RequestBody, &TpScenarioStrategyIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpScenarioStrategyIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpScenarioStrategyIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpScenarioStrategyController.Ctx))
			break
		}
		return
	}
	var TpScenarioStrategyService services.TpScenarioStrategyService
	d, err := TpScenarioStrategyService.GetTpScenarioStrategyDetail(TpScenarioStrategyIdValidate.Id)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(TpScenarioStrategyController.Ctx))
		return
	}
	utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpScenarioStrategyController.Ctx))
}

// 编辑
func (TpScenarioStrategyController *TpScenarioStrategyController) Edit() {
	EditTpScenarioStrategyValidate := valid.EditTpScenarioStrategyValidate{}
	err := json.Unmarshal(TpScenarioStrategyController.Ctx.Input.RequestBody, &EditTpScenarioStrategyValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(EditTpScenarioStrategyValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(EditTpScenarioStrategyValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpScenarioStrategyController.Ctx))
			break
		}
		return
	}
	if EditTpScenarioStrategyValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpScenarioStrategyController.Ctx))
	}
	var TpScenarioStrategyService services.TpScenarioStrategyService
	d, err := TpScenarioStrategyService.EditTpScenarioStrategy(EditTpScenarioStrategyValidate)
	if err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpScenarioStrategyController.Ctx))
	} else {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(TpScenarioStrategyController.Ctx))
	}
}

// 新增
func (TpScenarioStrategyController *TpScenarioStrategyController) Add() {
	AddTpScenarioStrategyValidate := valid.AddTpScenarioStrategyValidate{}
	err := json.Unmarshal(TpScenarioStrategyController.Ctx.Input.RequestBody, &AddTpScenarioStrategyValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpScenarioStrategyValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpScenarioStrategyValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpScenarioStrategyController.Ctx))
			break
		}
		return
	}
	var TpScenarioStrategyService services.TpScenarioStrategyService
	d, rsp_err := TpScenarioStrategyService.AddTpScenarioStrategy(AddTpScenarioStrategyValidate)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpScenarioStrategyController.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpScenarioStrategyController.Ctx))
	}
}

// 删除
func (TpScenarioStrategyController *TpScenarioStrategyController) Delete() {
	TpScenarioStrategyIdValidate := valid.TpScenarioStrategyIdValidate{}
	err := json.Unmarshal(TpScenarioStrategyController.Ctx.Input.RequestBody, &TpScenarioStrategyIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpScenarioStrategyIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpScenarioStrategyIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpScenarioStrategyController.Ctx))
			break
		}
		return
	}
	if TpScenarioStrategyIdValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpScenarioStrategyController.Ctx))
	}
	var TpScenarioStrategyService services.TpScenarioStrategyService
	TpScenarioStrategy := models.TpScenarioStrategy{
		Id: TpScenarioStrategyIdValidate.Id,
	}
	req_err := TpScenarioStrategyService.DeleteTpScenarioStrategy(TpScenarioStrategy)
	if req_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(TpScenarioStrategyController.Ctx))
	} else {
		utils.SuccessWithMessage(400, "无法删除；可能的原因：1.被自动化关联的场景无法删除，需要先取消关联；"+req_err.Error(), (*context2.Context)(TpScenarioStrategyController.Ctx))
	}
}
