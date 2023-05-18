package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
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

type TpAutomationController struct {
	beego.Controller
}

// 列表
func (c *TpAutomationController) List() {
	reqData := valid.TpAutomationPaginationValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		utils.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpAutomationService services.TpAutomationService
	d, t, err := TpAutomationService.GetTpAutomationList(reqData, tenantId)

	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspTpAutomationPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 编辑
func (TpAutomationController *TpAutomationController) Edit() {
	TpAutomationValidate := valid.TpAutomationValidate{}
	err := json.Unmarshal(TpAutomationController.Ctx.Input.RequestBody, &TpAutomationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpAutomationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpAutomationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpAutomationController.Ctx))
			break
		}
		return
	}
	if TpAutomationValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpAutomationController.Ctx))
	}
	var TpAutomationService services.TpAutomationService
	d, err := TpAutomationService.EditTpAutomation(TpAutomationValidate)
	if err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpAutomationController.Ctx))
	} else {
		utils.SuccessWithMessage(400, err.Error(), (*context2.Context)(TpAutomationController.Ctx))
	}
}

// 新增
func (c *TpAutomationController) Add() {
	reqData := valid.AddTpAutomationValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		utils.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	reqData.TenantId = tenantId
	var TpAutomationService services.TpAutomationService
	d, rsp_err := TpAutomationService.AddTpAutomation(reqData)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}
}

// 删除
func (TpAutomationController *TpAutomationController) Delete() {
	TpAutomationIdValidate := valid.TpAutomationIdValidate{}
	err := json.Unmarshal(TpAutomationController.Ctx.Input.RequestBody, &TpAutomationIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpAutomationIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpAutomationIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpAutomationController.Ctx))
			break
		}
		return
	}
	if TpAutomationIdValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpAutomationController.Ctx))
	}
	var TpAutomationService services.TpAutomationService
	req_err := TpAutomationService.DeleteTpAutomation(TpAutomationIdValidate.Id)
	if req_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(TpAutomationController.Ctx))
	} else {
		utils.SuccessWithMessage(400, req_err.Error(), (*context2.Context)(TpAutomationController.Ctx))
	}
}

// 详情
func (TpAutomationController *TpAutomationController) Detail() {
	TpAutomationIdValidate := valid.TpAutomationIdValidate{}
	err := json.Unmarshal(TpAutomationController.Ctx.Input.RequestBody, &TpAutomationIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpAutomationIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpAutomationIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpAutomationController.Ctx))
			break
		}
		return
	}
	var TpAutomationService services.TpAutomationService
	d, err := TpAutomationService.GetTpAutomationDetail(TpAutomationIdValidate.Id)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(TpAutomationController.Ctx))
		return
	}
	utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpAutomationController.Ctx))
}

// 开启和关闭
func (TpAutomationController *TpAutomationController) Enabled() {
	TpAutomationIdValidate := valid.TpAutomationEnabledValidate{}
	err := json.Unmarshal(TpAutomationController.Ctx.Input.RequestBody, &TpAutomationIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpAutomationIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpAutomationIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpAutomationController.Ctx))
			break
		}
		return
	}
	var TpAutomationService services.TpAutomationService
	err = TpAutomationService.EnabledAutomation(TpAutomationIdValidate.Id, TpAutomationIdValidate.Enabled)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(TpAutomationController.Ctx))
		return
	}
	utils.SuccessWithMessage(200, "sucess", (*context2.Context)(TpAutomationController.Ctx))
}
