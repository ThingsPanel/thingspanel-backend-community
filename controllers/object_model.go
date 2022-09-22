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

type ObjectModelController struct {
	beego.Controller
}

// 列表
func (ObjectModelController *ObjectModelController) List() {
	PaginationValidate := valid.ObjectModelPaginationValidate{}
	err := json.Unmarshal(ObjectModelController.Ctx.Input.RequestBody, &PaginationValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(ObjectModelController.Ctx))
			break
		}
		return
	}
	var ObjectModelService services.ObjectModelService
	isSuccess, d, t := ObjectModelService.GetObjectModelList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(ObjectModelController.Ctx))
		return
	}
	dd := valid.RspObjectModelPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(ObjectModelController.Ctx))
}

// 编辑
func (ObjectModelController *ObjectModelController) Edit() {
	ObjectModelValidate := valid.ObjectModelValidate{}
	err := json.Unmarshal(ObjectModelController.Ctx.Input.RequestBody, &ObjectModelValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(ObjectModelValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(ObjectModelValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(ObjectModelController.Ctx))
			break
		}
		return
	}
	if ObjectModelValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(ObjectModelController.Ctx))
	}
	var ObjectModelService services.ObjectModelService
	isSucess := ObjectModelService.EditObjectModel(ObjectModelValidate)
	if isSucess {
		d := ObjectModelService.GetObjectModelDetail(ObjectModelValidate.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(ObjectModelController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(ObjectModelController.Ctx))
	}
}

// 新增
func (ObjectModelController *ObjectModelController) Add() {
	AddObjectModelValidate := valid.AddObjectModelValidate{}
	err := json.Unmarshal(ObjectModelController.Ctx.Input.RequestBody, &AddObjectModelValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddObjectModelValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddObjectModelValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(ObjectModelController.Ctx))
			break
		}
		return
	}
	var ObjectModelService services.ObjectModelService
	id := uuid.GetUuid()
	ObjectModel := models.ObjectModel{
		Id:             id,
		ObjectName:     AddObjectModelValidate.ObjectName,
		ObjectDescribe: AddObjectModelValidate.ObjectDescribe,
		ObjectType:     AddObjectModelValidate.ObjectType,
		ObjectData:     AddObjectModelValidate.ObjectData,
		CreatedAt:      time.Now().Unix(),
		Sort:           AddObjectModelValidate.Sort,
		Remark:         AddObjectModelValidate.Remark,
	}
	if ObjectModel.ObjectData == "" {
		ObjectModel.ObjectData = "{}"
	}
	isSucess, d := ObjectModelService.AddObjectModel(ObjectModel)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(ObjectModelController.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(ObjectModelController.Ctx))
	}
}

// 删除
func (ObjectModelController *ObjectModelController) Delete() {
	ObjectModelValidate := valid.ObjectModelValidate{}
	err := json.Unmarshal(ObjectModelController.Ctx.Input.RequestBody, &ObjectModelValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(ObjectModelValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(ObjectModelValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(ObjectModelController.Ctx))
			break
		}
		return
	}
	if ObjectModelValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(ObjectModelController.Ctx))
	}
	var ObjectModelService services.ObjectModelService
	ObjectModel := models.ObjectModel{
		Id: ObjectModelValidate.Id,
	}
	isSucess := ObjectModelService.DeleteObjectModel(ObjectModel)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(ObjectModelController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(ObjectModelController.Ctx))
	}
}
