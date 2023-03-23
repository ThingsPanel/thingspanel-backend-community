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
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpBatchController struct {
	beego.Controller
}

// 列表
func (TpBatchController *TpBatchController) List() {
	PaginationValidate := valid.TpBatchPaginationValidate{}
	err := json.Unmarshal(TpBatchController.Ctx.Input.RequestBody, &PaginationValidate)
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
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpBatchController.Ctx))
			break
		}
		return
	}
	var TpBatchService services.TpBatchService
	isSuccess, d, t := TpBatchService.GetTpBatchList(PaginationValidate)

	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpBatchController.Ctx))
		return
	}
	dd := valid.RspTpBatchPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpBatchController.Ctx))
}

// 编辑
func (TpBatchController *TpBatchController) Edit() {
	TpBatchValidate := valid.TpBatchValidate{}
	err := json.Unmarshal(TpBatchController.Ctx.Input.RequestBody, &TpBatchValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpBatchValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpBatchValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpBatchController.Ctx))
			break
		}
		return
	}
	if TpBatchValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpBatchController.Ctx))
	}
	var TpBatchService services.TpBatchService
	isSucess := TpBatchService.EditTpBatch(TpBatchValidate)
	if isSucess {
		d := TpBatchService.GetTpBatchDetail(TpBatchValidate.Id)
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpBatchController.Ctx))
	} else {
		utils.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpBatchController.Ctx))
	}
}

// 新增
func (TpBatchController *TpBatchController) Add() {
	AddTpBatchValidate := valid.AddTpBatchValidate{}
	err := json.Unmarshal(TpBatchController.Ctx.Input.RequestBody, &AddTpBatchValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpBatchValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpBatchValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpBatchController.Ctx))
			break
		}
		return
	}
	var TpBatchService services.TpBatchService
	id := utils.GetUuid()
	if AddTpBatchValidate.GenerateFlag == "" {
		AddTpBatchValidate.GenerateFlag = "0"
	}
	TpBatch := models.TpBatch{
		Id:            id,
		BatchNumber:   AddTpBatchValidate.BatchNumber,
		Describle:     AddTpBatchValidate.Describle,
		DeviceNumber:  AddTpBatchValidate.DeviceNumber,
		CreatedTime:   time.Now().Unix(),
		GenerateFlag:  AddTpBatchValidate.GenerateFlag,
		ProductId:     AddTpBatchValidate.ProductId,
		Remark:        AddTpBatchValidate.Remark,
		AccessAddress: AddTpBatchValidate.AccessAddress,
	}
	d, rsp_err := TpBatchService.AddTpBatch(TpBatch)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpBatchController.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23505")
		if isTrue {
			err = "批次编号不能重复！"
		} else {
			err = rsp_err.Error()
		}
		utils.SuccessWithMessage(400, err, (*context2.Context)(TpBatchController.Ctx))
	}
}

// 删除
func (TpBatchController *TpBatchController) Delete() {
	TpBatchIdValidate := valid.TpBatchIdValidate{}
	err := json.Unmarshal(TpBatchController.Ctx.Input.RequestBody, &TpBatchIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpBatchIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpBatchIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpBatchController.Ctx))
			break
		}
		return
	}
	if TpBatchIdValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpBatchController.Ctx))
	}
	var TpBatchService services.TpBatchService
	TpBatch := models.TpBatch{
		Id: TpBatchIdValidate.Id,
	}
	rsp_err := TpBatchService.DeleteTpBatch(TpBatch)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(TpBatchController.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpBatchController.Ctx))
	}
}

// 生成批次
func (TpBatchController *TpBatchController) GenerateBatchById() {
	TpBatchIdValidate := valid.TpBatchIdValidate{}
	err := json.Unmarshal(TpBatchController.Ctx.Input.RequestBody, &TpBatchIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpBatchIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpBatchIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpBatchController.Ctx))
			break
		}
		return
	}
	// if TpBatchIdValidate.Id == "" {
	// 	utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpBatchController.Ctx))
	// }
	var TpBatchService services.TpBatchService
	rsp_err := TpBatchService.GenerateBatch(TpBatchIdValidate.Id)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(TpBatchController.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpBatchController.Ctx))
	}
}

// 导出
func (TpBatchController *TpBatchController) Export() {
	TpBatchIdValidate := valid.TpBatchIdValidate{}
	err := json.Unmarshal(TpBatchController.Ctx.Input.RequestBody, &TpBatchIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpBatchIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpBatchIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpBatchController.Ctx))
			break
		}
		return
	}
	// if TpBatchIdValidate.Id == "" {
	// 	utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpBatchController.Ctx))
	// }
	var TpBatchService services.TpBatchService
	filepath, rsp_err := TpBatchService.Export(TpBatchIdValidate.Id)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", filepath, map[string]string{}, (*context2.Context)(TpBatchController.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpBatchController.Ctx))
	}
}

//导入
func (TpBatchController *TpBatchController) Import() {
	ImportTpBatchValidate := valid.ImportTpBatchValidate{}
	err := json.Unmarshal(TpBatchController.Ctx.Input.RequestBody, &ImportTpBatchValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(ImportTpBatchValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(ImportTpBatchValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpBatchController.Ctx))
			break
		}
		return
	}
	if err := utils.CheckPathFilename(ImportTpBatchValidate.File); err != nil || ImportTpBatchValidate.File == "" {
		utils.SuccessWithMessage(1000, "文件不合法或不存在", (*context2.Context)(TpBatchController.Ctx))
	}
	var TpBatchService services.TpBatchService
	id := utils.GetUuid()
	TpBatch := models.TpBatch{
		Id:           id,
		BatchNumber:  ImportTpBatchValidate.BatchNumber,
		CreatedTime:  time.Now().Unix(),
		GenerateFlag: "0",
		ProductId:    ImportTpBatchValidate.ProductId,
	}
	var data map[string]interface{}
	d, rsp_err := TpBatchService.Import(id, ImportTpBatchValidate.BatchNumber, ImportTpBatchValidate.ProductId, ImportTpBatchValidate.File)
	if rsp_err != nil {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpBatchController.Ctx))
	}
	tpbath, rsp_err1 := TpBatchService.AddTpBatch(TpBatch)
	if rsp_err1 == nil {
		data["tpbath"] = tpbath
		data["generate_devices"] = d
		utils.SuccessWithDetailed(200, "success", data, map[string]string{}, (*context2.Context)(TpBatchController.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23505")
		if isTrue {
			err = "批次编号不能重复！"
		} else {
			err = rsp_err1.Error()
		}
		utils.SuccessWithMessage(400, err, (*context2.Context)(TpBatchController.Ctx))
	}
}
