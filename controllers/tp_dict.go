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

type TpDictController struct {
	beego.Controller
}

// 列表
func (TpDictController *TpDictController) List() {
	PaginationValidate := valid.TpDictPaginationValidate{}
	err := json.Unmarshal(TpDictController.Ctx.Input.RequestBody, &PaginationValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpDictController.Ctx))
			break
		}
		return
	}
	var TpDictService services.TpDictService
	isSuccess, d, t := TpDictService.GetTpDictList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpDictController.Ctx))
		return
	}
	dd := valid.RspTpDictPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpDictController.Ctx))
}

// 编辑
func (TpDictController *TpDictController) Edit() {
	TpDictValidate := valid.TpDictValidate{}
	err := json.Unmarshal(TpDictController.Ctx.Input.RequestBody, &TpDictValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpDictValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpDictValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpDictController.Ctx))
			break
		}
		return
	}
	if TpDictValidate.ID == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpDictController.Ctx))
	}
	var TpDictService services.TpDictService
	TpDict := models.TpDict{
		ID:        TpDictValidate.ID,
		DictCode:  TpDictValidate.DictCode,
		DictValue: TpDictValidate.DictValue,
		CreatedAt: TpDictValidate.CreatedAt,
	}
	isSucess := TpDictService.EditTpDict(TpDict)
	if isSucess {
		response.SuccessWithDetailed(200, "success", TpDict, map[string]string{}, (*context2.Context)(TpDictController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpDictController.Ctx))
	}
}

// 新增
func (TpDictController *TpDictController) Add() {
	AddTpDictValidate := valid.AddTpDictValidate{}
	err := json.Unmarshal(TpDictController.Ctx.Input.RequestBody, &AddTpDictValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpDictValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpDictValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpDictController.Ctx))
			break
		}
		return
	}
	var TpDictService services.TpDictService
	id := uuid.GetUuid()
	TpDict := models.TpDict{
		ID:        id,
		DictCode:  AddTpDictValidate.DictCode,
		DictValue: AddTpDictValidate.DictValue,
		CreatedAt: AddTpDictValidate.CreatedAt,
	}
	isSucess, d := TpDictService.AddTpDict(TpDict)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpDictController.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(TpDictController.Ctx))
	}
}

// 删除
func (TpDictController *TpDictController) Delete() {
	TpDictValidate := valid.TpDictValidate{}
	err := json.Unmarshal(TpDictController.Ctx.Input.RequestBody, &TpDictValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpDictValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpDictValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpDictController.Ctx))
			break
		}
		return
	}
	if TpDictValidate.ID == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpDictController.Ctx))
	}
	var TpDictService services.TpDictService
	TpDict := models.TpDict{
		ID: TpDictValidate.ID,
	}
	isSucess := TpDictService.DeleteTpDict(TpDict)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(TpDictController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpDictController.Ctx))
	}
}
