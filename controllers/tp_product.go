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

type TpProductController struct {
	beego.Controller
}

// 列表
func (TpProductController *TpProductController) List() {
	PaginationValidate := valid.TpProductPaginationValidate{}
	err := json.Unmarshal(TpProductController.Ctx.Input.RequestBody, &PaginationValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpProductController.Ctx))
			break
		}
		return
	}
	var TpProductService services.TpProductService
	isSuccess, d, t := TpProductService.GetTpProductList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpProductController.Ctx))
		return
	}
	dd := valid.RspTpProductPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpProductController.Ctx))
}

// 编辑
func (TpProductController *TpProductController) Edit() {
	TpProductValidate := valid.TpProductValidate{}
	err := json.Unmarshal(TpProductController.Ctx.Input.RequestBody, &TpProductValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpProductValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpProductValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpProductController.Ctx))
			break
		}
		return
	}
	if TpProductValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpProductController.Ctx))
	}
	var TpProductService services.TpProductService
	isSucess := TpProductService.EditTpProduct(TpProductValidate)
	if isSucess {
		d := TpProductService.GetTpProductDetail(TpProductValidate.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpProductController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpProductController.Ctx))
	}
}

// 新增
func (TpProductController *TpProductController) Add() {
	AddTpProductValidate := valid.AddTpProductValidate{}
	err := json.Unmarshal(TpProductController.Ctx.Input.RequestBody, &AddTpProductValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpProductValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpProductValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpProductController.Ctx))
			break
		}
		return
	}
	var TpProductService services.TpProductService
	if AddTpProductValidate.Plugin == "" {
		AddTpProductValidate.Plugin = "{}"
	}
	id := uuid.GetUuid()
	TpProduct := models.TpProduct{
		Id:            id,
		ProtocolType:  AddTpProductValidate.ProtocolType,
		AuthType:      AddTpProductValidate.AuthType,
		Describe:      AddTpProductValidate.Describe,
		CreatedTime:   time.Now().Unix(),
		Name:          AddTpProductValidate.Name,
		Plugin:        AddTpProductValidate.Plugin,
		Remark:        AddTpProductValidate.Remark,
		SerialNumber:  AddTpProductValidate.SerialNumber,
		DeviceModelId: AddTpProductValidate.DeviceModelId,
	}
	rsp_err, d := TpProductService.AddTpProduct(TpProduct)
	if rsp_err == nil {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpProductController.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23505")
		if isTrue {
			err = "产品编号不能重复！"
		} else {
			err = rsp_err.Error()
		}
		response.SuccessWithMessage(400, err, (*context2.Context)(TpProductController.Ctx))
	}
}

// 删除
func (TpProductController *TpProductController) Delete() {
	TpProductIdValidate := valid.TpProductIdValidate{}
	err := json.Unmarshal(TpProductController.Ctx.Input.RequestBody, &TpProductIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpProductIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpProductIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpProductController.Ctx))
			break
		}
		return
	}
	if TpProductIdValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpProductController.Ctx))
	}
	var TpProductService services.TpProductService
	TpProduct := models.TpProduct{
		Id: TpProductIdValidate.Id,
	}
	rsp_err := TpProductService.DeleteTpProduct(TpProduct)
	if rsp_err == nil {
		response.SuccessWithMessage(200, "success", (*context2.Context)(TpProductController.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23503")
		if isTrue {
			err = "该产品下存在批次，请先删除批次！"
		} else {
			err = rsp_err.Error()
		}
		response.SuccessWithMessage(400, err, (*context2.Context)(TpProductController.Ctx))
	}
}
