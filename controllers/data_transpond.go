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

type DataTranspondController struct {
	beego.Controller
}

// 列表
func (DataTranspondController *DataTranspondController) List() {
	PaginationValidate := valid.PaginationValidate{}
	err := json.Unmarshal(DataTranspondController.Ctx.Input.RequestBody, &PaginationValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(DataTranspondController.Ctx))
			break
		}
		return
	}
	var DataTranspondService services.DataTranspondService
	isSuccess, d, t := DataTranspondService.GetDataTranspondList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(DataTranspondController.Ctx))
		return
	}
	dd := valid.RspPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(DataTranspondController.Ctx))
}

// 编辑
func (DataTranspondController *DataTranspondController) Edit() {
	DataTranspondValidate := valid.DataTranspondValidate{}
	err := json.Unmarshal(DataTranspondController.Ctx.Input.RequestBody, &DataTranspondValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DataTranspondValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DataTranspondValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DataTranspondController.Ctx))
			break
		}
		return
	}
	if DataTranspondValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(DataTranspondController.Ctx))
	}
	var DataTranspondService services.DataTranspondService
	DataTranspond := models.DataTranspond{
		Id:          DataTranspondValidate.Id,
		ProcessId:   DataTranspondValidate.ProcessId,
		ProcessType: DataTranspondValidate.ProcessType,
		Label:       DataTranspondValidate.Label,
		Disabled:    DataTranspondValidate.Disabled,
		Info:        DataTranspondValidate.Info,
		Env:         DataTranspondValidate.Env,
		CustomerId:  DataTranspondValidate.CustomerId,
		RoleType:    DataTranspondValidate.RoleType,
	}
	isSucess := DataTranspondService.EditDataTranspond(DataTranspond)
	if isSucess {
		response.SuccessWithDetailed(200, "success", DataTranspond, map[string]string{}, (*context2.Context)(DataTranspondController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(DataTranspondController.Ctx))
	}
}

// 新增
func (DataTranspondController *DataTranspondController) Add() {
	AddDataTranspondValidate := valid.AddDataTranspondValidate{}
	err := json.Unmarshal(DataTranspondController.Ctx.Input.RequestBody, &AddDataTranspondValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddDataTranspondValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddDataTranspondValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DataTranspondController.Ctx))
			break
		}
		return
	}
	var DataTranspondService services.DataTranspondService
	id := uuid.GetUuid()
	DataTranspond := models.DataTranspond{
		Id:          id,
		ProcessId:   AddDataTranspondValidate.ProcessId,
		ProcessType: AddDataTranspondValidate.ProcessType,
		Label:       AddDataTranspondValidate.Label,
		Disabled:    AddDataTranspondValidate.Disabled,
		Info:        AddDataTranspondValidate.Info,
		Env:         AddDataTranspondValidate.Env,
		CustomerId:  AddDataTranspondValidate.CustomerId,
		CreatedAt:   time.Now().Unix(),
		RoleType:    AddDataTranspondValidate.RoleType,
	}
	isSucess, d := DataTranspondService.AddDataTranspond(DataTranspond)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DataTranspondController.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(DataTranspondController.Ctx))
	}
}

// 删除
func (DataTranspondController *DataTranspondController) Delete() {
	DataTranspondValidate := valid.DataTranspondValidate{}
	err := json.Unmarshal(DataTranspondController.Ctx.Input.RequestBody, &DataTranspondValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DataTranspondValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DataTranspondValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DataTranspondController.Ctx))
			break
		}
		return
	}
	if DataTranspondValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(DataTranspondController.Ctx))
	}
	var DataTranspondService services.DataTranspondService
	DataTranspond := models.DataTranspond{
		Id: DataTranspondValidate.Id,
	}
	isSucess := DataTranspondService.DeleteDataTranspond(DataTranspond)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(DataTranspondController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(DataTranspondController.Ctx))
	}
}
