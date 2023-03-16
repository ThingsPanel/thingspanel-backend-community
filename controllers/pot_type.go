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
	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"strings"
	"time"
)

type PotTypeController struct {
	beego.Controller
}

func (pot *PotTypeController) Index() {
	PaginationValidate := valid.TpProductPaginationValidate{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &PaginationValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(pot.Ctx))
			break
		}
		return
	}
	var TpProductService services.PotTypeService
	isSuccess, d, t := TpProductService.GetPotTypeList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(pot.Ctx))
		return
	}
	dd := valid.RspPotTypePaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(pot.Ctx))

}

/**
创建
*/
func (pot *PotTypeController) Add() {
	potTypeValidate := valid.PotType{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &potTypeValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(potTypeValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(potTypeValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(pot.Ctx))
			break
		}
		return
	}

	var PotTypeService services.PotTypeService

	id := uuid.GetUuid()
	PotType := models.PotType{
		Id:       id,
		CreateAt: time.Now().Unix(),
		Name:     potTypeValidate.Name,
		Image:    potTypeValidate.Image,
	}
	rsp_err, d := PotTypeService.AddPotType(PotType)
	if rsp_err == nil {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(pot.Ctx))
	} else {
		var err string
		err = rsp_err.Error()
		response.SuccessWithMessage(400, err, (*context2.Context)(pot.Ctx))
	}
	response.SuccessWithMessage(200, "success", (*context2.Context)(pot.Ctx))
}

// 编辑
func (pot *PotTypeController) Edit() {
	PotTypeValidate := valid.PotType{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &PotTypeValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(PotTypeValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(PotTypeValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(pot.Ctx))
			break
		}
		return
	}
	if PotTypeValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(pot.Ctx))
	}
	var PotTypeService services.PotTypeService
	isSucess := PotTypeService.EditPotType(PotTypeValidate)
	if isSucess {
		d := PotTypeService.GetPotTypeDetail(PotTypeValidate.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(pot.Ctx))
	}
}

// 删除
func (pot *PotTypeController) Delete() {
	PotTypeValidator := valid.PotTypeIdValidate{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &PotTypeValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(PotTypeValidator)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(PotTypeValidator, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(pot.Ctx))
			break
		}
		return
	}
	if PotTypeValidator.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(pot.Ctx))
	}
	var PotTypeService services.PotTypeService
	TpProduct := models.PotType{
		Id: PotTypeValidator.Id,
	}
	rsp_err := PotTypeService.DeletePotType(TpProduct)
	if rsp_err == nil {
		response.SuccessWithMessage(200, "success", (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(pot.Ctx))
	}
}
