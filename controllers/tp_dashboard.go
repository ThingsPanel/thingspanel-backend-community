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

type TpDashboardController struct {
	beego.Controller
}

// 列表
func (TpDashboardController *TpDashboardController) List() {
	PaginationValidate := valid.TpDashboardPaginationValidate{}
	err := json.Unmarshal(TpDashboardController.Ctx.Input.RequestBody, &PaginationValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpDashboardController.Ctx))
			break
		}
		return
	}
	var TpDashboardService services.TpDashboardService
	isSuccess, d, t := TpDashboardService.GetTpDashboardList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpDashboardController.Ctx))
		return
	}
	dd := valid.RspTpDashboardPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpDashboardController.Ctx))
}

// 编辑
func (TpDashboardController *TpDashboardController) Edit() {
	TpDashboardValidate := valid.TpDashboardValidate{}
	err := json.Unmarshal(TpDashboardController.Ctx.Input.RequestBody, &TpDashboardValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpDashboardValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpDashboardValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpDashboardController.Ctx))
			break
		}
		return
	}
	if TpDashboardValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpDashboardController.Ctx))
	}
	var TpDashboardService services.TpDashboardService
	isSucess := TpDashboardService.EditTpDashboard(TpDashboardValidate)
	if isSucess {
		d := TpDashboardService.GetTpDashboardDetail(TpDashboardValidate.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpDashboardController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpDashboardController.Ctx))
	}
}

// 新增
func (TpDashboardController *TpDashboardController) Add() {
	AddTpDashboardValidate := valid.AddTpDashboardValidate{}
	err := json.Unmarshal(TpDashboardController.Ctx.Input.RequestBody, &AddTpDashboardValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpDashboardValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpDashboardValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpDashboardController.Ctx))
			break
		}
		return
	}
	var TpDashboardService services.TpDashboardService
	id := uuid.GetUuid()
	TpDashboard := models.TpDashboard{
		Id:            id,
		RelationId:    AddTpDashboardValidate.RelationId,
		JsonData:      AddTpDashboardValidate.JsonData,
		DashboardName: AddTpDashboardValidate.DashboardName,
		CreateAt:      time.Now().Unix(),
		Sort:          AddTpDashboardValidate.Sort,
		Remark:        AddTpDashboardValidate.Remark,
	}
	if TpDashboard.JsonData == "" {
		TpDashboard.JsonData = "{}"
	}
	isSucess, d := TpDashboardService.AddTpDashboard(TpDashboard)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpDashboardController.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(TpDashboardController.Ctx))
	}
}

// 删除
func (TpDashboardController *TpDashboardController) Delete() {
	TpDashboardValidate := valid.TpDashboardValidate{}
	err := json.Unmarshal(TpDashboardController.Ctx.Input.RequestBody, &TpDashboardValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpDashboardValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpDashboardValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(TpDashboardController.Ctx))
			break
		}
		return
	}
	if TpDashboardValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpDashboardController.Ctx))
	}
	var TpDashboardService services.TpDashboardService
	TpDashboard := models.TpDashboard{
		Id: TpDashboardValidate.Id,
	}
	isSucess := TpDashboardService.DeleteTpDashboard(TpDashboard)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(TpDashboardController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpDashboardController.Ctx))
	}
}
