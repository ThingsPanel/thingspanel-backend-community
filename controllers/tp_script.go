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
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpScriptController struct {
	beego.Controller
}

// 列表
func (TpScriptController *TpScriptController) List() {
	PaginationValidate := valid.TpScriptPaginationValidate{}
	err := json.Unmarshal(TpScriptController.Ctx.Input.RequestBody, &PaginationValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpScriptController.Ctx))
			break
		}
		return
	}
	var TpScriptService services.TpScriptService
	isSuccess, d, t := TpScriptService.GetTpScriptList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpScriptController.Ctx))
		return
	}
	dd := valid.RspTpScriptPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpScriptController.Ctx))
}

// 编辑
func (TpScriptController *TpScriptController) Edit() {
	TpScriptValidate := valid.TpScriptValidate{}
	err := json.Unmarshal(TpScriptController.Ctx.Input.RequestBody, &TpScriptValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpScriptValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpScriptValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpScriptController.Ctx))
			break
		}
		return
	}
	if TpScriptValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpScriptController.Ctx))
	}
	var TpScriptService services.TpScriptService
	isSucess := TpScriptService.EditTpScript(TpScriptValidate)
	if isSucess {
		d := TpScriptService.GetTpScriptDetail(TpScriptValidate.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpScriptController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpScriptController.Ctx))
	}
}

// 新增
func (TpScriptController *TpScriptController) Add() {
	AddTpScriptValidate := valid.AddTpScriptValidate{}
	err := json.Unmarshal(TpScriptController.Ctx.Input.RequestBody, &AddTpScriptValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpScriptValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpScriptValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpScriptController.Ctx))
			break
		}
		return
	}
	var TpScriptService services.TpScriptService
	id := uuid.GetUuid()
	TpScript := models.TpScript{
		Id:             id,
		ProtocolType:   AddTpScriptValidate.ProtocolType,
		ScriptName:     AddTpScriptValidate.ScriptName,
		Company:        AddTpScriptValidate.Company,
		CreatedAt:      time.Now().Unix(),
		ProductName:    AddTpScriptValidate.ProductName,
		ScriptContentA: AddTpScriptValidate.ScriptContentA,
		ScriptContentB: AddTpScriptValidate.ScriptContentB,
		ScriptType:     AddTpScriptValidate.ScriptType,
		Remark:         AddTpScriptValidate.Remark,
	}
	isSucess, d := TpScriptService.AddTpScript(TpScript)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpScriptController.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(TpScriptController.Ctx))
	}
}

// 删除
func (TpScriptController *TpScriptController) Delete() {
	TpScriptIdValidate := valid.TpScriptIdValidate{}
	err := json.Unmarshal(TpScriptController.Ctx.Input.RequestBody, &TpScriptIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpScriptIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpScriptIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpScriptController.Ctx))
			break
		}
		return
	}
	if TpScriptIdValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpScriptController.Ctx))
	}
	var TpScriptService services.TpScriptService
	TpScript := models.TpScript{
		Id: TpScriptIdValidate.Id,
	}
	isSucess := TpScriptService.DeleteTpScript(TpScript)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(TpScriptController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpScriptController.Ctx))
	}
}
