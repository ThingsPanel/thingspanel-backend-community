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

type ChartController struct {
	beego.Controller
}

// 列表
func (ChartController *ChartController) List() {
	PaginationValidate := valid.ChartPaginationValidate{}
	err := json.Unmarshal(ChartController.Ctx.Input.RequestBody, &PaginationValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(ChartController.Ctx))
			break
		}
		return
	}
	var ChartService services.ChartService
	isSuccess, d, t := ChartService.GetChartList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(ChartController.Ctx))
		return
	}
	dd := valid.RspChartPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(ChartController.Ctx))
}

// 编辑
func (ChartController *ChartController) Edit() {
	ChartValidate := valid.ChartValidate{}
	err := json.Unmarshal(ChartController.Ctx.Input.RequestBody, &ChartValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(ChartValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(ChartValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(ChartController.Ctx))
			break
		}
		return
	}
	if ChartValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(ChartController.Ctx))
	}
	var ChartService services.ChartService
	isSucess := ChartService.EditChart(ChartValidate)
	if isSucess {
		d := ChartService.GetChartDetail(ChartValidate.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(ChartController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(ChartController.Ctx))
	}
}

// 新增
func (ChartController *ChartController) Add() {
	AddChartValidate := valid.AddChartValidate{}
	err := json.Unmarshal(ChartController.Ctx.Input.RequestBody, &AddChartValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddChartValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddChartValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(ChartController.Ctx))
			break
		}
		return
	}
	var ChartService services.ChartService
	id := uuid.GetUuid()
	Chart := models.Chart{
		ID:        id,
		ChartType: AddChartValidate.ChartType,
		ChartData: AddChartValidate.ChartData,
		ChartName: AddChartValidate.ChartName,
		CreatedAt: time.Now().Unix(),
		Sort:      AddChartValidate.Sort,
		Issued:    AddChartValidate.Issued,
		Remark:    AddChartValidate.Remark,
		Flag:      AddChartValidate.Flag,
	}
	isSucess, d := ChartService.AddChart(Chart)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(ChartController.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(ChartController.Ctx))
	}
}

// 删除
func (ChartController *ChartController) Delete() {
	ChartValidate := valid.ChartValidate{}
	err := json.Unmarshal(ChartController.Ctx.Input.RequestBody, &ChartValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(ChartValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(ChartValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(ChartController.Ctx))
			break
		}
		return
	}
	if ChartValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(ChartController.Ctx))
	}
	var ChartService services.ChartService
	Chart := models.Chart{
		ID: ChartValidate.Id,
	}
	isSucess := ChartService.DeleteChart(Chart)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(ChartController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(ChartController.Ctx))
	}
}
