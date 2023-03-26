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

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpOtaDeviceController struct {
	beego.Controller
}

// 列表
// 增加状态分类
func (TpOtaDeviceController *TpOtaDeviceController) List() {
	PaginationValidate := valid.TpOtaDevicePaginationValidate{}
	err := json.Unmarshal(TpOtaDeviceController.Ctx.Input.RequestBody, &PaginationValidate)
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
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpOtaDeviceController.Ctx))
			break
		}
		return
	}
	var TpOtaDeviceService services.TpOtaDeviceService
	isSuccess, d, t := TpOtaDeviceService.GetTpOtaDeviceList(PaginationValidate)
	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpOtaDeviceController.Ctx))
		return
	}
	datamap := make(map[string]interface{})
	datamap["list"] = d
	success, c := TpOtaDeviceService.GetTpOtaDeviceStatusCount(PaginationValidate)
	if !success {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpOtaDeviceController.Ctx))
		return
	}
	datamap["statuscount"] = c
	dd := valid.RspTpOtaDevicePaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        datamap,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpOtaDeviceController.Ctx))

}

// 新增
func (TpOtaDeviceController *TpOtaDeviceController) Add() {
	AddTpOtaDeviceValidate := valid.AddTpOtaDeviceValidate{}
	err := json.Unmarshal(TpOtaDeviceController.Ctx.Input.RequestBody, &AddTpOtaDeviceValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpOtaDeviceValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpOtaDeviceValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpOtaDeviceController.Ctx))
			break
		}
		return
	}
	var TpOtaDeviceService services.TpOtaDeviceService
	id := utils.GetUuid()
	TpOtaDevice := models.TpOtaDevice{
		Id:               id,
		DeviceId:         AddTpOtaDeviceValidate.DeviceId,
		CurrentVersion:   AddTpOtaDeviceValidate.CurrentVersion,
		TargetVersion:    AddTpOtaDeviceValidate.TargetVersion,
		UpgradeProgress:  AddTpOtaDeviceValidate.UpgradeProgress,
		StatusUpdateTime: AddTpOtaDeviceValidate.StatusUpdateTime,
		UpgradeStatus:    AddTpOtaDeviceValidate.UpgradeStatus,
		StatusDetail:     AddTpOtaDeviceValidate.StatusDetail,
	}
	d, rsp_err := TpOtaDeviceService.AddTpOtaDevice(TpOtaDevice)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpOtaDeviceController.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23505")
		if isTrue {
			err = "批次编号不能重复！"
		} else {
			err = rsp_err.Error()
		}
		utils.SuccessWithMessage(400, err, (*context2.Context)(TpOtaDeviceController.Ctx))
	}
}

//修改状态
func (TpOtaDeviceController *TpOtaDeviceController) ModfiyUpdate() {
	TpOtaDeviceIdValidate := valid.TpOtaDeviceIdValidate{}
	err := json.Unmarshal(TpOtaDeviceController.Ctx.Input.RequestBody, &TpOtaDeviceIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpOtaDeviceIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpOtaDeviceIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpOtaDeviceController.Ctx))
			break
		}
		return
	}
	if TpOtaDeviceIdValidate.Id == "" && TpOtaDeviceIdValidate.OtaTaskId == "" {
		utils.SuccessWithMessage(1000, "id与任务id不能同时为空", (*context2.Context)(TpOtaDeviceController.Ctx))
	}
	var TpOtaDeviceService services.TpOtaDeviceService
	rsp_err := TpOtaDeviceService.ModfiyUpdateDevice(TpOtaDeviceIdValidate)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(TpOtaDeviceController.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpOtaDeviceController.Ctx))
	}
}
