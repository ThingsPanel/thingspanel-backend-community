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

type TpProtocolPluginController struct {
	beego.Controller
}

// 列表
func (TpProtocolPluginController *TpProtocolPluginController) List() {
	PaginationValidate := valid.TpProtocolPluginPaginationValidate{}
	err := json.Unmarshal(TpProtocolPluginController.Ctx.Input.RequestBody, &PaginationValidate)
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
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpProtocolPluginController.Ctx))
			break
		}
		return
	}
	var TpProtocolPluginService services.TpProtocolPluginService
	isSuccess, d, t := TpProtocolPluginService.GetTpProtocolPluginList(PaginationValidate)

	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpProtocolPluginController.Ctx))
		return
	}
	dd := valid.RspTpProtocolPluginPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpProtocolPluginController.Ctx))
}

// 编辑
func (TpProtocolPluginController *TpProtocolPluginController) Edit() {
	TpProtocolPluginValidate := valid.TpProtocolPluginValidate{}
	err := json.Unmarshal(TpProtocolPluginController.Ctx.Input.RequestBody, &TpProtocolPluginValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpProtocolPluginValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpProtocolPluginValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpProtocolPluginController.Ctx))
			break
		}
		return
	}
	if TpProtocolPluginValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpProtocolPluginController.Ctx))
	}
	var TpProtocolPluginService services.TpProtocolPluginService
	rsp_err := TpProtocolPluginService.EditTpProtocolPlugin(TpProtocolPluginValidate)
	if rsp_err != nil {
		d := TpProtocolPluginService.GetTpProtocolPluginDetail(TpProtocolPluginValidate.Id)
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpProtocolPluginController.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpProtocolPluginController.Ctx))
	}
}

// 新增
func (TpProtocolPluginController *TpProtocolPluginController) Add() {
	AddTpProtocolPluginValidate := valid.AddTpProtocolPluginValidate{}
	err := json.Unmarshal(TpProtocolPluginController.Ctx.Input.RequestBody, &AddTpProtocolPluginValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpProtocolPluginValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpProtocolPluginValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpProtocolPluginController.Ctx))
			break
		}
		return
	}
	var TpProtocolPluginService services.TpProtocolPluginService
	p := TpProtocolPluginService.GetByProtocolType(AddTpProtocolPluginValidate.ProtocolType, AddTpProtocolPluginValidate.DeviceType)
	//存在记录，更新
	if p.Id != "" {
		TpProtocolPlugin := valid.TpProtocolPluginValidate{
			Id:             p.Id,
			Name:           AddTpProtocolPluginValidate.Name,
			AccessAddress:  AddTpProtocolPluginValidate.AccessAddress,
			HttpAddress:    AddTpProtocolPluginValidate.HttpAddress,
			SubTopicPrefix: AddTpProtocolPluginValidate.SubTopicPrefix,
			ProtocolType:   AddTpProtocolPluginValidate.ProtocolType,
			Description:    AddTpProtocolPluginValidate.Description,
			DeviceType:     AddTpProtocolPluginValidate.DeviceType,
			AdditionalInfo: AddTpProtocolPluginValidate.AdditionalInfo,
		}
		err := TpProtocolPluginService.EditTpProtocolPlugin(TpProtocolPlugin)
		if err == nil {
			utils.SuccessWithMessage(200, "update success", (*context2.Context)(TpProtocolPluginController.Ctx))
		} else {
			utils.SuccessWithMessage(400, err.Error(), (*context2.Context)(TpProtocolPluginController.Ctx))
		}
	} else { //不存在，新增
		id := utils.GetUuid()
		TpProtocolPlugin := models.TpProtocolPlugin{
			Id:             id,
			Name:           AddTpProtocolPluginValidate.Name,
			AccessAddress:  AddTpProtocolPluginValidate.AccessAddress,
			HttpAddress:    AddTpProtocolPluginValidate.HttpAddress,
			SubTopicPrefix: AddTpProtocolPluginValidate.SubTopicPrefix,
			ProtocolType:   AddTpProtocolPluginValidate.ProtocolType,
			Description:    AddTpProtocolPluginValidate.Description,
			DeviceType:     AddTpProtocolPluginValidate.DeviceType,
			AdditionalInfo: AddTpProtocolPluginValidate.AdditionalInfo,
			CreatedAt:      time.Now().Unix(),
		}
		d, rsp_err := TpProtocolPluginService.AddTpProtocolPlugin(TpProtocolPlugin)
		if rsp_err == nil {
			utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpProtocolPluginController.Ctx))
		} else {
			utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpProtocolPluginController.Ctx))
		}
	}
}

// 删除
func (TpProtocolPluginController *TpProtocolPluginController) Delete() {
	TpProtocolPluginIdValidate := valid.TpProtocolPluginIdValidate{}
	err := json.Unmarshal(TpProtocolPluginController.Ctx.Input.RequestBody, &TpProtocolPluginIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpProtocolPluginIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpProtocolPluginIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpProtocolPluginController.Ctx))
			break
		}
		return
	}
	if TpProtocolPluginIdValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpProtocolPluginController.Ctx))
	}
	var TpProtocolPluginService services.TpProtocolPluginService
	TpProtocolPlugin := models.TpProtocolPlugin{
		Id: TpProtocolPluginIdValidate.Id,
	}
	req_err := TpProtocolPluginService.DeleteTpProtocolPlugin(TpProtocolPlugin)
	if req_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(TpProtocolPluginController.Ctx))
	} else {
		utils.SuccessWithMessage(400, "删除失败", (*context2.Context)(TpProtocolPluginController.Ctx))
	}
}
