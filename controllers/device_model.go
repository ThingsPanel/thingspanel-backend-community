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

type DeviceModelController struct {
	beego.Controller
}

// 列表
func (DeviceModelController *DeviceModelController) List() {
	PaginationValidate := valid.DeviceModelPaginationValidate{}
	err := json.Unmarshal(DeviceModelController.Ctx.Input.RequestBody, &PaginationValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceModelController.Ctx))
			break
		}
		return
	}
	var DeviceModelService services.DeviceModelService
	isSuccess, d, t := DeviceModelService.GetDeviceModelList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(DeviceModelController.Ctx))
		return
	}
	dd := valid.RspDeviceModelPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(DeviceModelController.Ctx))
}

// 编辑
func (DeviceModelController *DeviceModelController) Edit() {
	DeviceModelValidate := valid.DeviceModelValidate{}
	err := json.Unmarshal(DeviceModelController.Ctx.Input.RequestBody, &DeviceModelValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DeviceModelValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DeviceModelValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceModelController.Ctx))
			break
		}
		return
	}
	if DeviceModelValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(DeviceModelController.Ctx))
	}
	var DeviceModelService services.DeviceModelService
	isSucess := DeviceModelService.EditDeviceModel(DeviceModelValidate)
	if isSucess {
		d := DeviceModelService.GetDeviceModelDetail(DeviceModelValidate.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DeviceModelController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(DeviceModelController.Ctx))
	}
}

// 新增
func (DeviceModelController *DeviceModelController) Add() {
	AddDeviceModelValidate := valid.AddDeviceModelValidate{}
	err := json.Unmarshal(DeviceModelController.Ctx.Input.RequestBody, &AddDeviceModelValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddDeviceModelValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddDeviceModelValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceModelController.Ctx))
			break
		}
		return
	}
	var DeviceModelService services.DeviceModelService
	id := uuid.GetUuid()
	DeviceModel := models.DeviceModel{
		ID:        id,
		ModelName: AddDeviceModelValidate.ModelName,
		Flag:      AddDeviceModelValidate.Flag,
		ChartData: AddDeviceModelValidate.ChartData,
		ModelType: AddDeviceModelValidate.ModelType,
		Describe:  AddDeviceModelValidate.Describe,
		Version:   AddDeviceModelValidate.Version,
		Author:    AddDeviceModelValidate.Author,
		CreatedAt: time.Now().Unix(),
		Sort:      AddDeviceModelValidate.Sort,
		Issued:    AddDeviceModelValidate.Issued,
		Remark:    AddDeviceModelValidate.Remark,
	}
	if DeviceModel.ChartData == "" {
		DeviceModel.ChartData = "{}"
	}
	isSucess, d := DeviceModelService.AddDeviceModel(DeviceModel)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DeviceModelController.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(DeviceModelController.Ctx))
	}
}

// 删除
func (DeviceModelController *DeviceModelController) Delete() {
	DeviceModelValidate := valid.DeviceModelValidate{}
	err := json.Unmarshal(DeviceModelController.Ctx.Input.RequestBody, &DeviceModelValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DeviceModelValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DeviceModelValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceModelController.Ctx))
			break
		}
		return
	}
	if DeviceModelValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(DeviceModelController.Ctx))
	}
	var DeviceModelService services.DeviceModelService
	DeviceModel := models.DeviceModel{
		ID: DeviceModelValidate.Id,
	}
	isSucess := DeviceModelService.DeleteDeviceModel(DeviceModel)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(DeviceModelController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(DeviceModelController.Ctx))
	}
}

// 删除
func (DeviceModelController *DeviceModelController) DeviceModelTree() {
	var DeviceModelService services.DeviceModelService
	trees := DeviceModelService.DeviceModelTree()
	response.SuccessWithDetailed(200, "success", trees, map[string]string{}, (*context2.Context)(DeviceModelController.Ctx))
}
